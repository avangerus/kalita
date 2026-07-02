package dsl

import "testing"

// LLD-06 §1 (PK-2): the product.mcp{} tool registry. The façade worker that
// reads this section is not built yet (Wave B, LLD-06 PR-2) — these tests
// cover only the compiler's contract: the section parses into typed manifest
// data, coexists with the unrelated product{} white-label block (same leading
// token, disambiguated by the dispatcher), and bad input is a structured
// error, not a silent no-op.

func TestProductMCPManifestParses(t *testing.T) {
	src := `pack contracts
version 1

entity Counterparty:
    name: string required

entity Contract:
    title:        string required
    counterparty: ref[Counterparty]
    terms:        text

entity Finding:
    contract: ref[Contract]

product.mcp:
    tool legal.submit_contract "Create a contract and send it to legal review":
        in:
            title:        string required
            counterparty: ref[Counterparty] required
            terms:        text required
        compose:
            create Contract(title: $title, counterparty: $counterparty, terms: $terms) -> $contract
            act send_to_legal on $contract
        out: contract

    tool legal.findings "Findings raised on a contract":
        in:
            contract: ref[Contract] required
        compose:
            query Finding where contract = $contract -> $findings
        out: findings

roles:
    Owner

permissions:
    Owner:
        full [Counterparty, Contract, Finding]
`
	m, errs := Compile(map[string]string{"p.dsl": src})
	for _, e := range errs {
		t.Errorf("unexpected: %v", e)
	}
	if t.Failed() {
		t.FailNow()
	}
	if m.ProductMCP == nil {
		t.Fatal("product.mcp block must parse")
	}
	if len(m.ProductMCP.Tools) != 2 {
		t.Fatalf("want 2 tools, got %d: %+v", len(m.ProductMCP.Tools), m.ProductMCP.Tools)
	}
	submit := m.ProductMCP.Tools[0]
	if submit.Name != "legal.submit_contract" {
		t.Errorf("name = %q, want legal.submit_contract", submit.Name)
	}
	if submit.Desc != "Create a contract and send it to legal review" {
		t.Errorf("desc = %q", submit.Desc)
	}
	if len(submit.In) != 3 {
		t.Fatalf("want 3 typed inputs, got %+v", submit.In)
	}
	if submit.In[0].Name != "title" || submit.In[0].Type.Scalar != "string" || !submit.In[0].Required {
		t.Errorf("in[0] = %+v", submit.In[0])
	}
	if submit.In[1].Type.Kind != TyRef || submit.In[1].Type.RefTarget != "Counterparty" {
		t.Errorf("in[1] type = %+v, want ref[Counterparty]", submit.In[1].Type)
	}
	if len(submit.Compose) != 2 {
		t.Fatalf("want 2 compose steps, got %+v", submit.Compose)
	}
	if submit.Out != "contract" {
		t.Errorf("out = %q, want contract", submit.Out)
	}

	findings := m.ProductMCP.Tools[1]
	if findings.Name != "legal.findings" {
		t.Errorf("name = %q, want legal.findings", findings.Name)
	}
	if len(findings.In) != 1 || findings.In[0].Name != "contract" {
		t.Errorf("in = %+v", findings.In)
	}
}

// TestProductMCPBlockOptional: a pack without product.mcp{} compiles exactly
// as before (I-1 — additive grammar, zero impact on packs that don't use it).
func TestProductMCPBlockOptional(t *testing.T) {
	src := `pack plain
version 1

entity Doc:
    title: string required

roles:
    Owner

permissions:
    Owner:
        full [Doc]
`
	m, errs := Compile(map[string]string{"p.dsl": src})
	for _, e := range errs {
		t.Errorf("unexpected: %v", e)
	}
	if m.ProductMCP != nil {
		t.Fatal("product.mcp must be nil when the pack declares no product.mcp block")
	}
}

// TestProductAndProductMCPCoexist: `product:` (LLD-05 §3, white-label) and
// `product.mcp:` (LLD-06 §1, tool registry) share the same leading token —
// the dispatcher must tell them apart and populate both independently.
func TestProductAndProductMCPCoexist(t *testing.T) {
	src := `pack p
version 1

product:
    name "P"

entity Doc:
    title: string required

product.mcp:
    tool p.echo "Echo a doc":
        in:
            title: string required
        compose:
            query Doc where title = $title -> $found
        out: found

roles:
    Owner

permissions:
    Owner:
        full [Doc]
`
	m, errs := Compile(map[string]string{"p.dsl": src})
	for _, e := range errs {
		t.Errorf("unexpected: %v", e)
	}
	if t.Failed() {
		t.FailNow()
	}
	if m.Product == nil || m.Product.Name != "P" {
		t.Fatalf("product block must still parse independently, got %+v", m.Product)
	}
	if m.ProductMCP == nil || len(m.ProductMCP.Tools) != 1 {
		t.Fatalf("product.mcp block must parse independently, got %+v", m.ProductMCP)
	}
}

// TestProductMCPDeclaredTwice: a second product.mcp{} block is rejected, not
// merged silently.
func TestProductMCPDeclaredTwice(t *testing.T) {
	src := `pack p
version 1

entity Doc:
    title: string required

product.mcp:
    tool p.a "A":
        compose:
            query Doc where title = $x -> $r
        out: r

product.mcp:
    tool p.b "B":
        compose:
            query Doc where title = $x -> $r
        out: r

roles:
    Owner

permissions:
    Owner:
        full [Doc]
`
	_, errs := Compile(map[string]string{"p.dsl": src})
	found := false
	for _, e := range errs {
		if e.Code == EBadProductMCP {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected E051_BAD_PRODUCT_MCP for a duplicate product.mcp block, got %v", errs)
	}
}

// TestProductMCPToolWithoutCompose: a tool with no compose steps is a
// structured error, not a silent empty tool (I-3 — errors are events).
func TestProductMCPToolWithoutCompose(t *testing.T) {
	src := `pack p
version 1

entity Doc:
    title: string required

product.mcp:
    tool p.a "A":
        out: r

roles:
    Owner

permissions:
    Owner:
        full [Doc]
`
	_, errs := Compile(map[string]string{"p.dsl": src})
	found := false
	for _, e := range errs {
		if e.Code == EBadProductMCP {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected E051_BAD_PRODUCT_MCP for a tool without compose steps, got %v", errs)
	}
}
