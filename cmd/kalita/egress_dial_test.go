package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

// LIM-9: host canonicalization, IDN refusal and resolve-and-pin dialing.

func TestNormalizeHostIPv6(t *testing.T) {
	cases := map[string]string{
		"::1":              "::1",
		"[::1]":            "::1",
		"[::1]:8080":       "::1",
		"fe80::1":          "fe80::1",
		"2001:DB8::1":      "2001:db8::1",
		"[2001:db8:0::1]":  "2001:db8::1",
		"::ffff:127.0.0.1": "127.0.0.1",
		"fe80::1%eth0":     "",
		"[fe80::1%25eth0]": "",
		"a:b:c":            "",
	}
	for in, want := range cases {
		if got := normalizeHost(in); got != want {
			t.Errorf("normalizeHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAllowedIPv6IsExact(t *testing.T) {
	domains := parseEgressDomains("fe80::1, [2001:db8::1]")
	if allowed(domains, "http://[fe80::2]/hook") {
		t.Fatal("allowlisting fe80::1 must not allow fe80::2")
	}
	if !allowed(domains, "http://[fe80::1]:9000/hook") {
		t.Fatal("the allowlisted IPv6 literal must match")
	}
	if !allowed(domains, "https://[2001:db8:0:0::1]/hook") {
		t.Fatal("a non-canonical spelling of an allowlisted address must match")
	}
}

func TestIDNHostsAreRefused(t *testing.T) {
	if got := parseEgressDomains("пример.рф,example.com"); len(got) != 1 || got[0] != "example.com" {
		t.Fatalf("a non-ASCII allowlist entry must be dropped, got %v", got)
	}
	puny := []string{"xn--e1afmkfd.xn--p1ai"}
	if allowed(puny, "https://пример.рф/hook") {
		t.Fatal("a Unicode host must not match, even against its own punycode")
	}
	if !allowed(puny, "https://xn--e1afmkfd.xn--p1ai/hook") {
		t.Fatal("the punycode form must match exactly")
	}
	// a Cyrillic 'а' homograph of an ASCII allowlisted domain
	if allowed([]string{"payments.example.com"}, "https://pаyments.example.com/hook") {
		t.Fatal("a homograph must not match")
	}
}

func TestEgressIPPolicy(t *testing.T) {
	strict, open := egressPolicy{}, egressPolicy{allowPrivate: true}
	cases := []struct {
		ip           string
		strict, open bool
	}{
		{"93.184.216.34", true, true},
		{"2606:2800:220:1::1", true, true},
		{"127.0.0.1", true, true},
		{"::1", true, true},
		{"10.1.2.3", false, true},
		{"172.16.0.1", false, true},
		{"192.168.88.8", false, true},
		{"169.254.169.254", false, true},
		{"100.64.0.1", false, true},
		{"fd00::1", false, true},
		{"fe80::1", false, true},
		{"0.0.0.0", false, false},
		{"::", false, false},
		{"224.0.0.1", false, false},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if got := strict.ipAllowed(ip); got != c.strict {
			t.Errorf("default policy, %s: got %v want %v", c.ip, got, c.strict)
		}
		if got := open.ipAllowed(ip); got != c.open {
			t.Errorf("allow-private policy, %s: got %v want %v", c.ip, got, c.open)
		}
	}
}

func stubLookup(ips ...string) func(context.Context, string) ([]net.IPAddr, error) {
	return func(context.Context, string) ([]net.IPAddr, error) {
		var out []net.IPAddr
		for _, s := range ips {
			out = append(out, net.IPAddr{IP: net.ParseIP(s)})
		}
		return out, nil
	}
}

// The name is resolved once and the connection goes to the vetted address: a
// name that resolves to loopback reaches the local server without real DNS.
func TestDelivererPinsResolvedAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "" {
			t.Error("the request must keep its host name")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)

	deliver := newEgressDeliverer(egressPolicy{lookup: stubLookup("10.9.9.9", "127.0.0.1")})
	if derr := deliver(context.Background(), "http://hooks.test:"+u.Port()+"/x", []byte(`{}`), ""); derr != nil {
		t.Fatalf("a name with a loopback answer must be delivered (private answer skipped): %v", derr)
	}
}

func TestDelivererRefusesPrivateResolution(t *testing.T) {
	deliver := newEgressDeliverer(egressPolicy{lookup: stubLookup("10.0.0.5", "169.254.169.254")})
	derr := deliver(context.Background(), "https://rebind.test/x", []byte(`{}`), "")
	if derr == nil || derr.category != "private_target" {
		t.Fatalf("want private_target refusal, got %+v", derr)
	}
	if derr.retryable() {
		t.Fatal("a policy refusal must not be retried")
	}
}

func TestDelivererAllowPrivateDialsPrivate(t *testing.T) {
	// with the opt-in the private address is dialed; the closed port is a
	// plain network error, not a policy refusal
	deliver := newEgressDeliverer(egressPolicy{allowPrivate: true, lookup: stubLookup("127.0.0.1")})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	derr := deliver(context.Background(), "http://lan.test:"+strconv.Itoa(port)+"/x", []byte(`{}`), "")
	if derr == nil || derr.category == "private_target" {
		t.Fatalf("want a network error, got %+v", derr)
	}
}
