package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509/pkix"
	"encoding/asn1"
	"testing"
)

// RFC-3161 request encoding: the TimeStampReq DER must round-trip with the
// right version, the SHA-256 MessageImprint over the exact content, and the
// nonce — so `kalita anchor --tsa` sends a spec-valid query a TSA will accept.
func TestBuildTimeStampReqRoundTrip(t *testing.T) {
	content := []byte("checkpoint-anchor-1d47ae62")
	der, nonce, err := buildTimeStampReq(content)
	if err != nil {
		t.Fatal(err)
	}

	var req timeStampReq
	if _, err := asn1.Unmarshal(der, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.Version != 1 {
		t.Fatalf("version = %d, want 1", req.Version)
	}
	if !req.MessageImprint.HashAlgorithm.Algorithm.Equal(sha256OID) {
		t.Fatalf("hash algorithm = %v, want sha256", req.MessageImprint.HashAlgorithm.Algorithm)
	}
	wantDigest := sha256.Sum256(content)
	if !bytes.Equal(req.MessageImprint.HashedMessage, wantDigest[:]) {
		t.Fatalf("hashedMessage mismatch")
	}
	if req.Nonce == nil || req.Nonce.Cmp(nonce) != 0 {
		t.Fatalf("nonce mismatch")
	}
}

// The DER must contain the sha256 OID bytes (60 86 48 01 65 03 04 02 01 =
// 2.16.840.1.101.3.4.2.1) — a spec-level sanity check independent of our own
// decoder.
func TestBuildTimeStampReqDERContainsOID(t *testing.T) {
	der, _, err := buildTimeStampReq([]byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(der, []byte{0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x01}) {
		t.Fatal("DER does not contain the sha256 OID")
	}
	// and the NULL parameters of the AlgorithmIdentifier
	if !bytes.Contains(der, []byte{0x05, 0x00}) {
		t.Fatal("DER does not contain the NULL parameters")
	}
}

// Two requests over the same content must differ (fresh nonce) — replay
// protection per RFC-3161 §2.4.1.
func TestBuildTimeStampReqNonceIsFresh(t *testing.T) {
	a, na, err := buildTimeStampReq([]byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	b, nb, err := buildTimeStampReq([]byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) || na.Cmp(nb) == 0 {
		t.Fatal("two requests must carry different nonces")
	}
}

var _ = pkix.AlgorithmIdentifier{} // keep the import honest if fields move
