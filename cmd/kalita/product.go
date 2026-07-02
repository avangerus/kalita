package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// splitFlagsAndPositional separates known --flag [value] / --flag=value pairs
// (order-independent, may be interleaved with positional args) from bare
// positional tokens, so `product scaffold knowvault --out x` and
// `product scaffold --out x knowvault` both work — unlike raw flag.Parse,
// which stops at the first non-flag token.
func splitFlagsAndPositional(args []string, known map[string]bool) (flagArgs, positional []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if body, ok := strings.CutPrefix(a, "--"); ok {
			if fname, fval, hasEq := strings.Cut(body, "="); hasEq {
				if known[fname] {
					flagArgs = append(flagArgs, "--"+fname+"="+fval)
					continue
				}
			} else if known[body] {
				flagArgs = append(flagArgs, a)
				if i+1 < len(args) {
					i++
					flagArgs = append(flagArgs, args[i])
				}
				continue
			}
		}
		positional = append(positional, a)
	}
	return flagArgs, positional
}

// sanitizeEnvName turns a pack name into a valid shell/compose env-var
// fragment: non-alnum runs collapse to underscores.
func sanitizeEnvName(pack string) string {
	var b strings.Builder
	for _, r := range pack {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

// kalita product scaffold (LLD-05 §3, PK-4): the deploy obvious-path for a
// pack shipped as a standalone product. Not magic — it copies templates with
// the pack name substituted, mirroring the hand-built deploy/knowvault
// layout. It does not know what workers a product needs (that is
// product-specific, e.g. KnowVault's indexer/search) — those are added by
// hand afterward; the scaffold gives the operator node+postgres+status+a
// brand-manifest skeleton to start from.

func productCmd(args []string) {
	if len(args) < 1 || args[0] != "scaffold" {
		fmt.Println("usage: kalita product scaffold <pack> [--pack-dir <dir>] [--out <dir>]")
		os.Exit(1)
	}
	// Flags and the positional <pack> may appear in any order (operators
	// naturally type `product scaffold knowvault --out ...`); the stdlib
	// flag package stops parsing at the first non-flag token, which would
	// silently drop flags typed after the pack name. Split them ourselves.
	fs := flag.NewFlagSet("product scaffold", flag.ExitOnError)
	packDir := fs.String("pack-dir", "", "pack source directory (default packs/<pack>)")
	out := fs.String("out", "", "output directory (default deploy/<pack>)")
	flagArgs, positional := splitFlagsAndPositional(args[1:], map[string]bool{"pack-dir": true, "out": true})
	_ = fs.Parse(flagArgs)
	if len(positional) < 1 {
		log.Fatal("usage: kalita product scaffold <pack> [--pack-dir <dir>] [--out <dir>]")
	}
	pack := positional[0]
	if *packDir == "" {
		*packDir = filepath.Join("packs", pack)
	}
	if *out == "" {
		*out = filepath.Join("deploy", pack)
	}
	if err := scaffoldProduct(pack, *packDir, *out); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("scaffolded %s: docker-compose.yml, .env.example, status.html, product-manifest.dsl.skeleton, RUNBOOK.md\n", *out)
	fmt.Println("next: fill in .env, add any product-specific workers to docker-compose.yml, merge the skeleton's product{} block into your pack")
}

// scaffoldTemplates is the closed set of files a scaffold produces, each a
// Go text/template rendered with scaffoldVars. Kept as named consts (not a
// dir walk) so the set is reviewable in one place and testable file-by-file.
var scaffoldTemplates = map[string]string{
	"docker-compose.yml":            composeTemplate,
	".env.example":                  envExampleTemplate,
	"status.html":                   statusHTMLTemplate,
	"product-manifest.dsl.skeleton": manifestSkeletonTemplate,
	"RUNBOOK.md":                    runbookTemplate,
}

type scaffoldVars struct {
	Pack     string // technical pack name, e.g. "knowvault"
	PackDir  string // relative path to the pack source, for the compose comment
	BrandEnv string // KALITA_BRAND default = the pack name, Title-cased is left to the operator
	// NodePortVar is precomputed (not built inside the template) to avoid
	// docker-compose's own ${VAR:-default} syntax colliding with Go
	// text/template's {{ }} delimiters.
	NodePortVar string
}

// scaffoldProduct renders every template in scaffoldTemplates into outDir,
// substituting the pack name. It does not read packDir's contents (no need
// to — the compose command line references it by path, and the DSL parses
// its own `product:` block independently); packDir is only checked to exist,
// as a sanity guard against scaffolding a product with no pack behind it.
func scaffoldProduct(pack, packDir, outDir string) error {
	if pack == "" {
		return fmt.Errorf("product scaffold: pack name is required")
	}
	if st, err := os.Stat(packDir); err != nil || !st.IsDir() {
		return fmt.Errorf("product scaffold: pack directory %s not found (pass --pack-dir if it lives elsewhere)", packDir)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("product scaffold: %w", err)
	}
	vars := scaffoldVars{
		Pack: pack, PackDir: packDir, BrandEnv: pack,
		NodePortVar: fmt.Sprintf("${%s_NODE_PORT:-8080}", strings.ToUpper(sanitizeEnvName(pack))),
	}
	for name, tmplSrc := range scaffoldTemplates {
		t, err := template.New(name).Parse(tmplSrc)
		if err != nil {
			return fmt.Errorf("product scaffold: template %s: %w", name, err)
		}
		f, err := os.Create(filepath.Join(outDir, name))
		if err != nil {
			return fmt.Errorf("product scaffold: %w", err)
		}
		err = t.Execute(f, vars)
		cerr := f.Close()
		if err != nil {
			return fmt.Errorf("product scaffold: render %s: %w", name, err)
		}
		if cerr != nil {
			return fmt.Errorf("product scaffold: write %s: %w", name, cerr)
		}
	}
	return nil
}

// --- templates --------------------------------------------------------------
//
// Modeled on deploy/knowvault/docker-compose.yml, stripped of anything
// KnowVault-specific (indexer/search/qdrant workers) — those are product
// capabilities, not something a generic scaffold can know (LLD-05 §3: "не
// магия — копирование шаблонов с подстановкой имени пака").

const composeTemplate = `# {{.Pack}} — generated by "kalita product scaffold {{.Pack}}".
# Fill in .env (copy from .env.example), then: docker compose up --build
#
# This is the generic skeleton: node + postgres. Add any product-specific
# workers (indexers, search backends, connectors — see deploy/knowvault for
# an example with an indexer + qdrant + search worker) as extra services.

name: {{.Pack}}

services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-{{.Pack}}}
      POSTGRES_DB: {{.Pack}}
    volumes: [journal:/var/lib/postgresql/data]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 5s
      retries: 10

  node:
    build: { context: ../.., dockerfile: Dockerfile }
    depends_on:
      postgres: { condition: service_healthy }
    environment:
      KALITA_PG_DSN: postgres://postgres:${POSTGRES_PASSWORD:-{{.Pack}}}@postgres:5432/{{.Pack}}
      KALITA_BRAND: ${BRAND_NAME:-{{.BrandEnv}}}
    command:
      - serve
      - --pack=/opt/kalita/{{.PackDir}}
      - --listen=:8080
      - --insecure-http            # TLS terminates at your reverse proxy
      - --ui-dir=/opt/kalita/web
      - --files-dir=/data/files
      - --data-dir=/data/keys
    volumes: [files:/data/files, keys:/data/keys]
    ports: ["{{.NodePortVar}}:8080"]

volumes:
  journal:
  files:
  keys:
`

const envExampleTemplate = `# Copy to .env and fill in. Then: docker compose up --build
POSTGRES_PASSWORD=change-me-strong
BRAND_NAME={{.Pack}}
`

const statusHTMLTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>{{.Pack}} — status</title>
<style>
  body{font:14px/1.5 system-ui,sans-serif;background:#0f1419;color:#dbe2ea;padding:32px;max-width:640px;margin:0 auto}
  h1{font-size:18px} .row{display:flex;justify-content:space-between;padding:8px 0;border-bottom:1px solid #2a323c}
  .ok{color:#3fb96f} .bad{color:#e0604f} .pending{color:#e0a93f}
</style>
</head>
<body>
<h1>{{.Pack}} — node health</h1>
<div class="row"><span>liveness (/healthz)</span><span id="healthz" class="pending">checking…</span></div>
<div class="row"><span>readiness (/readyz)</span><span id="readyz" class="pending">checking…</span></div>
<p class="pending" style="font-size:12px">Organism-level status page (LLD-05 §3): reads the node's own /healthz and
/readyz. Point it at your node origin if this file is served separately.</p>
<script type="module">
async function check(id, path) {
  const el = document.getElementById(id);
  try {
    const r = await fetch(path);
    el.textContent = r.ok ? 'ok' : 'error ' + r.status;
    el.className = r.ok ? 'ok' : 'bad';
  } catch (e) {
    el.textContent = 'unreachable';
    el.className = 'bad';
  }
}
check('healthz', '/healthz');
check('readyz', '/readyz');
</script>
</body>
</html>
`

const manifestSkeletonTemplate = `# product-manifest.dsl.skeleton — merge this block into your pack's .dsl
# (LLD-05 §3, PK-4). It is NOT auto-applied: review it, then paste it into
# {{.PackDir}}/<something>.dsl next to your "pack {{.Pack}}" line.

product:
    name "{{.Pack}}"
    logo "https://cdn.example.com/{{.Pack}}-logo.svg"
    color accent "#4da3ff"
    home SearchFace
    auth password
`

const runbookTemplate = `# {{.Pack}} — runbook

Generated by ` + "`kalita product scaffold {{.Pack}}`" + `. Fill in the sections below as you
learn the product's operational quirks.

## Start

1. cd deploy/{{.Pack}}
2. cp .env.example .env, fill in POSTGRES_PASSWORD and any worker secrets
3. docker compose up --build
4. Register the first Owner: docker compose exec node kalita user add --id you@example.com --role Owner --password '...'

## Health

- GET /healthz — liveness (always 200 once the process is up)
- GET /readyz — readiness (200 once a pack is loaded and the engine is serving)
- status.html — the human-readable version of the two above

## Backup

- kalita export --pack /opt/kalita/{{.PackDir}} --out backup.json
- kalita verify --pub node.pub (offline chain-integrity proof, I-5)

## Incidents

(fill in as they happen — this is a living document, not a template to leave blank)
`
