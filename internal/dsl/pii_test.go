package dsl

import "testing"

// LLD-16 §1 — the `pii subject=<ref>` field declaration. The grammar layer:
// personal-data fields are declared with their data subject, so the runtime can
// encrypt them per-subject and shred them per-subject later (the crypto half is
// a separate PR). These tests lock the grammar and its validation.

func TestPIICompiles(t *testing.T) {
	src := `entity Request:
    requester: ref[core.User] required
    notes: text pii subject=requester

roles:
    Manager

permissions:
    Manager:
        full [Request]
`
	if _, errs := Compile(map[string]string{"t.dsl": src}); len(errs) > 0 {
		t.Fatalf("valid pii declaration must compile: %v", errs[0])
	}
}

func TestPIIRequiresSubject(t *testing.T) {
	src := `entity Request:
    notes: text pii
`
	if _, errs := Compile(map[string]string{"t.dsl": src}); len(errs) == 0 {
		t.Fatal("pii without subject= must not compile")
	}
}

func TestPIISubjectMustBeAField(t *testing.T) {
	src := `entity Request:
    notes: text pii subject=missing
`
	_, errs := Compile(map[string]string{"t.dsl": src})
	if len(errs) == 0 {
		t.Fatal("pii subject naming an unknown field must not compile")
	}
}

func TestPIISubjectMustBeRef(t *testing.T) {
	src := `entity Request:
    title: string
    notes: text pii subject=title
`
	_, errs := Compile(map[string]string{"t.dsl": src})
	if len(errs) == 0 {
		t.Fatal("pii subject naming a non-ref field must not compile")
	}
}

func TestPIICannotBeComputed(t *testing.T) {
	src := `entity Request:
    requester: ref[core.User]
    upper: string computed = notes pii subject=requester
    notes: text
`
	_, errs := Compile(map[string]string{"t.dsl": src})
	if len(errs) == 0 {
		t.Fatal("pii on a computed field must not compile")
	}
}

func TestPIICannotBeUnique(t *testing.T) {
	src := `entity Request:
    requester: ref[core.User]
    notes: text pii subject=requester unique
`
	_, errs := Compile(map[string]string{"t.dsl": src})
	if len(errs) == 0 {
		t.Fatal("pii + unique must not compile (encryption randomizes ciphertext)")
	}
}
