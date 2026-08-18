package api

// Credential (WebAuthn/passkey) endpoints and the approval-challenge flow
// (LLD-02 §4). All crypto verification lives in internal/identity
// (webauthn.go) — this file only authenticates the caller, holds the
// short-lived server-side challenge state a WebAuthn ceremony needs, and
// reconstructs the canonical string from the node's OWN state (never from
// anything the client echoes back — LLD-02 §2).

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

const challengeTTL = 5 * time.Minute

// regChallenge is one outstanding REGISTRATION challenge. Registration is
// the one classic WebAuthn flow here (LLD-02 §4): the server picks a random
// challenge, unlike the deterministic approval-decision challenge below.
type regChallenge struct {
	challenge []byte
	expires   time.Time
}

// approvalChallenge is one outstanding approval-DECISION challenge: the
// head_seq captured at issuance (LLD-02 §2 — the canonical fixes "state as
// of head_seq, what the human saw") and who it was issued to, so a signed
// assertion cannot be swapped onto a different actor's decision.
type approvalChallenge struct {
	headSeq uint64
	actorID string
	expires time.Time
}

// challengeStore is in-memory, per-node, non-durable — losing it on
// restart only means an in-flight ceremony must be restarted (begin/finish,
// or challenge/decide), never a security weakening: nothing about the
// eventual verification trusts anything the client could replay from a
// stale entry, since VerifyApprovalAssertion recomputes the canonical from
// the approval's own journaled state.
type challengeStore struct {
	mu   sync.Mutex
	reg  map[string]*regChallenge
	appr map[string]*approvalChallenge
	prop map[string]*approvalChallenge
}

func (c *challengeStore) putReg(actorID string, ch []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reg == nil {
		c.reg = map[string]*regChallenge{}
	}
	c.reg[actorID] = &regChallenge{challenge: ch, expires: time.Now().Add(challengeTTL)}
}

// takeReg consumes the challenge (one-shot): a registration ceremony is
// finished at most once per begin().
func (c *challengeStore) takeReg(actorID string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rc, ok := c.reg[actorID]
	if !ok {
		return nil, false
	}
	delete(c.reg, actorID)
	if time.Now().After(rc.expires) {
		return nil, false
	}
	return rc.challenge, true
}

func (c *challengeStore) putApproval(approvalID, actorID string, headSeq uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.appr == nil {
		c.appr = map[string]*approvalChallenge{}
	}
	c.appr[approvalID] = &approvalChallenge{headSeq: headSeq, actorID: actorID, expires: time.Now().Add(challengeTTL)}
}

// getApproval reads WITHOUT consuming: a decide attempt can fail
// (validation, a stale record) and be retried/re-signed within the TTL
// without re-fetching the challenge. The approval itself is one-shot
// regardless (Decide rejects a second decision on an already-decided
// approval), so leaving this readable does not enable a second real
// decision.
func (c *challengeStore) getApproval(approvalID string) (*approvalChallenge, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ac, ok := c.appr[approvalID]
	if !ok || time.Now().After(ac.expires) {
		return nil, false
	}
	return ac, true
}

// putProposal/getProposal are the definition-decision twin of the approval
// challenge store: same shape (head_seq captured at issuance, actor binding,
// TTL), keyed by proposal id. A signed proposal assertion cannot be swapped
// onto a different actor's decision; the proposal itself is one-shot
// regardless (DecideProposal rejects a second decision), so the entry stays
// readable within the TTL for a retry after a failed decide.
func (c *challengeStore) putProposal(proposalID, actorID string, headSeq uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.prop == nil {
		c.prop = map[string]*approvalChallenge{}
	}
	c.prop[proposalID] = &approvalChallenge{headSeq: headSeq, actorID: actorID, expires: time.Now().Add(challengeTTL)}
}

func (c *challengeStore) getProposal(proposalID string) (*approvalChallenge, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ac, ok := c.prop[proposalID]
	if !ok || time.Now().After(ac.expires) {
		return nil, false
	}
	return ac, true
}

// rpIDFromHost derives the WebAuthn relying party id from the request Host:
// the bare domain, no scheme, no port — the node has no separate rpId
// configuration, it is always exactly the host the request arrived on
// (consistent with api.Secure's same-origin policy, sameOrigin in
// secure.go).
func rpIDFromHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// originAllowed reuses the SAME same-origin check api.Secure already
// enforces on every request (LLD-02 §3.1: "origin ∈ разрешённых origin
// узла — те же значения, что same-origin-политика api.Secure").
func originAllowed(host string) func(string) bool {
	return func(origin string) bool { return sameOrigin(origin, host) }
}

// --- registration: POST /api/credentials/begin -----------------------------

type credentialsBeginResponse struct {
	Challenge        string           `json:"challenge"` // base64url
	RPID             string           `json:"rp_id"`
	RPName           string           `json:"rp_name"`
	UserID           string           `json:"user_id"`
	UserName         string           `json:"user_name"`
	PubKeyCredParams []map[string]any `json:"pub_key_cred_params"`
	TimeoutMS        int              `json:"timeout_ms"`
}

func (s *Server) credentialsBegin(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok || actor.Type != eventstore.ActorHuman {
		writeAuthRequired(w)
		return
	}
	ch := make([]byte, 32)
	if _, err := rand.Read(ch); err != nil {
		writeJSON(w, http.StatusInternalServerError, &engine.Err{Code: "INTERNAL", Message: "could not generate challenge"})
		return
	}
	s.challenges.putReg(actor.ID, ch)
	writeJSON(w, http.StatusOK, credentialsBeginResponse{
		Challenge: base64.RawURLEncoding.EncodeToString(ch),
		RPID:      rpIDFromHost(r.Host),
		RPName:    s.brand.Name,
		UserID:    actor.ID,
		UserName:  actor.ID,
		// ES256 mandatory, RS256 allowed (LLD-02 §3.3) — offered in that
		// preference order; nothing else is offered so a compliant browser
		// never even tries an alg this node would reject at verify time.
		PubKeyCredParams: []map[string]any{
			{"type": "public-key", "alg": -7},
			{"type": "public-key", "alg": -257},
		},
		TimeoutMS: 60000,
	})
}

// --- registration: POST /api/credentials/finish ----------------------------

type credentialsFinishRequest struct {
	ClientDataJSON    []byte   `json:"client_data_json"`
	AttestationObject []byte   `json:"attestation_object"`
	Transports        []string `json:"transports,omitempty"`
	Label             string   `json:"label,omitempty"`
}

func (s *Server) credentialsFinish(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok || actor.Type != eventstore.ActorHuman {
		writeAuthRequired(w)
		return
	}
	var req credentialsFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, &engine.Err{Code: engine.CodeValidation, Message: "bad json"})
		return
	}
	challenge, ok := s.challenges.takeReg(actor.ID)
	if !ok {
		writeJSON(w, http.StatusUnprocessableEntity, &engine.Err{Code: engine.CodeValidation,
			Message: "no outstanding registration challenge (expired or never begun)",
			FixHint: "call POST /api/credentials/begin first"})
		return
	}
	credID, cose, alg, aaguid, signCount, err := identity.VerifyRegistration(
		req.ClientDataJSON, req.AttestationObject, challenge, rpIDFromHost(r.Host), originAllowed(r.Host))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, &engine.Err{Code: engine.CodeValidation, Message: err.Error()})
		return
	}
	basis := &eventstore.Basis{Type: "human", ID: actor.ID}
	if err := s.reg.RegisterCredential(r.Context(), actor, credID, cose, alg, aaguid, signCount, req.Transports, req.Label, basis); err != nil {
		writeJSON(w, http.StatusInternalServerError, &engine.Err{Code: "INTERNAL", Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"credential_id": credID})
}

// --- GET /api/credentials (self) -------------------------------------------

func (s *Server) listCredentials(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	creds, err := s.reg.Credentials(r.Context(), actor.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, &engine.Err{Code: "INTERNAL", Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": creds})
}

// --- DELETE /api/credentials/{id} (self-revoke or admin) -------------------

type revokeCredentialRequest struct {
	Reason string `json:"reason,omitempty"`
	// ActorID lets an admin revoke another actor's credential (LLD-02 §4:
	// "сам актор или admin"). Defaults to the caller's own id — the common
	// case (lost device) needs no field at all.
	ActorID string `json:"actor_id,omitempty"`
}

func (s *Server) revokeCredential(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	var req revokeCredentialRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // body optional
	targetActorID := req.ActorID
	if targetActorID == "" {
		targetActorID = actor.ID
	}
	// D-014 (Cycle-A2): revoking ANOTHER actor's credential is an admin action
	// (LLD-02 §4 intent: "сам актор или admin"). The v0 check only required the
	// caller to be a human — any staff member could revoke a colleague's passkey
	// and lock them out of signing. Self-revocation stays open to the actor.
	if targetActorID != actor.ID &&
		(actor.Type != eventstore.ActorHuman || actor.Role != s.eng.DefinitionApprover()) {
		writeErr(w, &engine.Err{Code: engine.CodePermissionDenied,
			Message: "only the actor themself or the node admin (role " + s.eng.DefinitionApprover() + ") may revoke this credential",
			Rule:    "identity admin boundary (self-or-admin)"})
		return
	}
	credID := r.PathValue("id")
	if err := s.reg.RevokeCredential(r.Context(), actor, targetActorID, credID, req.Reason,
		&eventstore.Basis{Type: "human", ID: actor.ID}); err != nil {
		writeJSON(w, http.StatusInternalServerError, &engine.Err{Code: "INTERNAL", Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// --- GET /api/approvals/{id}/challenge (LLD-02 §4) --------------------------

type approvalChallengeResponse struct {
	TaskID     string `json:"task_id"`
	Entity     string `json:"entity"`
	RecordID   string `json:"record_id"`
	Transition string `json:"transition"`
	Decision   string `json:"decision"`
	BasisType  string `json:"basis_type"`
	BasisID    string `json:"basis_id"`
	HeadSeq    uint64 `json:"head_seq"`
	NodeID     string `json:"node_id"`
	RPID       string `json:"rp_id"`
	Challenge  string `json:"challenge"` // base64url(SHA-256(canonical))
}

// approvalCanonicalBasis fixes the canonical string's basis_type:basis_id
// component to the approval itself (LLD-02 §2's "basis_type ":" basis_id"
// line): the decision's justification IS the approval request being
// decided — this is a self-referential, always-reproducible choice, so
// challenge-issuance and later verification can never disagree about it
// (there is no round trip of a client-supplied basis to get out of sync
// over). This is UNRELATED to the eventstore.Basis the decide request still
// carries for the approval.decided EVENT's own Basis field (provenance of
// the API call, an existing, separate mechanism — see decideRequest.Basis).
func approvalCanonicalBasis(approvalID string) (basisType, basisID string) {
	return "approval", approvalID
}

func (s *Server) approvalChallenge(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok || actor.Type != eventstore.ActorHuman {
		writeAuthRequired(w)
		return
	}
	approvalID := r.PathValue("id")
	a, ok := s.eng.GetApproval(approvalID)
	if !ok {
		writeJSON(w, http.StatusNotFound, &engine.Err{Code: engine.CodeNotFound, Message: "approval " + approvalID + " not found"})
		return
	}
	if !s.eng.CanApprove(actor.Role, a.Action) {
		writeJSON(w, http.StatusForbidden, &engine.Err{Code: engine.CodePermissionDenied,
			Message: "role " + actor.Role + " may not approve " + a.Action,
			Rule:    "approve permissions list the actions a role signs"})
		return
	}
	decision := r.URL.Query().Get("decision")
	if decision != identity.DecisionGrant && decision != identity.DecisionDeny {
		writeJSON(w, http.StatusBadRequest, &engine.Err{Code: engine.CodeValidation,
			Message: "?decision must be \"grant\" or \"deny\""})
		return
	}
	headSeq, err := s.eng.HeadSeq(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, &engine.Err{Code: "INTERNAL", Message: err.Error()})
		return
	}
	s.challenges.putApproval(approvalID, actor.ID, headSeq)
	basisType, basisID := approvalCanonicalBasis(approvalID)
	canon := identity.BuildApprovalCanonical(approvalID, a.Entity, a.RecordID, a.Action, decision, basisType, basisID, headSeq, s.nodeID)
	challenge := identity.ApprovalChallenge(canon)
	writeJSON(w, http.StatusOK, approvalChallengeResponse{
		TaskID: approvalID, Entity: a.Entity, RecordID: a.RecordID, Transition: a.Action,
		Decision: decision, BasisType: basisType, BasisID: basisID,
		HeadSeq: headSeq, NodeID: s.nodeID, RPID: rpIDFromHost(r.Host),
		Challenge: base64.RawURLEncoding.EncodeToString(challenge[:]),
	})
}

// --- GET /api/proposals/{id}/challenge (definition-decision ceremony) -------

type proposalChallengeResponse struct {
	ProposalID  string   `json:"proposal_id"`
	Description string   `json:"description"`
	Plan        []string `json:"plan"`
	Decision    string   `json:"decision"`
	HeadSeq     uint64   `json:"head_seq"`
	NodeID      string   `json:"node_id"`
	RPID        string   `json:"rp_id"`
	Challenge   string   `json:"challenge"` // base64url(SHA-256(canonical))
}

// proposalChallenge issues the WebAuthn ceremony for a definition decision.
// Gated exactly like DecideProposal itself: only a human with the
// defApprover role can even fetch the challenge, and only for a pending
// proposal. The canonical is built server-side from the proposal id, the
// decision and the head_seq captured here (identity.BuildDefinitionCanonical)
// — never from anything the client echoes.
func (s *Server) proposalChallenge(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok || actor.Type != eventstore.ActorHuman {
		writeAuthRequired(w)
		return
	}
	proposalID := r.PathValue("id")
	p, err := s.eng.GetProposal(proposalID)
	if err != nil || p.Status != engine.ProposalPending {
		writeJSON(w, http.StatusNotFound, &engine.Err{Code: engine.CodeNotFound, Message: "proposal " + proposalID + " not found or not pending"})
		return
	}
	if actor.Role != s.eng.DefinitionApprover() {
		writeJSON(w, http.StatusForbidden, &engine.Err{Code: engine.CodePermissionDenied,
			Message: "definitions are approved by a human with role " + s.eng.DefinitionApprover(),
			Rule:    "definition approver"})
		return
	}
	decision := r.URL.Query().Get("decision")
	if decision != identity.DecisionGrant && decision != identity.DecisionDeny {
		writeJSON(w, http.StatusBadRequest, &engine.Err{Code: engine.CodeValidation,
			Message: "?decision must be \"grant\" or \"deny\""})
		return
	}
	headSeq, err := s.eng.HeadSeq(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, &engine.Err{Code: "INTERNAL", Message: err.Error()})
		return
	}
	s.challenges.putProposal(proposalID, actor.ID, headSeq)
	canon := identity.BuildDefinitionCanonical(proposalID, decision,
		engine.DefinitionContentHash(p.Files), headSeq, s.nodeID)
	challenge := identity.ApprovalChallenge(canon)
	writeJSON(w, http.StatusOK, proposalChallengeResponse{
		ProposalID: proposalID, Description: p.Description, Plan: p.Plan,
		Decision: decision, HeadSeq: headSeq, NodeID: s.nodeID,
		RPID: rpIDFromHost(r.Host),
		Challenge: base64.RawURLEncoding.EncodeToString(challenge[:]),
	})
}
