package identity

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Synthetic test-vector helpers (LLD-02 §6: "сгенерировать фиксированные
// assertion-байты хелпером на cose/ecdsa в тестах, без браузера"). These
// build byte-for-byte real WebAuthn wire structures (COSE_Key CBOR,
// authenticatorData, clientDataJSON) so the tests exercise the actual parser
// and verifier, not a shortcut.

const testRPID = "kalita.local"
const testOrigin = "https://kalita.local"

func testOriginOK(origin string) bool { return origin == testOrigin }

// coseECBytes CBOR-encodes an EC2 COSE_Key with an arbitrary alg id, so tests
// can construct otherwise-valid-looking ES384 keys to prove the alg
// allowlist (not the parser) is what rejects them.
func coseECBytes(t *testing.T, pub *ecdsa.PublicKey, crv, alg int) []byte {
	t.Helper()
	size := (pub.Curve.Params().BitSize + 7) / 8
	x := pub.X.FillBytes(make([]byte, size))
	y := pub.Y.FillBytes(make([]byte, size))
	m := map[int]interface{}{1: 2, 3: alg, -1: crv, -2: x, -3: y}
	enc, err := cbor.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return enc
}

func coseRSABytes(t *testing.T, pub *rsa.PublicKey, alg int) []byte {
	t.Helper()
	n := pub.N.Bytes()
	e := big.NewInt(int64(pub.E)).Bytes()
	m := map[int]interface{}{1: 3, 3: alg, -1: n, -2: e}
	enc, err := cbor.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return enc
}

// buildAuthData constructs a bare (no attested credential data) authData
// blob, exactly what a GET/assertion ceremony returns.
func buildAuthData(rpID string, flags byte, signCount uint32) []byte {
	h := sha256.Sum256([]byte(rpID))
	var sc [4]byte
	binary.BigEndian.PutUint32(sc[:], signCount)
	out := append([]byte{}, h[:]...)
	out = append(out, flags)
	out = append(out, sc[:]...)
	return out
}

func buildClientDataJSON(typ string, challenge [32]byte, origin string) []byte {
	cd := clientData{
		Type:      typ,
		Challenge: base64.RawURLEncoding.EncodeToString(challenge[:]),
		Origin:    origin,
	}
	raw, _ := json.Marshal(cd)
	return raw
}

func signES256(t *testing.T, priv *ecdsa.PrivateKey, authData, clientDataJSON []byte) []byte {
	t.Helper()
	cdHash := sha256.Sum256(clientDataJSON)
	msg := append(append([]byte{}, authData...), cdHash[:]...)
	sum := sha256.Sum256(msg)
	sig, err := ecdsa.SignASN1(rand.Reader, priv, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

func signRS256(t *testing.T, priv *rsa.PrivateKey, authData, clientDataJSON []byte) []byte {
	t.Helper()
	cdHash := sha256.Sum256(clientDataJSON)
	msg := append(append([]byte{}, authData...), cdHash[:]...)
	sum := sha256.Sum256(msg)
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

// testFixture registers one ES256 credential for actorID and returns
// everything needed to produce a valid signed decision, plus the canonical
// fields used to build it — tests mutate ONE field at a time from there.
type testFixture struct {
	reg          *Registry
	actorID      string
	credentialID string
	priv         *ecdsa.PrivateKey

	taskID, entity, recordID, transition, decision, basisType, basisID, nodeID string
	headSeq                                                                    uint64
}

func newWebauthnFixture(t *testing.T, signCount uint32) *testFixture {
	t.Helper()
	reg := newRegistry()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cose := coseECBytes(t, &priv.PublicKey, 1, coseAlgES256)
	credID := base64.RawURLEncoding.EncodeToString([]byte("cred-" + t.Name()))
	actor := eventstore.Actor{Type: eventstore.ActorHuman, ID: "fin-dir", Role: "FinDirector"}
	if err := reg.RegisterCredential(ctx, actor, credID, cose, coseAlgES256, nil, signCount, []string{"internal"}, "test laptop", &eventstore.Basis{Type: "human", ID: "fin-dir"}); err != nil {
		t.Fatal(err)
	}
	return &testFixture{
		reg: reg, actorID: actor.ID, credentialID: credID, priv: priv,
		taskID: "appr-1", entity: "Debtor", recordID: "d-1", transition: "escalate",
		decision: DecisionGrant, basisType: "approval", basisID: "appr-1", nodeID: "node-pub-abc",
		headSeq: 42,
	}
}

func (f *testFixture) canonical() []byte {
	return BuildApprovalCanonical(f.taskID, f.entity, f.recordID, f.transition, f.decision, f.basisType, f.basisID, f.headSeq, f.nodeID)
}

func (f *testFixture) validAssertion(t *testing.T, flags byte, signCount uint32) *HumanSignaturePayload {
	t.Helper()
	canon := f.canonical()
	challenge := ApprovalChallenge(canon)
	authData := buildAuthData(testRPID, flags, signCount)
	cdJSON := buildClientDataJSON("webauthn.get", challenge, testOrigin)
	sig := signES256(t, f.priv, authData, cdJSON)
	return &HumanSignaturePayload{
		CredentialID: f.credentialID, AuthenticatorData: authData,
		ClientDataJSON: cdJSON, Signature: sig,
	}
}

// --- acceptance test 1: register -> sign -> verify round-trip (ES256) -----

func TestAcceptance1_RoundTrip(t *testing.T) {
	f := newWebauthnFixture(t, 0)
	hs := f.validAssertion(t, flagUP|flagUV, 1)
	if err := f.reg.VerifyApprovalSignature(ctx, f.actorID, hs, f.canonical(), testRPID, testOriginOK, true); err != nil {
		t.Fatalf("valid ES256 assertion must verify: %v", err)
	}
}

// --- acceptance test 3: tampering any canonical field breaks the challenge -

func TestAcceptance3_CanonicalTamperBreaksChallenge(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(f *testFixture)
	}{
		{"task_id", func(f *testFixture) { f.taskID = "appr-EVIL" }},
		{"grant_to_deny", func(f *testFixture) { f.decision = DecisionDeny }},
		{"head_seq", func(f *testFixture) { f.headSeq = 999 }},
		{"node_id", func(f *testFixture) { f.nodeID = "node-pub-EVIL" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newWebauthnFixture(t, 0)
			// sign over the ORIGINAL (untampered) canonical/challenge
			hs := f.validAssertion(t, flagUP|flagUV, 1)
			// now verify against a canonical with one field changed: the
			// server reconstructs a DIFFERENT canonical, so the challenge in
			// clientDataJSON no longer matches SHA-256(canonical).
			c.mutate(f)
			err := f.reg.VerifyApprovalSignature(ctx, f.actorID, hs, f.canonical(), testRPID, testOriginOK, true)
			if err == nil {
				t.Fatalf("tampered %s must break the challenge and reject", c.name)
			}
			if !strings.Contains(err.Error(), "challenge mismatch") {
				t.Fatalf("want challenge mismatch, got: %v", err)
			}
		})
	}
}

// --- acceptance test 4: wrong origin / rpIdHash / missing UP -> reject ----

func TestAcceptance4_OriginRPIDUPRejections(t *testing.T) {
	t.Run("bad_origin", func(t *testing.T) {
		f := newWebauthnFixture(t, 0)
		canon := f.canonical()
		challenge := ApprovalChallenge(canon)
		authData := buildAuthData(testRPID, flagUP|flagUV, 1)
		cdJSON := buildClientDataJSON("webauthn.get", challenge, "https://evil.example")
		sig := signES256(t, f.priv, authData, cdJSON)
		hs := &HumanSignaturePayload{CredentialID: f.credentialID, AuthenticatorData: authData, ClientDataJSON: cdJSON, Signature: sig}
		if err := f.reg.VerifyApprovalSignature(ctx, f.actorID, hs, canon, testRPID, testOriginOK, true); err == nil {
			t.Fatal("wrong origin must reject")
		}
	})
	t.Run("bad_rpid_hash", func(t *testing.T) {
		f := newWebauthnFixture(t, 0)
		canon := f.canonical()
		challenge := ApprovalChallenge(canon)
		authData := buildAuthData("evil.example", flagUP|flagUV, 1) // wrong rpID baked into the hash
		cdJSON := buildClientDataJSON("webauthn.get", challenge, testOrigin)
		sig := signES256(t, f.priv, authData, cdJSON)
		hs := &HumanSignaturePayload{CredentialID: f.credentialID, AuthenticatorData: authData, ClientDataJSON: cdJSON, Signature: sig}
		if err := f.reg.VerifyApprovalSignature(ctx, f.actorID, hs, canon, testRPID, testOriginOK, true); err == nil {
			t.Fatal("wrong rpIdHash must reject")
		}
	})
	t.Run("missing_up", func(t *testing.T) {
		f := newWebauthnFixture(t, 0)
		hs := f.validAssertion(t, flagUV, 1) // UV set, UP NOT set
		if err := f.reg.VerifyApprovalSignature(ctx, f.actorID, hs, f.canonical(), testRPID, testOriginOK, true); err == nil {
			t.Fatal("missing user-presence flag must reject")
		}
	})
}

// --- acceptance test 5: sign_count regression -> warning, decision proceeds

func TestAcceptance5_SignCountRegressionWarns(t *testing.T) {
	f := newWebauthnFixture(t, 5)               // registered with sign_count = 5
	hs := f.validAssertion(t, flagUP|flagUV, 3) // assertion regresses to 3
	if err := f.reg.VerifyApprovalSignature(ctx, f.actorID, hs, f.canonical(), testRPID, testOriginOK, true); err != nil {
		t.Fatalf("sign_count regression must NOT block the decision: %v", err)
	}
	events, err := f.reg.store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range events {
		if e.Kind == eventstore.ActorCredentialWarning {
			found = true
		}
	}
	if !found {
		t.Fatal("actor.credential_warning must be journaled on sign_count regression")
	}
}

// --- acceptance test 6: revoked credential -> reject -----------------------

func TestAcceptance6_RevokedCredentialRejected(t *testing.T) {
	f := newWebauthnFixture(t, 0)
	hs := f.validAssertion(t, flagUP|flagUV, 1)
	admin := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	if err := f.reg.RevokeCredential(ctx, admin, f.actorID, f.credentialID, "lost device", &eventstore.Basis{Type: "human", ID: "root"}); err != nil {
		t.Fatal(err)
	}
	err := f.reg.VerifyApprovalSignature(ctx, f.actorID, hs, f.canonical(), testRPID, testOriginOK, true)
	if !errors.Is(err, ErrCredentialRevoked) {
		t.Fatalf("revoked credential must reject with ErrCredentialRevoked, got %v", err)
	}
}

// --- acceptance test 9: RS256 passes; ES384/EdDSA rejected -----------------

func TestAcceptance9_AlgorithmAllowlist(t *testing.T) {
	t.Run("rs256_passes", func(t *testing.T) {
		reg := newRegistry()
		priv, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		cose := coseRSABytes(t, &priv.PublicKey, coseAlgRS256)
		credID := base64.RawURLEncoding.EncodeToString([]byte("cred-rs256"))
		actor := eventstore.Actor{Type: eventstore.ActorHuman, ID: "fin-dir", Role: "FinDirector"}
		if err := reg.RegisterCredential(ctx, actor, credID, cose, coseAlgRS256, nil, 0, nil, "rsa key", &eventstore.Basis{Type: "human", ID: "fin-dir"}); err != nil {
			t.Fatal(err)
		}
		f := &testFixture{reg: reg, actorID: actor.ID, credentialID: credID,
			taskID: "appr-2", entity: "Debtor", recordID: "d-2", transition: "escalate",
			decision: DecisionGrant, basisType: "approval", basisID: "appr-2", nodeID: "node-pub-abc", headSeq: 1}
		canon := f.canonical()
		challenge := ApprovalChallenge(canon)
		authData := buildAuthData(testRPID, flagUP|flagUV, 1)
		cdJSON := buildClientDataJSON("webauthn.get", challenge, testOrigin)
		sig := signRS256(t, priv, authData, cdJSON)
		hs := &HumanSignaturePayload{CredentialID: credID, AuthenticatorData: authData, ClientDataJSON: cdJSON, Signature: sig}
		if err := reg.VerifyApprovalSignature(ctx, actor.ID, hs, canon, testRPID, testOriginOK, true); err != nil {
			t.Fatalf("RS256 assertion must verify: %v", err)
		}
	})

	unsupported := []struct {
		name string
		alg  int
	}{
		{"es384", coseAlgES384},
		{"eddsa", coseAlgEdDSA},
	}
	for _, u := range unsupported {
		t.Run(u.name, func(t *testing.T) {
			reg := newRegistry()
			priv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			// EdDSA has no EC2 shape at all; reusing an EC2-shaped key with a
			// mismatched alg id is enough to prove the ALG ALLOWLIST (not key
			// parsing) is what rejects it — parseCOSEKey happily decodes the
			// bytes either way, verifyAssertionSignature must still refuse.
			cose := coseECBytes(t, &priv.PublicKey, 2, u.alg)
			credID := base64.RawURLEncoding.EncodeToString([]byte("cred-" + u.name))
			actor := eventstore.Actor{Type: eventstore.ActorHuman, ID: "fin-dir", Role: "FinDirector"}
			if err := reg.RegisterCredential(ctx, actor, credID, cose, int64(u.alg), nil, 0, nil, "unsupported alg", &eventstore.Basis{Type: "human", ID: "fin-dir"}); err != nil {
				t.Fatal(err)
			}
			f := &testFixture{reg: reg, actorID: actor.ID, credentialID: credID,
				taskID: "appr-3", entity: "Debtor", recordID: "d-3", transition: "escalate",
				decision: DecisionGrant, basisType: "approval", basisID: "appr-3", nodeID: "node-pub-abc", headSeq: 1}
			canon := f.canonical()
			challenge := ApprovalChallenge(canon)
			authData := buildAuthData(testRPID, flagUP|flagUV, 1)
			cdJSON := buildClientDataJSON("webauthn.get", challenge, testOrigin)
			sum := sha256.Sum256(append(append([]byte{}, authData...), func() []byte { h := sha256.Sum256(cdJSON); return h[:] }()...))
			sig, err := ecdsa.SignASN1(rand.Reader, priv, sum[:])
			if err != nil {
				t.Fatal(err)
			}
			hs := &HumanSignaturePayload{CredentialID: credID, AuthenticatorData: authData, ClientDataJSON: cdJSON, Signature: sig}
			err = reg.VerifyApprovalSignature(ctx, actor.ID, hs, canon, testRPID, testOriginOK, true)
			if err == nil || !strings.Contains(err.Error(), "unsupported alg") {
				t.Fatalf("%s must reject as unsupported alg, got: %v", u.name, err)
			}
		})
	}
}

// --- registration round-trip: attestation ("none") -> credential_registered

func TestWebauthnRegistrationRoundTrip(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cose := coseECBytes(t, &priv.PublicKey, 1, coseAlgES256)
	credID := []byte("registration-cred-id")
	aaguid := make([]byte, 16)
	var credIDLen [2]byte
	binary.BigEndian.PutUint16(credIDLen[:], uint16(len(credID)))
	authData := buildAuthData(testRPID, flagUP|flagAT, 0)
	authData = append(authData, aaguid...)
	authData = append(authData, credIDLen[:]...)
	authData = append(authData, credID...)
	authData = append(authData, cose...)

	attObj, err := cbor.Marshal(map[string]interface{}{
		"fmt": "none", "authData": authData, "attStmt": map[string]interface{}{},
	})
	if err != nil {
		t.Fatal(err)
	}

	var regChallenge [32]byte
	if _, err := rand.Read(regChallenge[:]); err != nil {
		t.Fatal(err)
	}
	cdJSON := buildClientDataJSON("webauthn.create", regChallenge, testOrigin)

	gotCredID, gotCOSE, gotAlg, gotAAGUID, gotSignCount, err := VerifyRegistration(cdJSON, attObj, regChallenge[:], testRPID, testOriginOK)
	if err != nil {
		t.Fatalf("registration must verify: %v", err)
	}
	if gotCredID != base64.RawURLEncoding.EncodeToString(credID) {
		t.Fatalf("credential id mismatch: %s", gotCredID)
	}
	if gotAlg != coseAlgES256 {
		t.Fatalf("alg mismatch: %d", gotAlg)
	}
	if len(gotAAGUID) != 16 {
		t.Fatalf("aaguid: %v", gotAAGUID)
	}
	if gotSignCount != 0 {
		t.Fatalf("sign count: %d", gotSignCount)
	}
	if len(gotCOSE) == 0 {
		t.Fatal("public key cose bytes empty")
	}

	reg := newRegistry()
	actor := eventstore.Actor{Type: eventstore.ActorHuman, ID: "fin-dir", Role: "FinDirector"}
	if err := reg.RegisterCredential(ctx, actor, gotCredID, gotCOSE, gotAlg, gotAAGUID, gotSignCount, []string{"internal"}, "laptop", &eventstore.Basis{Type: "human", ID: "fin-dir"}); err != nil {
		t.Fatal(err)
	}
	creds, err := reg.Credentials(ctx, actor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 || creds[0].CredentialID != gotCredID {
		t.Fatalf("registered credential not found in projection: %+v", creds)
	}
}

// --- acceptance test 8: `kalita verify` offline, on a plain event dump -----

// appendApprovalDecision journals a synthetic approval.requested +
// approval.granted pair the way internal/engine would (matching the JSON
// shapes VerifyApprovalSignaturesOffline reads back), optionally with a
// human_signature payload — this test does not go through the engine at all
// so it exercises the identity package's offline path in isolation.
func appendApprovalDecision(t *testing.T, store eventstore.Store, actor eventstore.Actor, approvalID, entity, recordID, action string, hs *HumanSignaturePayload, rpID string, headSeq uint64) {
	t.Helper()
	reqPayload, _ := json.Marshal(map[string]any{"entity": entity, "record_id": recordID, "action": action})
	if _, err := store.Append(ctx, eventstore.AppendInput{
		Actor: actor, Kind: eventstore.ApprovalRequested,
		Subject: eventstore.Subject{ApprovalID: approvalID}, Payload: reqPayload,
		Basis: &eventstore.Basis{Type: "human", ID: actor.ID},
	}); err != nil {
		t.Fatal(err)
	}
	payloadMap := map[string]any{"entity": entity, "record_id": recordID, "action": action}
	if hs != nil {
		payloadMap["human_signature"] = hs
		payloadMap["rp_id"] = rpID
		payloadMap["head_seq"] = headSeq
	}
	decPayload, _ := json.Marshal(payloadMap)
	if _, err := store.Append(ctx, eventstore.AppendInput{
		Actor: actor, Kind: eventstore.ApprovalGranted,
		Subject: eventstore.Subject{ApprovalID: approvalID}, Payload: decPayload,
		Basis: &eventstore.Basis{Type: "approval", ID: approvalID},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptance8_OfflineVerification(t *testing.T) {
	t.Run("signed_decision_verifies_offline", func(t *testing.T) {
		f := newWebauthnFixture(t, 0)
		hs := f.validAssertion(t, flagUP|flagUV, 1)
		actor := eventstore.Actor{Type: eventstore.ActorHuman, ID: f.actorID, Role: "FinDirector"}
		appendApprovalDecision(t, f.reg.store, actor, f.taskID, f.entity, f.recordID, f.transition, hs, testRPID, f.headSeq)

		events, err := f.reg.store.All(ctx)
		if err != nil {
			t.Fatal(err)
		}
		checked, problems := VerifyApprovalSignaturesOffline(events, f.nodeID, false)
		if checked != 1 {
			t.Fatalf("want 1 decision checked, got %d", checked)
		}
		if len(problems) != 0 {
			t.Fatalf("valid signed decision must verify clean offline, got: %v", problems)
		}
	})

	t.Run("require_signed_approvals_catches_token_decision", func(t *testing.T) {
		reg := newRegistry()
		actor := eventstore.Actor{Type: eventstore.ActorHuman, ID: "fin-dir", Role: "FinDirector"}
		// a token-only decision (no human_signature at all) slipped into the
		// journal — exactly what --require-signed-approvals is for.
		appendApprovalDecision(t, reg.store, actor, "appr-token", "Debtor", "d-9", "escalate", nil, "", 0)

		events, err := reg.store.All(ctx)
		if err != nil {
			t.Fatal(err)
		}
		checked, problems := VerifyApprovalSignaturesOffline(events, "node-pub-abc", false)
		if checked != 1 || len(problems) != 0 {
			t.Fatalf("without the flag a token decision is not itself a problem: checked=%d problems=%v", checked, problems)
		}
		_, problems = VerifyApprovalSignaturesOffline(events, "node-pub-abc", true)
		if len(problems) == 0 {
			t.Fatal("--require-signed-approvals must flag a token-only human decision")
		}
	})

	t.Run("later_revocation_does_not_invalidate_a_past_valid_signature", func(t *testing.T) {
		// Revocation is forward-looking (LLD-02 §3.5: it blocks FUTURE
		// decisions). A decision legitimately signed BEFORE the credential
		// was revoked (e.g. a lost laptop revoked afterwards) must still
		// verify clean on offline audit — ordinary operational history is
		// not tampering.
		f := newWebauthnFixture(t, 0)
		hs := f.validAssertion(t, flagUP|flagUV, 1)
		actor := eventstore.Actor{Type: eventstore.ActorHuman, ID: f.actorID, Role: "FinDirector"}
		appendApprovalDecision(t, f.reg.store, actor, f.taskID, f.entity, f.recordID, f.transition, hs, testRPID, f.headSeq)
		admin := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
		if err := f.reg.RevokeCredential(ctx, admin, f.actorID, f.credentialID, "lost laptop", &eventstore.Basis{Type: "human", ID: "root"}); err != nil {
			t.Fatal(err)
		}
		events, err := f.reg.store.All(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, problems := VerifyApprovalSignaturesOffline(events, f.nodeID, false)
		if len(problems) != 0 {
			t.Fatalf("a past valid signature must not be flagged just because the credential was later revoked: %v", problems)
		}
	})
}
