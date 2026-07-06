// WebAuthn/passkey verification (LLD-02): the credential registry (a fold of
// actor.credential_registered/_revoked/_warning), COSE key parsing, and the
// §3 assertion-verification order. The canonical string and challenge policy
// (LLD-02 §2) are NOT decided here — they are fixed by the spec and
// reproduced byte-for-byte by BuildApprovalCanonical/ApprovalChallenge.
//
// Crypto path summary (for review, per the caller's request):
//   - challenge = SHA-256(canonical), where canonical is rebuilt SERVER-SIDE
//     from the approval's own state (BuildApprovalCanonical) — the client's
//     clientDataJSON.challenge is only ever compared against it, never
//     trusted as an input to it (LLD-02 §2).
//   - the public key verified against comes from the actor's
//     actor.credential_registered event (COSE bytes, parsed by
//     parseCOSEKey) — never from the request.
//   - sign_count is compared but a regression only journals
//     actor.credential_warning; it never blocks (Apple/Google-synced
//     passkeys legitimately never advance the counter — LLD-02 §3.4).
//   - replay protection is structural, not challenge-randomness: the
//     canonical embeds the approval id (one-shot: Decide rejects an
//     already-decided approval) and head_seq/node_id, so a captured
//     assertion cannot be replayed into a second decision or a different
//     node (LLD-02 §2).
package identity

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"

	"github.com/fxamacker/cbor/v2"

	"github.com/avangerus/kalita/internal/eventstore"
)

// --- COSE algorithm/key-type identifiers (RFC 9053) ------------------------

const (
	coseAlgES256 = -7   // ECDSA w/ SHA-256 over P-256 — mandatory (LLD-02 §3.3)
	coseAlgRS256 = -257 // RSASSA-PKCS1-v1_5 w/ SHA-256 — allowed (LLD-02 §3.3)
	coseAlgES384 = -35  // accepted for PARSING only; verifyAssertion rejects it
	coseAlgEdDSA = -8   // accepted for PARSING only; verifyAssertion rejects it

	coseKtyEC2 = 2
	coseKtyRSA = 3
)

// parseCOSEKey decodes a COSE_Key CBOR map into a crypto public key and its
// COSE algorithm id. Only the key shapes WebAuthn authenticators actually
// produce are handled: EC2 (ES256/ES384) and RSA (RS256). Accepting
// ES384/EdDSA *parsing* here is not itself a security decision — the alg
// allowlist enforced in VerifyApprovalAssertion is what rejects them
// (acceptance test 9: "ES384/EdDSA -> reject unsupported alg"); parsing them
// only lets that rejection be tested against a well-formed key instead of a
// synthetically-broken one.
func parseCOSEKey(data []byte) (pub any, alg int64, err error) {
	var m map[int64]cbor.RawMessage
	if uerr := cbor.Unmarshal(data, &m); uerr != nil {
		return nil, 0, fmt.Errorf("identity: cose: %w", uerr)
	}
	var kty int64
	if raw, ok := m[1]; ok {
		if uerr := cbor.Unmarshal(raw, &kty); uerr != nil {
			return nil, 0, fmt.Errorf("identity: cose: kty: %w", uerr)
		}
	}
	if raw, ok := m[3]; ok {
		if uerr := cbor.Unmarshal(raw, &alg); uerr != nil {
			return nil, 0, fmt.Errorf("identity: cose: alg: %w", uerr)
		}
	}
	switch kty {
	case coseKtyEC2:
		var crv int64
		if raw, ok := m[-1]; ok {
			_ = cbor.Unmarshal(raw, &crv)
		}
		var x, y []byte
		if raw, ok := m[-2]; ok {
			_ = cbor.Unmarshal(raw, &x)
		}
		if raw, ok := m[-3]; ok {
			_ = cbor.Unmarshal(raw, &y)
		}
		var curve elliptic.Curve
		switch crv {
		case 1:
			curve = elliptic.P256()
		case 2:
			curve = elliptic.P384()
		case 3:
			curve = elliptic.P521()
		default:
			return nil, 0, fmt.Errorf("identity: cose: unsupported EC curve %d", crv)
		}
		if len(x) == 0 || len(y) == 0 {
			return nil, 0, errors.New("identity: cose: EC2 key missing x/y")
		}
		pk := &ecdsa.PublicKey{Curve: curve, X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		return pk, alg, nil
	case coseKtyRSA:
		var n, e []byte
		if raw, ok := m[-1]; ok {
			_ = cbor.Unmarshal(raw, &n)
		}
		if raw, ok := m[-2]; ok {
			_ = cbor.Unmarshal(raw, &e)
		}
		if len(n) == 0 || len(e) == 0 {
			return nil, 0, errors.New("identity: cose: RSA key missing n/e")
		}
		eInt := 0
		for _, b := range e {
			eInt = eInt<<8 | int(b)
		}
		pk := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: eInt}
		return pk, alg, nil
	default:
		return nil, 0, fmt.Errorf("identity: cose: unsupported kty %d", kty)
	}
}

// --- authenticatorData / attestationObject (WebAuthn wire structures) ------

const (
	flagUP = 1 << 0 // user presence
	flagUV = 1 << 2 // user verification
	flagAT = 1 << 6 // attested credential data present (registration only)
	flagED = 1 << 7 // extension data present
)

// parsedAuthData is authenticatorData decoded per the WebAuthn spec layout:
// rpIdHash(32) || flags(1) || signCount(4) || [attestedCredentialData].
type parsedAuthData struct {
	RPIDHash  []byte
	Flags     byte
	SignCount uint32
	AAGUID    []byte // set only when flagAT is present (registration)
	CredID    []byte
	COSEKey   []byte // raw COSE_Key CBOR bytes, set only when flagAT is present
}

func parseAuthenticatorData(raw []byte) (*parsedAuthData, error) {
	if len(raw) < 37 {
		return nil, errors.New("identity: webauthn: authenticatorData too short")
	}
	ad := &parsedAuthData{
		RPIDHash:  append([]byte(nil), raw[:32]...),
		Flags:     raw[32],
		SignCount: binary.BigEndian.Uint32(raw[33:37]),
	}
	rest := raw[37:]
	if ad.Flags&flagAT != 0 {
		if len(rest) < 18 {
			return nil, errors.New("identity: webauthn: authenticatorData: truncated attested credential data")
		}
		ad.AAGUID = append([]byte(nil), rest[:16]...)
		credIDLen := binary.BigEndian.Uint16(rest[16:18])
		rest = rest[18:]
		if len(rest) < int(credIDLen) {
			return nil, errors.New("identity: webauthn: authenticatorData: truncated credential id")
		}
		ad.CredID = append([]byte(nil), rest[:credIDLen]...)
		rest = rest[credIDLen:]
		// The COSE public key is the next well-formed CBOR item.
		// UnmarshalFirst tells us exactly where it ends, so trailing
		// extension data (flagED) is never mistaken for part of the key.
		var probe cbor.RawMessage
		tail, err := cbor.UnmarshalFirst(rest, &probe)
		if err != nil {
			return nil, fmt.Errorf("identity: webauthn: authenticatorData: credential public key: %w", err)
		}
		ad.COSEKey = append([]byte(nil), rest[:len(rest)-len(tail)]...)
	}
	return ad, nil
}

// attestationObject is the CBOR structure `navigator.credentials.create`
// returns. Its attStmt is deliberately not modeled/verified here: LLD-02 §4
// accepts "none" attestation as sufficient (the device itself is not
// attested) — only authData (which carries the credential public key) is
// used, regardless of fmt.
type attestationObject struct {
	Fmt      string          `cbor:"fmt"`
	AuthData []byte          `cbor:"authData"`
	AttStmt  cbor.RawMessage `cbor:"attStmt"`
}

func parseAttestationObject(raw []byte) (*attestationObject, error) {
	var ao attestationObject
	if err := cbor.Unmarshal(raw, &ao); err != nil {
		return nil, fmt.Errorf("identity: webauthn: attestationObject: %w", err)
	}
	return &ao, nil
}

// clientData is the JSON-encoded clientDataJSON the browser produces for
// both create() and get() calls.
type clientData struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Origin    string `json:"origin"`
}

func decodeChallenge(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

// --- credential registry (projection of actor.credential_* events) --------

// CredentialInfo is the current state of one WebAuthn credential, folded
// from the journal.
type CredentialInfo struct {
	ActorID       string   `json:"actor_id"`
	CredentialID  string   `json:"credential_id"` // base64url, matches the wire/event encoding
	PublicKeyCOSE []byte   `json:"public_key_cose,omitempty"`
	Alg           int64    `json:"alg"`
	AAGUID        []byte   `json:"aaguid,omitempty"`
	SignCount     uint32   `json:"sign_count"`
	Transports    []string `json:"transports,omitempty"`
	Label         string   `json:"label,omitempty"`
	Revoked       bool     `json:"revoked"`
}

// credentialState projects actor.credential_registered/_revoked/_warning
// events independently of the actor-directory fold in registry.go's all() —
// same separation-of-concerns pattern as authFailState/lockdown (each
// concern folds its own kinds, under its own lock, rather than bloating the
// hot Authenticate path). Unlike the journal-derived fields, SignCount is
// ALSO updated in-memory after every successful verification that does not
// itself produce a warning event (LLD-02 §3.4: "Обновить проекцию") — a
// deliberate, spec-sanctioned exception to strict replay-purity: only
// regressions are journaled, so a live counter is kept for the CURRENT
// process to catch a LATER regression, understanding that a restart resets
// it to the last durable value (registration or last warning).
type credentialState struct {
	mu        sync.Mutex
	watermark uint64
	byID      map[string]*CredentialInfo
}

type credentialRegisteredPayload struct {
	CredentialID  string   `json:"credential_id"`
	PublicKeyCOSE []byte   `json:"public_key_cose"`
	Alg           int64    `json:"alg"`
	AAGUID        []byte   `json:"aaguid,omitempty"`
	SignCount     uint32   `json:"sign_count"`
	Transports    []string `json:"transports,omitempty"`
	Label         string   `json:"label,omitempty"`
}

type credentialRevokedPayload struct {
	CredentialID string `json:"credential_id"`
	Reason       string `json:"reason,omitempty"`
}

type credentialWarningPayload struct {
	CredentialID string `json:"credential_id"`
	Kind         string `json:"kind"` // "sign_count_regression"
	Observed     uint32 `json:"observed"`
	Expected     uint32 `json:"expected"`
}

var (
	ErrCredentialUnknown = errors.New("identity: credential not registered")
	ErrCredentialRevoked = errors.New("identity: credential is revoked")
)

// RegisterCredential appends actor.credential_registered (LLD-02 §1).
// Several credentials per actor is normal (a laptop, a phone, a recovery
// key) — no uniqueness beyond the credential id itself is enforced.
func (r *Registry) RegisterCredential(ctx context.Context, actor eventstore.Actor, credentialID string, publicKeyCOSE []byte, alg int64, aaguid []byte, signCount uint32, transports []string, label string, basis *eventstore.Basis) error {
	payload, err := json.Marshal(credentialRegisteredPayload{
		CredentialID: credentialID, PublicKeyCOSE: publicKeyCOSE, Alg: alg,
		AAGUID: aaguid, SignCount: signCount, Transports: transports, Label: label,
	})
	if err != nil {
		return err
	}
	_, err = r.store.Append(ctx, eventstore.AppendInput{
		Actor:   actor,
		Kind:    eventstore.ActorCredentialRegistered,
		SchemaV: eventstore.CurrentSchemaV,
		Subject: eventstore.Subject{ActorID: actor.ID},
		Payload: payload,
		Basis:   basis,
	})
	return err
}

// RevokeCredential appends actor.credential_revoked. registrar may be the
// actor themselves (self-revoke on device loss) or an admin — revoking one's
// OWN credential is not a privileged action, unlike Register/Disable.
func (r *Registry) RevokeCredential(ctx context.Context, registrar eventstore.Actor, actorID, credentialID, reason string, basis *eventstore.Basis) error {
	payload, err := json.Marshal(credentialRevokedPayload{CredentialID: credentialID, Reason: reason})
	if err != nil {
		return err
	}
	_, err = r.store.Append(ctx, eventstore.AppendInput{
		Actor:   registrar,
		Kind:    eventstore.ActorCredentialRevoked,
		SchemaV: eventstore.CurrentSchemaV,
		Subject: eventstore.Subject{ActorID: actorID},
		Payload: payload,
		Basis:   basis,
	})
	return err
}

// Credentials returns actorID's registered credentials (including revoked
// ones, marked Revoked=true — callers filter as needed for display).
func (r *Registry) Credentials(ctx context.Context, actorID string) ([]*CredentialInfo, error) {
	all, err := r.credentials(ctx)
	if err != nil {
		return nil, err
	}
	var out []*CredentialInfo
	for _, c := range all {
		if c.ActorID == actorID {
			cp := *c
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *Registry) credentialByID(ctx context.Context, credentialID string) (*CredentialInfo, error) {
	all, err := r.credentials(ctx)
	if err != nil {
		return nil, err
	}
	c, ok := all[credentialID]
	if !ok {
		return nil, ErrCredentialUnknown
	}
	cp := *c
	return &cp, nil
}

// credentials folds every actor.credential_* event since the last watermark
// into the live registry, mirroring Registry.all()'s incremental-cache shape
// but scoped to its own credState (see credentialState doc).
func (r *Registry) credentials(ctx context.Context) (map[string]*CredentialInfo, error) {
	r.credState.mu.Lock()
	defer r.credState.mu.Unlock()
	if r.credState.byID == nil {
		r.credState.byID = map[string]*CredentialInfo{}
	}
	events, err := r.store.Since(ctx, r.credState.watermark)
	if err != nil {
		return nil, err
	}
	for _, e := range events {
		r.credState.watermark = e.Seq
	}
	foldCredentialEvents(events, r.credState.byID)
	out := make(map[string]*CredentialInfo, len(r.credState.byID))
	for k, v := range r.credState.byID {
		cp := *v
		out[k] = &cp
	}
	return out, nil
}

// foldCredentialEvents applies actor.credential_* events onto byID. Shared
// by the live incremental fold above AND FoldCredentialsFromEvents (LLD-02
// §5, `kalita verify` — an entirely offline caller with a plain event
// slice, no Registry/Store involved) so the two never drift apart.
func foldCredentialEvents(events []*eventstore.Event, byID map[string]*CredentialInfo) {
	for _, e := range events {
		switch e.Kind {
		case eventstore.ActorCredentialRegistered:
			var p credentialRegisteredPayload
			if json.Unmarshal(e.Payload, &p) != nil {
				continue
			}
			byID[p.CredentialID] = &CredentialInfo{
				ActorID: e.Subject.ActorID, CredentialID: p.CredentialID,
				PublicKeyCOSE: p.PublicKeyCOSE, Alg: p.Alg, AAGUID: p.AAGUID,
				SignCount: p.SignCount, Transports: p.Transports, Label: p.Label,
			}
		case eventstore.ActorCredentialRevoked:
			var p credentialRevokedPayload
			if json.Unmarshal(e.Payload, &p) != nil {
				continue
			}
			if c, ok := byID[p.CredentialID]; ok {
				c.Revoked = true
			}
		case eventstore.ActorCredentialWarning:
			var p credentialWarningPayload
			if json.Unmarshal(e.Payload, &p) != nil {
				continue
			}
			if c, ok := byID[p.CredentialID]; ok {
				c.SignCount = p.Observed
			}
		}
	}
}

// FoldCredentialsFromEvents rebuilds the credential registry purely from a
// journal slice — no live Registry, no Store.Since watermark. This is what
// `kalita verify` (LLD-02 §5) uses: exactly what a third party auditing a
// pg_dump copy on a different machine has available.
func FoldCredentialsFromEvents(events []*eventstore.Event) map[string]*CredentialInfo {
	byID := map[string]*CredentialInfo{}
	foldCredentialEvents(events, byID)
	return byID
}

// updateSignCount live-updates the in-memory sign_count after a successful
// verification (see credentialState doc: sanctioned exception to strict
// replay-purity, LLD-02 §3.4 "Обновить проекцию").
func (r *Registry) updateSignCount(credentialID string, observed uint32) {
	r.credState.mu.Lock()
	defer r.credState.mu.Unlock()
	if c, ok := r.credState.byID[credentialID]; ok {
		c.SignCount = observed
	}
}

// --- canonical string / challenge (LLD-02 §2 — fixed, not Sonnet's call) --

// ApprovalCanonicalDomain is the fixed first line of the canonical string.
// Changing it invalidates every offline-verifiable signature ever produced
// — a named constant, never inlined, so the risk of an accidental edit is
// visible in review.
const ApprovalCanonicalDomain = "kalita-approval-v1"

// DecisionGrant / DecisionDeny are the two literal tokens the canonical
// string's decision line may hold (LLD-02 §2) — nothing else.
const (
	DecisionGrant = "grant"
	DecisionDeny  = "deny"
)

// BuildApprovalCanonical reproduces the exact byte layout fixed by LLD-02 §2:
//
//	"kalita-approval-v1" NL
//	task_id NL
//	entity "/" record_id NL
//	transition NL
//	("grant" | "deny") NL
//	basis_type ":" basis_id NL
//	head_seq NL
//	node_id
//
// taskID is the approval id: there is no separate "task" entity for
// approvals in this codebase (Task is a different, unrelated queue concept —
// see internal/engine/task.go), so "the unit of work the human is asked to
// decide" is the approval itself. This mapping is an implementation
// necessity, not a reinterpretation of the format.
//
// The caller MUST supply every value from the server's OWN state (the
// approval record, the basis attached to the decision, the head_seq captured
// when the challenge was issued, and the node's own public key) — never
// anything echoed back by the client (LLD-02 §2: "не доверяет клиентскому
// пересказу").
func BuildApprovalCanonical(taskID, entity, recordID, transition, decision, basisType, basisID string, headSeq uint64, nodeID string) []byte {
	var b strings.Builder
	b.WriteString(ApprovalCanonicalDomain)
	b.WriteByte('\n')
	b.WriteString(taskID)
	b.WriteByte('\n')
	b.WriteString(entity)
	b.WriteByte('/')
	b.WriteString(recordID)
	b.WriteByte('\n')
	b.WriteString(transition)
	b.WriteByte('\n')
	b.WriteString(decision)
	b.WriteByte('\n')
	b.WriteString(basisType)
	b.WriteByte(':')
	b.WriteString(basisID)
	b.WriteByte('\n')
	b.WriteString(strconv.FormatUint(headSeq, 10))
	b.WriteByte('\n')
	b.WriteString(nodeID)
	return []byte(b.String())
}

// ApprovalChallenge is SHA-256(canonical) — the 32-byte challenge a passkey
// signs over (LLD-02 §2).
func ApprovalChallenge(canonical []byte) [32]byte {
	return sha256.Sum256(canonical)
}

// --- human_signature payload (additive approval.decided field) -----------

// HumanSignaturePayload is the additive approval.decided payload field
// (LLD-02 §1): the WebAuthn assertion a human produced when signing a
// decision. It never contains a private key or raw secret — only what the
// authenticator returned, which is exactly what an offline verifier
// (kalita verify, LLD-02 §5) needs to redo every check in VerifyApprovalAssertion.
type HumanSignaturePayload struct {
	CredentialID      string `json:"credential_id"` // base64url
	AuthenticatorData []byte `json:"authenticator_data"`
	ClientDataJSON    []byte `json:"client_data_json"`
	Signature         []byte `json:"signature"`
}

// --- §3 assertion verification (the crypto path under review) ------------

// VerifyApprovalAssertion implements LLD-02 §3 in the mandated order:
//  1. clientDataJSON: type == "webauthn.get", challenge == SHA-256(canonical), origin allowed.
//  2. authenticatorData: rpIdHash, user presence (UP), user verification (UV) if required.
//  3. signature: ES256 mandatory, RS256 allowed, anything else rejected.
//  4. sign_count: a regression is reported to the caller, never blocks here.
//
// Step 5 of §3 (revoked credential -> reject before all else) is NOT this
// function's job: it has no actor/revocation context on its own. See
// Registry.VerifyApprovalSignature, which wires the credential lookup and
// revocation check around this pure function, in that order.
//
// canonical MUST be what the SERVER reconstructed (BuildApprovalCanonical),
// never the client's echo. rpID is the relying party id in effect for this
// request (journaled alongside human_signature — LLD-02 §5). originOK
// reports whether an origin string is acceptable — callers pass the same
// same-origin policy api.Secure already enforces. requireUV, when true,
// rejects an assertion without the UV flag (LLD-02 §5: required under
// --require-human-signature enforcement).
//
// Returns signCountRegressed=true when the observed sign_count is a
// regression the caller should journal as actor.credential_warning.
func VerifyApprovalAssertion(cred *CredentialInfo, hs *HumanSignaturePayload, canonical []byte, rpID string, originOK func(string) bool, requireUV bool) (signCountRegressed bool, observedSignCount uint32, err error) {
	// 1. clientDataJSON
	var cd clientData
	if uerr := json.Unmarshal(hs.ClientDataJSON, &cd); uerr != nil {
		return false, 0, fmt.Errorf("identity: webauthn: bad clientDataJSON: %w", uerr)
	}
	if cd.Type != "webauthn.get" {
		return false, 0, fmt.Errorf("identity: webauthn: clientDataJSON.type = %q, want webauthn.get", cd.Type)
	}
	wantChallenge := ApprovalChallenge(canonical)
	gotChallenge, derr := decodeChallenge(cd.Challenge)
	if derr != nil {
		return false, 0, fmt.Errorf("identity: webauthn: bad challenge encoding: %w", derr)
	}
	if !bytes.Equal(gotChallenge, wantChallenge[:]) {
		return false, 0, errors.New("identity: webauthn: challenge mismatch")
	}
	if originOK == nil || !originOK(cd.Origin) {
		return false, 0, fmt.Errorf("identity: webauthn: origin %q not allowed", cd.Origin)
	}

	// 2. authenticatorData
	ad, aerr := parseAuthenticatorData(hs.AuthenticatorData)
	if aerr != nil {
		return false, 0, aerr
	}
	wantRPIDHash := sha256.Sum256([]byte(rpID))
	if !bytes.Equal(ad.RPIDHash, wantRPIDHash[:]) {
		return false, 0, errors.New("identity: webauthn: rpIdHash mismatch")
	}
	if ad.Flags&flagUP == 0 {
		return false, 0, errors.New("identity: webauthn: user presence flag not set")
	}
	if requireUV && ad.Flags&flagUV == 0 {
		return false, 0, errors.New("identity: webauthn: user verification required but not performed")
	}

	// 3. signature: authenticatorData || SHA-256(clientDataJSON)
	pub, _, perr := parseCOSEKey(cred.PublicKeyCOSE)
	if perr != nil {
		return false, 0, perr
	}
	clientDataHash := sha256.Sum256(hs.ClientDataJSON)
	signedMsg := append(append([]byte{}, hs.AuthenticatorData...), clientDataHash[:]...)
	if verr := verifyAssertionSignature(pub, cred.Alg, signedMsg, hs.Signature); verr != nil {
		return false, 0, verr
	}

	// 4. sign_count: regression only if BOTH sides are non-zero (synced
	// passkeys legitimately report 0 forever) and the new value did not
	// strictly advance.
	regressed := ad.SignCount != 0 && cred.SignCount != 0 && ad.SignCount <= cred.SignCount
	return regressed, ad.SignCount, nil
}

// verifyAssertionSignature enforces the algorithm allowlist AND performs the
// verification: ES256 (mandatory) and RS256 (allowed) only. Any other COSE
// alg — including ES384 and EdDSA, which parseCOSEKey can decode fine — is
// rejected here with "unsupported alg" (acceptance test 9).
func verifyAssertionSignature(pub any, alg int64, msg, sig []byte) error {
	sum := sha256.Sum256(msg)
	switch alg {
	case coseAlgES256:
		ecpub, ok := pub.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("identity: webauthn: alg ES256 needs an EC2 key, got %T", pub)
		}
		if !ecdsa.VerifyASN1(ecpub, sum[:], sig) {
			return errors.New("identity: webauthn: ES256 signature does not verify")
		}
		return nil
	case coseAlgRS256:
		rsapub, ok := pub.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("identity: webauthn: alg RS256 needs an RSA key, got %T", pub)
		}
		if err := rsa.VerifyPKCS1v15(rsapub, crypto.SHA256, sum[:], sig); err != nil {
			return fmt.Errorf("identity: webauthn: RS256 signature does not verify: %w", err)
		}
		return nil
	case coseAlgES384, coseAlgEdDSA:
		return fmt.Errorf("identity: webauthn: unsupported alg %d (only ES256/RS256 accepted)", alg)
	default:
		return fmt.Errorf("identity: webauthn: unsupported alg %d (only ES256/RS256 accepted)", alg)
	}
}

// VerifyApprovalSignature is the entry point the API layer calls to verify a
// human_signature before letting a decision through. It enforces LLD-02 §3
// step 5 (revoked -> reject before all else) by checking BEFORE calling
// VerifyApprovalAssertion, looks up the credential by id scoped to actorID
// (a credential registered to one actor cannot sign for another), and on
// success updates the live sign_count / journals a warning on regression —
// but still returns success (LLD-02 §3.4: a regression never blocks).
func (r *Registry) VerifyApprovalSignature(ctx context.Context, actorID string, hs *HumanSignaturePayload, canonical []byte, rpID string, originOK func(string) bool, requireUV bool) error {
	cred, err := r.credentialByID(ctx, hs.CredentialID)
	if err != nil {
		return err
	}
	if cred.ActorID != actorID {
		return ErrCredentialUnknown
	}
	if cred.Revoked {
		return ErrCredentialRevoked
	}
	regressed, observed, verr := VerifyApprovalAssertion(cred, hs, canonical, rpID, originOK, requireUV)
	if verr != nil {
		return verr
	}
	if regressed {
		payload, _ := json.Marshal(credentialWarningPayload{
			CredentialID: cred.CredentialID, Kind: "sign_count_regression",
			Observed: observed, Expected: cred.SignCount,
		})
		_, _ = r.store.Append(ctx, eventstore.AppendInput{
			Actor:   eventstore.Actor{Type: eventstore.ActorSystem, ID: "node"},
			Kind:    eventstore.ActorCredentialWarning,
			SchemaV: eventstore.CurrentSchemaV,
			Subject: eventstore.Subject{ActorID: actorID},
			Payload: payload,
			Basis:   &eventstore.Basis{Type: "rule", ID: "webauthn-sign-count"},
		})
	}
	r.updateSignCount(cred.CredentialID, observed)
	return nil
}

// --- registration (§4) ------------------------------------------------

// VerifyRegistration validates a WebAuthn registration response (LLD-02 §4)
// and returns the pieces the caller journals via RegisterCredential.
// Attestation is NOT cryptographically verified (attStmt is ignored
// regardless of fmt) — LLD-02 §6 accepts "none" attestation as sufficient
// since the device itself is not attested; only that the browser created a
// credential bound to THIS origin/rpId/challenge is checked.
func VerifyRegistration(clientDataJSON, attestationObj, expectedChallenge []byte, rpID string, originOK func(string) bool) (credentialID string, publicKeyCOSE []byte, alg int64, aaguid []byte, signCount uint32, err error) {
	var cd clientData
	if uerr := json.Unmarshal(clientDataJSON, &cd); uerr != nil {
		return "", nil, 0, nil, 0, fmt.Errorf("identity: webauthn: bad clientDataJSON: %w", uerr)
	}
	if cd.Type != "webauthn.create" {
		return "", nil, 0, nil, 0, fmt.Errorf("identity: webauthn: clientDataJSON.type = %q, want webauthn.create", cd.Type)
	}
	gotChallenge, derr := decodeChallenge(cd.Challenge)
	if derr != nil || !bytes.Equal(gotChallenge, expectedChallenge) {
		return "", nil, 0, nil, 0, errors.New("identity: webauthn: registration challenge mismatch")
	}
	if originOK == nil || !originOK(cd.Origin) {
		return "", nil, 0, nil, 0, fmt.Errorf("identity: webauthn: origin %q not allowed", cd.Origin)
	}
	ao, aerr := parseAttestationObject(attestationObj)
	if aerr != nil {
		return "", nil, 0, nil, 0, aerr
	}
	ad, aderr := parseAuthenticatorData(ao.AuthData)
	if aderr != nil {
		return "", nil, 0, nil, 0, aderr
	}
	wantRPIDHash := sha256.Sum256([]byte(rpID))
	if !bytes.Equal(ad.RPIDHash, wantRPIDHash[:]) {
		return "", nil, 0, nil, 0, errors.New("identity: webauthn: rpIdHash mismatch")
	}
	if ad.Flags&flagUP == 0 {
		return "", nil, 0, nil, 0, errors.New("identity: webauthn: user presence flag not set")
	}
	if ad.COSEKey == nil {
		return "", nil, 0, nil, 0, errors.New("identity: webauthn: no attested credential public key in authenticatorData")
	}
	_, parsedAlg, perr := parseCOSEKey(ad.COSEKey)
	if perr != nil {
		return "", nil, 0, nil, 0, perr
	}
	return base64.RawURLEncoding.EncodeToString(ad.CredID), ad.COSEKey, parsedAlg, ad.AAGUID, ad.SignCount, nil
}

// --- offline re-verification: `kalita verify` (LLD-02 §5) -----------------

// approvalEventPayload is the JSON shape kalita verify needs from
// approval.requested/granted/rejected payloads — a read-only mirror of
// internal/engine's private approvalPayload (field names/tags kept in sync
// by hand: engine owns writing them, this package only reads them back for
// offline audit). Decoding by field name here avoids a reverse import of
// internal/engine from this lower-level package.
type approvalEventPayload struct {
	Entity         string          `json:"entity"`
	RecordID       string          `json:"record_id"`
	Action         string          `json:"action"`
	HumanSignature json.RawMessage `json:"human_signature,omitempty"`
	RPID           string          `json:"rp_id,omitempty"`
	HeadSeq        uint64          `json:"head_seq,omitempty"`
}

// VerifyApprovalSignaturesOffline re-verifies every human_signature found on
// approval.granted/approval.rejected events made by a human actor, purely
// from a journal slice and the node's own public key — LLD-02 §5's whole
// point: "history with evidentiary force", checkable by a third party on a
// pg_dump copy with no access to the live server. It rebuilds the credential
// registry AND the approval-requested lookup from the SAME slice (never a
// live Registry/Store), exactly what such an auditor has available.
//
// The origin check from VerifyApprovalAssertion §3.1 is skipped here on
// purpose (LLD-02 §5: "офлайн нет rpId? ... кроме origin") — there is no
// live request to compare a captured origin against; rpId itself IS
// re-checked (it travels in the payload precisely so this is possible).
//
// requireSignedApprovals implements `--require-signed-approvals` (LLD-02
// §5): when true, a human decision WITHOUT a human_signature is itself
// reported as a problem — this is what catches a token-approve slipped into
// a journal that is supposed to be passkey-only.
//
// Returns the number of human decisions examined and one problem string per
// issue found (empty means clean); this function itself never errors —
// every parse/verify failure becomes one entry in the returned slice so the
// caller can report ALL of them in one pass instead of stopping at the
// first.
func VerifyApprovalSignaturesOffline(events []*eventstore.Event, nodeID string, requireSignedApprovals bool) (checked int, problems []string) {
	creds := FoldCredentialsFromEvents(events)
	requested := map[string]approvalEventPayload{}
	for _, e := range events {
		if e.Kind != eventstore.ApprovalRequested {
			continue
		}
		var p approvalEventPayload
		if json.Unmarshal(e.Payload, &p) != nil {
			continue
		}
		requested[e.Subject.ApprovalID] = p
	}

	for _, e := range events {
		if e.Kind != eventstore.ApprovalGranted && e.Kind != eventstore.ApprovalRejected {
			continue
		}
		if e.Actor.Type != eventstore.ActorHuman {
			continue // LLD-02 scope is human non-repudiation; agent decisions are Ed25519, unrelated
		}
		checked++
		approvalID := e.Subject.ApprovalID
		var p approvalEventPayload
		if json.Unmarshal(e.Payload, &p) != nil {
			problems = append(problems, fmt.Sprintf("approval %s: payload does not parse", approvalID))
			continue
		}
		if len(p.HumanSignature) == 0 {
			if requireSignedApprovals {
				problems = append(problems, fmt.Sprintf("approval %s: token-only decision (no human_signature) found under --require-signed-approvals", approvalID))
			}
			continue
		}
		var hs HumanSignaturePayload
		if json.Unmarshal(p.HumanSignature, &hs) != nil {
			problems = append(problems, fmt.Sprintf("approval %s: human_signature does not parse", approvalID))
			continue
		}
		cred, ok := creds[hs.CredentialID]
		if !ok {
			problems = append(problems, fmt.Sprintf("approval %s: credential %s not found in journal", approvalID, hs.CredentialID))
			continue
		}
		if cred.ActorID != e.Actor.ID {
			problems = append(problems, fmt.Sprintf("approval %s: credential %s belongs to a different actor than the decision", approvalID, hs.CredentialID))
			continue
		}
		// NOTE: cred.Revoked is deliberately NOT checked here. Revocation
		// (LLD-02 §3.5) is a forward-looking gate against FUTURE decisions —
		// it says nothing about whether a signature made while the
		// credential was still valid was legitimate. Flagging every past
		// decision from a NOW-revoked credential (e.g. an employee who lost
		// a laptop and revoked it) would misreport ordinary operational
		// history as tampering. `kalita verify` only checks the math: did
		// THIS signature verify against the key that was registered at the
		// time.
		req, ok := requested[approvalID]
		if !ok {
			problems = append(problems, fmt.Sprintf("approval %s: no matching approval.requested event", approvalID))
			continue
		}
		decision := DecisionDeny
		if e.Kind == eventstore.ApprovalGranted {
			decision = DecisionGrant
		}
		basisType, basisID := "approval", approvalID // fixed, self-referential — see internal/api's approvalCanonicalBasis
		canon := BuildApprovalCanonical(approvalID, req.Entity, req.RecordID, req.Action, decision, basisType, basisID, p.HeadSeq, nodeID)
		allowAnyOrigin := func(string) bool { return true } // no live request to check origin against (LLD-02 §5)
		if _, _, verr := VerifyApprovalAssertion(cred, &hs, canon, p.RPID, allowAnyOrigin, false); verr != nil {
			problems = append(problems, fmt.Sprintf("approval %s: signature does not verify: %v", approvalID, verr))
		}
	}
	return checked, problems
}
