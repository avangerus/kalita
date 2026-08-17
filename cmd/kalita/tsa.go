package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"io"
	"math/big"
	"net/http"
)

// RFC-3161 TSA client for the checkpoint anchor (PROD-READINESS O3 / LLD-17):
// `kalita anchor --tsa <url>` timestamps the anchor with a trusted third party,
// so journal integrity is provable WITHOUT trusting our node. The full
// TimeStampResp DER is saved next to the anchor and verifiable later with any
// RFC-3161 verifier (e.g. openssl ts -verify).

// sha256OID is the hash algorithm identifier (RFC 5754).
var sha256OID = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}

type messageImprint struct {
	HashAlgorithm pkix.AlgorithmIdentifier
	HashedMessage []byte
}

// timeStampReq ::= SEQUENCE { version INTEGER, messageImprint MessageImprint,
// nonce INTEGER OPTIONAL, certReq BOOLEAN DEFAULT FALSE }
type timeStampReq struct {
	Version        int
	MessageImprint messageImprint
	Nonce          *big.Int `asn1:"optional"`
	CertReq        bool     `asn1:"optional"`
}

type pkiStatusInfo struct {
	Status int
}

// timeStampResp ::= SEQUENCE { status PKIStatusInfo, timeStampToken OPTIONAL }
type timeStampResp struct {
	Status         pkiStatusInfo
	TimeStampToken asn1.RawValue `asn1:"optional"`
}

// buildTimeStampReq encodes an RFC-3161 TimeStampReq over content (SHA-256
// MessageImprint + random nonce). Returns the DER and the nonce (for tests).
func buildTimeStampReq(content []byte) ([]byte, *big.Int, error) {
	nonce, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		return nil, nil, err
	}
	digest := sha256.Sum256(content)
	req := timeStampReq{
		Version: 1,
		MessageImprint: messageImprint{
			HashAlgorithm: pkix.AlgorithmIdentifier{Algorithm: sha256OID, Parameters: asn1.NullRawValue},
			HashedMessage: digest[:],
		},
		Nonce:   nonce,
		CertReq: false,
	}
	der, err := asn1.Marshal(req)
	if err != nil {
		return nil, nil, err
	}
	return der, nonce, nil
}

// tsaTimestamp sends a timestamp request to an RFC-3161 TSA and returns the
// full TimeStampResp DER. The status must be granted (0).
func tsaTimestamp(content []byte, tsaURL string) ([]byte, error) {
	reqDER, _, err := buildTimeStampReq(content)
	if err != nil {
		return nil, err
	}
	resp, err := http.Post(tsaURL, "application/timestamp-query", bytes.NewReader(reqDER))
	if err != nil {
		return nil, fmt.Errorf("tsa request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tsa http %d: %.200s", resp.StatusCode, body)
	}
	var tsr timeStampResp
	rest, err := asn1.Unmarshal(body, &tsr)
	if err != nil {
		return nil, fmt.Errorf("tsa response parse: %w", err)
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("tsa response has trailing bytes")
	}
	if tsr.Status.Status != 0 {
		return nil, fmt.Errorf("tsa rejected the request: PKIStatus %d", tsr.Status.Status)
	}
	return body, nil
}
