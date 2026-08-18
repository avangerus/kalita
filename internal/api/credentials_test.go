package api

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fxamacker/cbor/v2"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// End-to-end WebAuthn flow over REST (LLD-02 §4, PR-3/PR-4): begin -> finish
// registration, then challenge -> decide with a human_signature, and the
// --require-human-signature enforcement gate. Uses a real synthetic ES256
// authenticator (no browser), exercising the exact wire shapes the API
// exposes — not the lower-level identity package directly (that is covered
// by internal/identity/webauthn_test.go).

const webauthnTestPack = `
entity Job:
    title: string required
    status: enum[New, Escalated] default=New

roles:
    Worker
    Director

permissions:
    Worker:
        full [Job]
        act  [escalate]
    Director:
        approve [escalate]
        read all

workflow Job on status:
    New -> Escalated: escalate requires approval(Director)
`

func newWebauthnEngine(t *testing.T, opts ...engine.Option) (*engine.Engine, *identity.Registry) {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"t.dsl": webauthnTestPack})
	if len(errs) > 0 {
		t.Fatalf("pack must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	allOpts := append([]engine.Option{engine.WithVerifier(reg.VerifySignature)}, opts...)
	eng, err := engine.New(context.Background(), model, store, allOpts...)
	if err != nil {
		t.Fatal(err)
	}
	return eng, reg
}

// coseECBytes mirrors internal/identity's test helper — kept local since it
// is unexported there.
func coseECBytes(t *testing.T, pub *ecdsa.PublicKey, alg int) []byte {
	t.Helper()
	x := pub.X.FillBytes(make([]byte, 32))
	y := pub.Y.FillBytes(make([]byte, 32))
	enc, err := cbor.Marshal(map[int]interface{}{1: 2, 3: alg, -1: 1, -2: x, -3: y})
	if err != nil {
		t.Fatal(err)
	}
	return enc
}

func buildAssertionAuthData(t *testing.T, rpID string, flags byte, signCount uint32) []byte {
	t.Helper()
	h := sha256.Sum256([]byte(rpID))
	var sc [4]byte
	binary.BigEndian.PutUint32(sc[:], signCount)
	out := append([]byte{}, h[:]...)
	out = append(out, flags)
	out = append(out, sc[:]...)
	return out
}

func signGet(t *testing.T, priv *ecdsa.PrivateKey, authData, clientDataJSON []byte) []byte {
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

type webauthnClient struct {
	t    *testing.T
	base string
	id   string
	role string
}

func (c *webauthnClient) request(method, path string, body any) (*http.Response, []byte) {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			c.t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, c.base+path, &buf)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("X-Actor-Id", c.id)
	req.Header.Set("X-Actor-Role", c.role)
	req.Header.Set("Origin", c.base)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp, out
}

// registerCredential drives POST /api/credentials/begin + finish end to end
// and returns the ES256 keypair + credential id for later signing.
func registerCredential(t *testing.T, srv *httptest.Server, actor *webauthnClient) (*ecdsa.PrivateKey, string) {
	t.Helper()
	resp, body := actor.request("POST", "/api/credentials/begin", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("begin: status %d body %s", resp.StatusCode, body)
	}
	var begin credentialsBeginResponse
	if err := json.Unmarshal(body, &begin); err != nil {
		t.Fatal(err)
	}
	challenge, err := base64.RawURLEncoding.DecodeString(begin.Challenge)
	if err != nil {
		t.Fatal(err)
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cose := coseECBytes(t, &priv.PublicKey, -7)
	credID := []byte("rest-cred-" + actor.id)
	aaguid := make([]byte, 16)
	var credIDLen [2]byte
	binary.BigEndian.PutUint16(credIDLen[:], uint16(len(credID)))
	authData := buildAssertionAuthData(t, begin.RPID, 0x41, 0) // UP|AT
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
	cdJSON, _ := json.Marshal(map[string]string{
		"type": "webauthn.create", "challenge": base64.RawURLEncoding.EncodeToString(challenge), "origin": srv.URL,
	})

	resp, body = actor.request("POST", "/api/credentials/finish", credentialsFinishRequest{
		ClientDataJSON: cdJSON, AttestationObject: attObj, Label: "test device",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("finish: status %d body %s", resp.StatusCode, body)
	}
	var out map[string]string
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return priv, out["credential_id"]
}

func TestWebauthnRESTRoundTrip(t *testing.T) {
	eng, reg := newWebauthnEngine(t)
	srv := httptest.NewServer(Secure(New(eng, reg, WithDevHeaders(), WithNodeID("node-test-pub"))))
	t.Cleanup(srv.Close)

	worker := &webauthnClient{t, srv.URL, "worker-1", "Worker"}
	director := &webauthnClient{t, srv.URL, "director-1", "Director"}

	priv, credID := registerCredential(t, srv, director)
	if credID == "" {
		t.Fatal("empty credential id")
	}

	// list credentials round-trips
	resp, body := director.request("GET", "/api/credentials", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list credentials: %d %s", resp.StatusCode, body)
	}

	// worker creates a Job and escalates it -> pending_approval
	resp, body = worker.request("POST", "/api/records/Job", mutate(map[string]any{"title": "t1"}))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.StatusCode, body)
	}
	var created map[string]any
	_ = json.Unmarshal(body, &created)
	jobID := created["id"].(string)

	resp, body = worker.request("POST", "/api/records/Job/"+jobID+"/act", map[string]any{
		"action": "escalate", "basis": map[string]any{"type": "human", "id": "worker-1"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("act: %d %s", resp.StatusCode, body)
	}
	var actRes map[string]any
	_ = json.Unmarshal(body, &actRes)
	approvalID := actRes["approval_id"].(string)
	if approvalID == "" {
		t.Fatal("expected pending approval")
	}

	// director fetches the challenge for granting this approval
	resp, body = director.request("GET", "/api/approvals/"+approvalID+"/challenge?decision=grant", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("challenge: %d %s", resp.StatusCode, body)
	}
	var chal approvalChallengeResponse
	if err := json.Unmarshal(body, &chal); err != nil {
		t.Fatal(err)
	}
	if chal.NodeID != "node-test-pub" {
		t.Fatalf("node_id not echoed from server config: %+v", chal)
	}
	challenge32, err := base64.RawURLEncoding.DecodeString(chal.Challenge)
	if err != nil {
		t.Fatal(err)
	}

	authData := buildAssertionAuthData(t, chal.RPID, 0x05, 1) // UP|UV
	cdJSON, _ := json.Marshal(map[string]string{
		"type": "webauthn.get", "challenge": base64.RawURLEncoding.EncodeToString(challenge32), "origin": srv.URL,
	})
	sig := signGet(t, priv, authData, cdJSON)

	resp, body = director.request("POST", "/api/approvals/"+approvalID+"/decide", map[string]any{
		"grant": true,
		"basis": map[string]any{"type": "human", "id": "director-1"},
		"human_signature": identity.HumanSignaturePayload{
			CredentialID: credID, AuthenticatorData: authData, ClientDataJSON: cdJSON, Signature: sig,
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("decide with human_signature: %d %s", resp.StatusCode, body)
	}
	var rec map[string]any
	_ = json.Unmarshal(body, &rec)
	if vals, _ := rec["values"].(map[string]any); vals["status"] != "Escalated" {
		t.Fatalf("job must be Escalated after grant: %+v", rec)
	}
}

func TestWebauthnRESTEnforcement(t *testing.T) {
	eng, reg := newWebauthnEngine(t, engine.WithRequireHumanSignature())
	srv := httptest.NewServer(Secure(New(eng, reg, WithDevHeaders(), WithNodeID("node-test-pub"), WithRequireHumanSignature())))
	t.Cleanup(srv.Close)

	worker := &webauthnClient{t, srv.URL, "worker-1", "Worker"}
	director := &webauthnClient{t, srv.URL, "director-1", "Director"}

	resp, body := worker.request("POST", "/api/records/Job", mutate(map[string]any{"title": "t1"}))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.StatusCode, body)
	}
	var created map[string]any
	_ = json.Unmarshal(body, &created)
	jobID := created["id"].(string)

	resp, body = worker.request("POST", "/api/records/Job/"+jobID+"/act", map[string]any{
		"action": "escalate", "basis": map[string]any{"type": "human", "id": "worker-1"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("act: %d %s", resp.StatusCode, body)
	}
	var actRes map[string]any
	_ = json.Unmarshal(body, &actRes)
	approvalID := actRes["approval_id"].(string)

	// token-only decide (no human_signature) must be rejected SIGNATURE_REQUIRED
	resp, body = director.request("POST", "/api/approvals/"+approvalID+"/decide", map[string]any{
		"grant": true, "basis": map[string]any{"type": "human", "id": "director-1"},
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("enforcement must reject token-only decide, got %d %s", resp.StatusCode, body)
	}
	var errBody map[string]any
	_ = json.Unmarshal(body, &errBody)
	if errBody["code"] != "SIGNATURE_REQUIRED" {
		t.Fatalf("want SIGNATURE_REQUIRED, got %+v", errBody)
	}
}

// TestWebauthnProposalSignatureRoundTrip proves the DEFINITION-decision
// ceremony (product mission: signed model changes): challenge issuance is
// gated to the defApprover role, a token-only decide is rejected under
// --require-human-signature, and a WebAuthn assertion signed over the
// proposal canonical applies the change.
func TestWebauthnProposalSignatureRoundTrip(t *testing.T) {
	eng, reg := newWebauthnEngine(t, engine.WithRequireHumanSignature())
	srv := httptest.NewServer(Secure(New(eng, reg, WithDevHeaders(), WithNodeID("node-test-pub"), WithRequireHumanSignature())))
	t.Cleanup(srv.Close)

	owner := &webauthnClient{t, srv.URL, "owner-1", "Owner"}
	worker := &webauthnClient{t, srv.URL, "worker-1", "Worker"}

	priv, credID := registerCredential(t, srv, owner)
	if credID == "" {
		t.Fatal("empty credential id")
	}

	// a proposal is parked through the engine (propose_change is MCP-only;
	// the REST surface is challenge + decide)
	p, dslErrs, err := eng.ProposeChange(context.Background(),
		eventstore.Actor{Type: eventstore.ActorHuman, ID: "admin-1", Role: "Admin"},
		map[string]string{"t.dsl": webauthnTestPack + "\nentity Boom:\n    x: int\n"},
		eng.DefVersion(), "add Boom",
		&eventstore.Basis{Type: "human", ID: "admin-1"}, "idem-prop-wa-0001")
	if err != nil || len(dslErrs) > 0 {
		t.Fatalf("propose: err=%v dslErrs=%v", err, dslErrs)
	}

	// challenge is gated: a non-approver role cannot even fetch it
	resp, body := worker.request("GET", "/api/proposals/"+p.ID+"/challenge?decision=grant", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-approver challenge must be 403, got %d %s", resp.StatusCode, body)
	}

	// token-only decide is rejected under --require-human-signature
	resp, body = owner.request("POST", "/api/proposals/"+p.ID+"/decide", map[string]any{
		"grant": true, "basis": map[string]any{"type": "human", "id": "owner-1"},
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("token-only proposal decide must be rejected, got %d %s", resp.StatusCode, body)
	}
	var errBody map[string]any
	_ = json.Unmarshal(body, &errBody)
	if errBody["code"] != "SIGNATURE_REQUIRED" {
		t.Fatalf("want SIGNATURE_REQUIRED, got %+v", errBody)
	}

	// the real ceremony: challenge -> assert -> signed decide
	resp, body = owner.request("GET", "/api/proposals/"+p.ID+"/challenge?decision=grant", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("challenge: %d %s", resp.StatusCode, body)
	}
	var chal proposalChallengeResponse
	if err := json.Unmarshal(body, &chal); err != nil {
		t.Fatal(err)
	}
	if chal.NodeID != "node-test-pub" {
		t.Fatalf("node_id not echoed from server config: %+v", chal)
	}
	challenge32, err := base64.RawURLEncoding.DecodeString(chal.Challenge)
	if err != nil {
		t.Fatal(err)
	}
	authData := buildAssertionAuthData(t, chal.RPID, 0x05, 1) // UP|UV
	cdJSON, _ := json.Marshal(map[string]string{
		"type": "webauthn.get", "challenge": base64.RawURLEncoding.EncodeToString(challenge32), "origin": srv.URL,
	})
	sig := signGet(t, priv, authData, cdJSON)

	resp, body = owner.request("POST", "/api/proposals/"+p.ID+"/decide", map[string]any{
		"grant": true,
		"basis": map[string]any{"type": "human", "id": "owner-1"},
		"human_signature": identity.HumanSignaturePayload{
			CredentialID: credID, AuthenticatorData: authData, ClientDataJSON: cdJSON, Signature: sig,
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("signed proposal decide: %d %s", resp.StatusCode, body)
	}
	if eng.DefVersion() != p.BaseDefVersion+1 {
		t.Fatalf("def_version must advance after the signed apply: got %d", eng.DefVersion())
	}
}

