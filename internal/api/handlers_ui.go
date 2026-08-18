package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
	"github.com/avangerus/kalita/internal/transport"
)

// Handlers backing the generated UI (week 7): per-actor meta, workflow
// actions, the approval inbox and human task queues.

func (s *Server) meta(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	m := s.eng.MetaFor(actor.ID, actor.Role)
	// surface optional node capabilities the UI keys off (e.g. the search tab)
	out, _ := json.Marshal(m)
	var withCaps map[string]any
	_ = json.Unmarshal(out, &withCaps)
	withCaps["search"] = s.rag != nil
	// the Ask Kalita worker's advertised URL (deploy sets KALITA_BUILDER_URL;
	// empty → the UI derives a localhost default) — Product Skeptic BLOCKER-1
	// (server-side config, not a localStorage-only client hack)
	withCaps["builder_url"] = os.Getenv("KALITA_BUILDER_URL")
	writeJSON(w, http.StatusOK, withCaps)
}

// modelMeta is the FULL executable-model structure for the Company Model view
// (product finding F1): the definition approver sees every entity/role they
// sign — no record access is granted by this (per-entity can_* flags stay
// honest). Reserved to the definition approver role.
func (s *Server) modelMeta(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	if actor.Role != s.eng.DefinitionApprover() {
		writeJSON(w, http.StatusForbidden, &engine.Err{Code: engine.CodePermissionDenied,
			Message: "the full model view is reserved to the definition approver role",
			Rule:    "definition approver"})
		return
	}
	writeJSON(w, http.StatusOK, s.eng.ModelMeta(actor.ID, actor.Role))
}

func (s *Server) act(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	var req transport.ActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR", "message": "bad json"})
		return
	}
	res, err := s.eng.Act(r.Context(), actor, r.PathValue("entity"), r.PathValue("id"), req.Action, req.Basis, req.IdempotencyKey)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) journal(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	events, err := s.eng.Journal(r.Context(), actor, r.PathValue("entity"), r.PathValue("id"), 200)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(events))
	for _, ev := range events {
		out = append(out, map[string]any{
			"seq": ev.Seq, "ts": ev.TS.Format(time.RFC3339), "kind": ev.Kind,
			"actor": ev.Actor, "payload": json.RawMessage(ev.Payload), "basis": ev.Basis,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}

type revertRequest struct {
	ToSeq uint64            `json:"to_seq,omitempty"` // rewind target; 0/absent = revert the last change
	Basis *eventstore.Basis `json:"basis"`
}

// revert undoes a record's most recent change (compensating event). Supervisory:
// the engine gates it on `full`.
func (s *Server) revert(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	var req revertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR", "message": "bad json"})
		return
	}
	// REST idempotency is soft-mode / out of scope for PR-5 (LLD-01 §5) — pass
	// through empty, unchanged from pre-PR-5 behavior.
	rec, err := s.eng.Revert(r.Context(), actor, r.PathValue("entity"), r.PathValue("id"), req.Basis, "")
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// rewind restores a record to its state as of a chosen journal seq.
func (s *Server) rewind(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	var req revertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR", "message": "bad json"})
		return
	}
	// REST idempotency is soft-mode / out of scope for PR-5 (LLD-01 §5) — pass
	// through empty, unchanged from pre-PR-5 behavior.
	rec, err := s.eng.RewindTo(r.Context(), actor, r.PathValue("entity"), r.PathValue("id"), req.ToSeq, req.Basis, "")
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// diff reports the net change to a record between two journal seqs (read).
// ?from=<seq>&to=<seq>; to omitted/0 = now.
func (s *Server) diff(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	from, _ := strconv.ParseUint(r.URL.Query().Get("from"), 10, 64)
	to, _ := strconv.ParseUint(r.URL.Query().Get("to"), 10, 64)
	d, err := s.eng.Diff(r.Context(), actor, r.PathValue("entity"), r.PathValue("id"), from, to)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) approvals(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": s.eng.PendingApprovals(actor.Role)})
}

type decideRequest struct {
	Grant     bool              `json:"grant"`
	Signature []byte            `json:"signature,omitempty"`
	Basis     *eventstore.Basis `json:"basis"`
	// HumanSignature is the additive WebAuthn assertion (LLD-02 §1, §4): the
	// UI signs over the challenge from GET /api/approvals/{id}/challenge and
	// submits it here instead of (or alongside) the legacy raw Signature.
	HumanSignature *identity.HumanSignaturePayload `json:"human_signature,omitempty"`
}

func (s *Server) decide(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	var req decideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR", "message": "bad json"})
		return
	}
	approvalID := r.PathValue("id")
	var opts []engine.DecideOption
	if req.HumanSignature != nil {
		// SIGNATURE_REQUIRED-style rejection for a non-human actor trying to
		// submit a WebAuthn assertion: only a browser/SDK acting as a human
		// can ever produce one (LLD-02 §4, "MCP without human_signature is
		// rejected" — an agent WITH one makes even less sense and is refused
		// the same way).
		if actor.Type != eventstore.ActorHuman {
			writeJSON(w, http.StatusForbidden, &engine.Err{Code: engine.CodeSignatureRequired,
				Message: "only a human actor may submit a WebAuthn human_signature"})
			return
		}
		a, ok := s.eng.GetApproval(approvalID)
		if !ok {
			writeJSON(w, http.StatusNotFound, &engine.Err{Code: engine.CodeNotFound, Message: "approval " + approvalID + " not found"})
			return
		}
		ac, ok := s.challenges.getApproval(approvalID)
		if !ok || ac.actorID != actor.ID {
			writeJSON(w, http.StatusUnprocessableEntity, &engine.Err{Code: engine.CodeValidation,
				Message: "no outstanding challenge for this approval and actor",
				FixHint: "call GET /api/approvals/{id}/challenge first"})
			return
		}
		decision := identity.DecisionDeny
		if req.Grant {
			decision = identity.DecisionGrant
		}
		basisType, basisID := approvalCanonicalBasis(approvalID)
		canon := identity.BuildApprovalCanonical(approvalID, a.Entity, a.RecordID, a.Action, decision, basisType, basisID, ac.headSeq, s.nodeID)
		rpID := rpIDFromHost(r.Host)
		if verr := s.reg.VerifyApprovalSignature(r.Context(), actor.ID, req.HumanSignature, canon, rpID, originAllowed(r.Host), s.requireHumanSig); verr != nil {
			writeJSON(w, http.StatusForbidden, &engine.Err{Code: engine.CodePermissionDenied,
				Message: "human signature invalid: " + verr.Error()})
			return
		}
		raw, _ := json.Marshal(req.HumanSignature)
		opts = append(opts, engine.WithHumanSignature(raw, rpID, ac.headSeq))
	}
	rec, err := s.eng.Decide(r.Context(), actor, approvalID, req.Grant, req.Signature, req.Basis, opts...)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) tasks(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "open"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tasks": s.eng.Tasks(actor.Role, engine.TaskStatus(status)),
	})
}

// agentBoard: the agent workforce pipeline for a human operator — every task on
// an agent role, to be grouped client-side into waiting / working / done. Humans
// only; agents do not watch each other.
func (s *Server) agentBoard(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok || actor.Type != eventstore.ActorHuman {
		writeAuthRequired(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": s.eng.AgentTasks()})
}

// actors: the directory behind the Agents screen. Humans only — agents do
// not enumerate or manage each other.
func (s *Server) actors(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok || actor.Type != eventstore.ActorHuman {
		writeAuthRequired(w)
		return
	}
	if s.reg == nil {
		writeJSON(w, http.StatusOK, map[string]any{"actors": []any{}})
		return
	}
	list, err := s.reg.List(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"actors": list})
}

func (s *Server) disableActor(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok || actor.Type != eventstore.ActorHuman {
		writeAuthRequired(w)
		return
	}
	// D-014 (Cycle-A2): disabling an actor is a node-admin action — the v0
	// "any human disables anyone" model was a takeover/DoS vector (SEC-1).
	if actor.Role != s.eng.DefinitionApprover() {
		writeErr(w, &engine.Err{Code: engine.CodePermissionDenied,
			Message: "only the node admin (role " + s.eng.DefinitionApprover() + ") may disable an actor",
			Rule:    "identity admin boundary"})
		return
	}
	err := s.reg.Disable(r.Context(), actor, r.PathValue("id"),
		&eventstore.Basis{Type: "human", ID: actor.ID})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type inviteRequest struct {
	Role      string `json:"role"`
	Entity    string `json:"entity,omitempty"`
	RecordID  string `json:"record_id,omitempty"`
	BindField string `json:"bind_field,omitempty"`
}

// createInvite issues a one-time registration code (humans only).
func (s *Server) createInvite(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok || actor.Type != eventstore.ActorHuman {
		writeAuthRequired(w)
		return
	}
	var req inviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Role == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR",
			"message": "role is required", "fix_hint": `send {"role": "Customer", "entity": "Customer", "record_id": "..."}`})
		return
	}
	// D-014 (Cycle-A2): an invite carries a role grant. Granting the node-admin
	// role (defApprover) is reserved to the admin role itself — otherwise any
	// staff member could self-escalate to admin via the public /api/register.
	// Staff inviting staff/customers stays v0 (documented KNOWN LIMITATION;
	// a full role-grant model is a separate gated design).
	if req.Role == s.eng.DefinitionApprover() && actor.Role != s.eng.DefinitionApprover() {
		writeErr(w, &engine.Err{Code: engine.CodePermissionDenied,
			Message: "only the node admin (role " + s.eng.DefinitionApprover() + ") may issue invites for that role",
			Rule:    "identity admin boundary (admin-role invites)"})
		return
	}
	code, err := s.reg.CreateInvite(r.Context(), actor, req.Role, req.Entity, req.RecordID, req.BindField,
		&eventstore.Basis{Type: "human", ID: actor.ID})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"invite_code": code})
}

// register is the only PUBLIC endpoint: redeem an invite, become an actor.
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Invite string `json:"invite"`
		ID     string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Invite == "" || req.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR",
			"message": "invite and id are required", "fix_hint": `send {"invite": "<code>", "id": "you@example.com"}`})
		return
	}
	token, inv, err := s.reg.Redeem(r.Context(), req.ID, req.Invite)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"code": "INVITE_INVALID", "message": err.Error()})
		return
	}
	resp := map[string]any{"token": token, "role": inv.Role}
	// bind the record to the new actor (convention: a field holding the actor
	// id powers `where <field> = $me` row-level visibility in the pack)
	if inv.Entity != "" && inv.RecordID != "" {
		_, err := s.eng.Update(r.Context(), inv.CreatedBy, inv.Entity, inv.RecordID,
			map[string]any{inv.BindField: req.ID},
			&eventstore.Basis{Type: "human", ID: inv.CreatedBy.ID}, "")
		if err != nil {
			resp["bind_warning"] = err.Error() // registered, but the pack must bind manually
		} else {
			resp["bound"] = inv.Entity + "/" + inv.RecordID
		}
	}
	writeJSON(w, http.StatusCreated, resp)
}

// queryV2 is the rich query endpoint: a JSON body with the full condition
// language (where), multi-field sort, full-text search and pagination.
func (s *Server) queryV2(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	var req struct {
		Where  string         `json:"where"`
		Filter map[string]any `json:"filter"`
		Sort   []string       `json:"sort"`
		Search string         `json:"search"`
		Limit  int            `json:"limit"`
		Offset int            `json:"offset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR",
			"message": "bad json", "fix_hint": `{"where": "...", "sort": ["-created_at"], "search": "...", "limit": 25}`})
		return
	}
	if r.PathValue("entity") == coreUserEntity {
		s.serveCoreUserList(w, r, req.Search, req.Limit)
		return
	}
	rows, err := s.eng.Query(r.Context(), actor, r.PathValue("entity"), engine.QueryOpts{
		Where: req.Where, Filter: req.Filter, Sort: req.Sort, Search: req.Search,
		Limit: req.Limit, Offset: req.Offset,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": rows})
}

func (s *Server) proposals(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	// Architecture Skeptic MAJOR-1: proposal payloads carry the FULL
	// definition sources (permission/approval boundaries) — the history and
	// the inbox are reserved to the definition approver role, and the sources
	// themselves are never returned here (approver-gated get_definition is
	// the only read of raw sources).
	if actor.Role != s.eng.DefinitionApprover() {
		writeJSON(w, http.StatusForbidden, &engine.Err{Code: engine.CodePermissionDenied,
			Message: "proposals are visible to the definition approver role only",
			Rule:    "definition approver"})
		return
	}
	list := s.eng.PendingProposals()
	if r.URL.Query().Get("all") == "1" {
		// the model-history view (product mission C): pending + decided,
		// newest first (uuid v7 = timeline order)
		list = s.eng.Proposals()
	}
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		row := map[string]any{
			"id": p.ID, "description": p.Description, "author": p.Author,
			"base_def_version": p.BaseDefVersion, "plan": p.Plan,
			"status": p.Status, "reason": p.Reason,
			"created_at": p.CreatedAt,
		}
		if p.DecidedAt != nil {
			row["decided_at"] = p.DecidedAt
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposals": out})
}

func (s *Server) decideProposal(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	var req decideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR", "message": "bad json"})
		return
	}
	var opts []engine.DecideProposalOption
	if req.HumanSignature != nil {
		// Same ceremony shape as decide(): the UI signs over the challenge
		// from GET /api/proposals/{id}/challenge. Only a human actor may
		// submit a WebAuthn assertion (LLD-02 §4).
		if actor.Type != eventstore.ActorHuman {
			writeJSON(w, http.StatusForbidden, &engine.Err{Code: engine.CodeSignatureRequired,
				Message: "only a human actor may submit a WebAuthn human_signature"})
			return
		}
		proposalID := r.PathValue("id")
		p, err := s.eng.GetProposal(proposalID)
		if err != nil || p.Status != engine.ProposalPending {
			writeJSON(w, http.StatusNotFound, &engine.Err{Code: engine.CodeNotFound, Message: "proposal " + proposalID + " not found or not pending"})
			return
		}
		ac, ok := s.challenges.getProposal(proposalID)
		if !ok || ac.actorID != actor.ID {
			writeJSON(w, http.StatusUnprocessableEntity, &engine.Err{Code: engine.CodeValidation,
				Message: "no outstanding challenge for this proposal and actor",
				FixHint: "call GET /api/proposals/{id}/challenge first"})
			return
		}
		decision := identity.DecisionDeny
		if req.Grant {
			decision = identity.DecisionGrant
		}
		canon := identity.BuildDefinitionCanonical(proposalID, decision,
			engine.DefinitionContentHash(p.Files), ac.headSeq, s.nodeID)
		rpID := rpIDFromHost(r.Host)
		if verr := s.reg.VerifyApprovalSignature(r.Context(), actor.ID, req.HumanSignature, canon, rpID, originAllowed(r.Host), s.requireHumanSig); verr != nil {
			writeJSON(w, http.StatusForbidden, &engine.Err{Code: engine.CodePermissionDenied,
				Message: "human signature invalid: " + verr.Error()})
			return
		}
		raw, _ := json.Marshal(req.HumanSignature)
		opts = append(opts, engine.WithProposalHumanSignature(raw, rpID, ac.headSeq))
	}
	p, err := s.eng.DecideProposal(r.Context(), actor, r.PathValue("id"), req.Grant, req.Signature, req.Basis, opts...)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
