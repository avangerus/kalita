// Command kalita is the single-binary entry point for the Kalita node.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/avangerus/kalita/internal/api"
	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
	"github.com/avangerus/kalita/internal/mcp"
	"github.com/avangerus/kalita/internal/secrets"
	"github.com/avangerus/kalita/internal/webui"
)

var version = "0.1.0-dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	switch os.Args[1] {
	case "version":
		fmt.Printf("kalita %s\n", version)
	case "serve":
		serve(os.Args[2:])
	case "check":
		check(os.Args[2:])
	case "agent":
		agentCmd(os.Args[2:])
	case "user":
		userCmd(os.Args[2:])
	case "verify":
		verifyCmd(os.Args[2:])
	case "lockdown":
		lockdownCmd(os.Args[2:])
	case "export":
		exportCmd(os.Args[2:])
	case "backup":
		backupCmd(os.Args[2:])
	case "trace":
		traceCmd(os.Args[2:])
	case "anchor":
		anchorCmd(os.Args[2:])
	case "import":
		importCmd(os.Args[2:])
	case "era":
		eraCmd(os.Args[2:])
	case "upgrade":
		upgradeCmd(os.Args[2:])
	case "product":
		productCmd(os.Args[2:])
	case "report":
		reportCmd(os.Args[2:])
	case "egress-report":
		egressReportCmd(os.Args[2:])
	case "egress":
		egressCmd(os.Args[2:])
	case "shadow":
		shadowCmd(os.Args[2:])
	case "mcp":
		mcpCmd(os.Args[2:])
	default:
		usage()
	}
}

// verifyCmd proves journal integrity offline: hash chain + (with node.pub)
// checkpoint signatures. This is the client's notary procedure and the heart
// of the backup drill in OPERATIONS.md.
func verifyCmd(args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	pub := fs.String("pub", "", "node.pub file for checkpoint AND human_signature verification (LLD-02 §5)")
	file := fs.String("file", "", "backup directory (with MANIFEST.json + journal.jsonl) to verify instead of the Postgres journal (OPS-2, Cycle-A3)")
	requireSignedApprovals := fs.Bool("require-signed-approvals", false, "fail if any human approve/reject decision has no WebAuthn human_signature (LLD-02 §5 — audits a prod journal that is supposed to be passkey-only)")
	_ = fs.Parse(args)

	events, err := loadVerifyEvents(*file)
	if err != nil {
		log.Fatal(err)
	}
	if err := eventstore.VerifyChain(events); err != nil {
		fmt.Printf("CHAIN BROKEN: %v\n", err)
		os.Exit(1)
	}
	headSeq, err := verifyHeadSeq(events)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("chain ok: %d events, head seq %d\n", len(events), headSeq)
	if *pub != "" {
		key, err := identity.LoadPub(*pub)
		if err != nil {
			log.Fatal(err)
		}
		if err := eventstore.VerifyCheckpoints(events, key); err != nil {
			fmt.Printf("CHECKPOINTS BROKEN: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("checkpoints ok: history is provably unrewritten")

		// LLD-02 §5: re-verify every human_signature offline, entirely from
		// this event dump plus the node's own public key (node_id — the
		// same base64 string a live server passes to api.WithNodeID).
		nodeID := base64.StdEncoding.EncodeToString(key)
		checked, problems := identity.VerifyApprovalSignaturesOffline(events, nodeID, *requireSignedApprovals)
		fmt.Printf("human signatures: %d human approve/reject decisions checked\n", checked)
		if len(problems) > 0 {
			for _, p := range problems {
				fmt.Println("SIGNATURE PROBLEM:", p)
			}
			os.Exit(1)
		}
		fmt.Println("human signatures ok")
	} else if *requireSignedApprovals {
		log.Fatal("--require-signed-approvals requires --pub (node_id is part of the canonical string it checks)")
	}
}

// verifyHeadSeq returns the head event's seq, or an explicit error on an empty
// journal — `kalita verify` used to index events[len(events)-1] and panic with
// no events in the journal. Extracted so the empty-journal case is testable
// without a postgres (verifyCmd itself requires KALITA_PG_DSN).
func verifyHeadSeq(events []*eventstore.Event) (uint64, error) {
	if len(events) == 0 {
		return 0, errors.New("journal is empty, nothing to verify")
	}
	return events[len(events)-1].Seq, nil
}

// loadVerifyEvents resolves the journal verify reads: a backup directory
// (--file) goes through loadVerifiedBackup (sha256 + chain + checkpoint anchor
// all re-checked — OPS-2, Cycle-A3); otherwise the Postgres journal. Extracted
// so the file path is testable without a database.
func loadVerifyEvents(file string) ([]*eventstore.Event, error) {
	if file != "" {
		_, events, err := loadVerifiedBackup(file)
		return events, err
	}
	dsn := resolveDSN()
	if dsn == "" {
		return nil, errors.New("KALITA_PG_DSN is required")
	}
	ctx := context.Background()
	store, err := eventstore.NewPGStore(ctx, dsn, os.Getenv("KALITA_PG_SCHEMA"), nil)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.All(ctx)
}

// agentCmd registers an actor (agent or human) and prints its bearer token.
// v0: run while the node is stopped — projections are rebuilt at serve start.
func agentCmd(args []string) {
	registerActor(args, eventstore.ActorAgent, "agent")
}

func userCmd(args []string) {
	registerActor(args, eventstore.ActorHuman, "user")
}

func registerActor(args []string, typ eventstore.ActorType, what string) {
	if len(args) < 1 || (args[0] != "add" && args[0] != "revoke") {
		fmt.Printf("usage: kalita %s add|revoke --id <id> [--role <Role>] [--model ...] (KALITA_PG_DSN required)\n", what)
		os.Exit(1)
	}
	action := args[0]
	fs := flag.NewFlagSet(what+" "+action, flag.ExitOnError)
	id := fs.String("id", "", what+" id")
	role := fs.String("role", "", "role from the pack")
	model := fs.String("model", "", "which model powers this agent (journaled)")
	endpoint := fs.String("endpoint", "", "where the agent runs")
	owner := fs.String("owner", "", "who answers for this actor")
	desc := fs.String("desc", "", "description")
	password := fs.String("password", "", "set a password credential (LLD-05 PK-4, humans only) — a UX wrapper over the same bearer token, not a new identity model")
	egressDomains := fs.String("egress-domains", "", "comma-separated whitelist of external domains this worker may call (LLD-09 §3): its OWN self-declaration, reconciled later by `kalita egress-report` against the egress.called facts it journals — not enforced by the kernel")
	_ = fs.Parse(args[1:])
	if *id == "" {
		log.Fatal("--id is required")
	}
	dsn := resolveDSN()
	if dsn == "" {
		log.Fatal("KALITA_PG_DSN is required: identities live in the journal")
	}
	ctx := context.Background()
	store, err := eventstore.NewPGStore(ctx, dsn, os.Getenv("KALITA_PG_SCHEMA"), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	reg := identity.NewRegistry(store)
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "node-admin", Role: "Owner"}
	basis := &eventstore.Basis{Type: "human", ID: "node-admin"}

	if action == "revoke" {
		if err := reg.Disable(ctx, registrar, *id, basis); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s %s revoked: token and signatures are dead\n", what, *id)
		fmt.Println("restart the node to pick up the change (v0)")
		return
	}

	if *role == "" {
		log.Fatal("--role is required for add")
	}
	var meta *identity.ActorMeta
	if *model != "" || *endpoint != "" || *owner != "" || *desc != "" || *egressDomains != "" {
		meta = &identity.ActorMeta{Model: *model, Endpoint: *endpoint, Owner: *owner, Description: *desc,
			EgressDomains: splitComma(*egressDomains)}
	}
	token, err := reg.RegisterWithToken(ctx, registrar, *id, typ, *role, nil, meta, basis)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s %s (role %s) registered\n", what, *id, *role)
	fmt.Printf("bearer token (shown once, only the hash is journaled):\n%s\n", token)
	if *password != "" {
		if typ != eventstore.ActorHuman {
			log.Fatal("--password is only for human actors (LLD-05 PK-4)")
		}
		if err := reg.SetPassword(ctx, registrar, *id, *password, basis); err != nil {
			log.Fatal(err)
		}
		fmt.Println("password set: sign in at POST /api/login with {login, password}")
	}
	fmt.Println("restart the node to pick up the registration (v0)")
}

func usage() {
	fmt.Println("kalita: an executable runtime for business systems in the agent era")
	fmt.Println("usage:")
	fmt.Println("  kalita serve --pack <dir>[,<dir>...] [--listen :8080]   run a node (KALITA_PG_DSN for postgres, else in-memory)")
	fmt.Println("  kalita check --pack <dir>[,<dir>...]                    compile one or more packs together and print diagnostics")
	fmt.Println("  kalita lockdown [lift] [--role Owner]           emergency brake: reject all tokens but a fresh emergency admin (LLD-10 §3)")
	fmt.Println("  kalita product scaffold <pack>                generate a deploy skeleton (compose, status page, brand manifest)")
	fmt.Println("  kalita export --pack <dir> --out era.jsonl      snapshot the journal for a manual era migration (LLD-03 §4)")
	fmt.Println("  kalita import --pack <new dir> --in era.jsonl   start a NEW era from a transformed export (journal must be empty)")
	fmt.Println("  kalita era verify --old <dsn|dump> --new <dsn>  prove the new era's genesis chains to the old era's sealed final state")
	fmt.Println("  kalita upgrade --to <ver|latest> [--dry-run]    docker-compose upgrade with automatic rollback on failure (LLD-03 §2)")
	fmt.Println("  kalita report --since 7d [--role R] [--format md|html] [--pack <dir>]   \"week in numbers\": a pure fold over the journal, zero writes (LLD-09 §2)")
	fmt.Println("  kalita egress-report --since 7d [--format md|html]                      what left the perimeter: declared whitelist vs egress.called facts (LLD-09 §3)")
	fmt.Println("  kalita egress dlq [--pack <dir>]                       list dead-lettered webhook deliveries (D-011)")
	fmt.Println("  kalita egress retry <task-id> [--pack <dir>]           journal a fresh delivery under the same idempotency identity (D-011)")
	fmt.Println("  kalita shadow enable|disable --role R --pack <dir>                      toggle shadow mode for a role (LLD-09 §1) — restart the node to pick up the change (v0)")
	fmt.Println("  kalita mcp contract > docs/MCP-CONTRACT-v1.md                           print the MCP tool contract, generated from internal/mcp/tools.go (RELEASE-1.0 N8)")
	fmt.Println("  kalita version")
}

// resolveDSN resolves KALITA_PG_DSN through the secrets fallback chain fixed
// by LLD-18 §1: a 0600 secrets file (path in KALITA_SECRETS_FILE), then a
// docker/systemd secret file (KALITA_PG_DSN_FILE), then the bare environment
// variable itself — which is allowed (does not break the existing
// docker-compose dev flow) but logged as a warning, "not for prod". A
// misconfigured secrets file (weak permissions) or an explicit but unreadable
// _FILE path fails the process immediately (fail-fast) rather than silently
// falling through to a weaker tier.
func resolveDSN() string {
	loader, err := secrets.NewLoader(os.Getenv("KALITA_SECRETS_FILE"))
	if err != nil {
		log.Fatalf("secrets: %v", err)
	}
	dsn, warn, err := loader.Resolve("KALITA_PG_DSN")
	if err != nil {
		log.Fatalf("secrets: %v", err)
	}
	if warn != "" {
		log.Print("WARNING: " + warn)
	}
	return dsn
}

// lockdownCmd is the operator's one-command emergency brake (LLD-10 §3): stop
// authentication cold except for a single freshly-minted admin token, printed
// once — only its hash is journaled. `kalita lockdown lift` removes the
// curtain again; previously issued tokens are accepted again immediately
// because lockdown never revoked them (a mass revoke is a separate,
// deliberate operation — see internal/identity/lockdown.go). Takes effect on
// the running node without a restart: Authenticate polls the journal on
// every call (Registry.all -> Store.Since), so this is a live journal write,
// not a local file the running process would need to reload.
func lockdownCmd(args []string) {
	action := "apply"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet("lockdown "+action, flag.ExitOnError)
	role := fs.String("role", "Owner", "role granted to the emergency admin token")
	_ = fs.Parse(args)
	if action != "apply" && action != "lift" {
		fmt.Printf("usage: kalita lockdown [lift] [--role <Role>] (KALITA_PG_DSN required)\n")
		os.Exit(1)
	}

	dsn := resolveDSN()
	if dsn == "" {
		log.Fatal("KALITA_PG_DSN is required: lockdown state lives in the journal")
	}
	ctx := context.Background()
	store, err := eventstore.NewPGStore(ctx, dsn, os.Getenv("KALITA_PG_SCHEMA"), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	reg := identity.NewRegistry(store)
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "node-admin", Role: "Owner"}
	basis := &eventstore.Basis{Type: "human", ID: "node-admin"}

	switch action {
	case "apply":
		token, err := reg.Lockdown(ctx, registrar, *role, basis)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("LOCKDOWN ACTIVE: every bearer token is now rejected except the emergency admin below.")
		fmt.Printf("emergency admin token (shown once, only its hash is journaled):\n%s\n", token)
		fmt.Println("lift with: kalita lockdown lift")
	case "lift":
		if err := reg.LiftLockdown(ctx, registrar, basis); err != nil {
			log.Fatal(err)
		}
		fmt.Println("lockdown lifted: previously issued tokens are accepted again (lockdown never revoked them).")
	}
}

// loadPack compiles a single pack directory's .dsl files into one model —
// unchanged from before multi-pack support existed (I-1: an existing
// `--pack <dir>` invocation folds byte-for-byte the same as always).
func loadPack(dir string) (*dsl.Model, []*dsl.Error) {
	files := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Fatalf("read pack dir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".dsl" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			log.Fatal(err)
		}
		files[e.Name()] = string(raw)
	}
	if len(files) == 0 {
		log.Fatalf("no .dsl files in %s", dir)
	}
	return dsl.Compile(files)
}

// loadPacks compiles one or more pack directories and, for more than one,
// merges the resulting models into a single model the node runs (multi-pack
// loading). Each directory is compiled on its own via loadPack — so a
// single-directory call is IDENTICAL to the pre-multi-pack code path — and
// dsl.MergeModels then catches any cross-pack name collision loudly instead
// of silently overwriting one pack's symbol with another's.
func loadPacks(dirs []string) (*dsl.Model, []*dsl.Error) {
	if len(dirs) == 1 {
		return loadPack(dirs[0])
	}
	models := make([]*dsl.Model, 0, len(dirs))
	var allErrs []*dsl.Error
	for _, dir := range dirs {
		model, errs := loadPack(dir)
		if len(errs) > 0 {
			allErrs = append(allErrs, errs...)
			continue
		}
		models = append(models, model)
	}
	if len(allErrs) > 0 {
		return nil, allErrs
	}
	return dsl.MergeModels(models)
}

// collectPackSources flattens every pack directory's .dsl files into one
// sources map with collision-free keys ("<dir>/<file>") — the same texts
// loadPacks compiles, keyed for the engine's definition-sources state
// (DefinitionSources / MCP get_definition, product mission). The pack
// identity lives in each file's `pack X` header, never in the key.
func collectPackSources(dirs []string) map[string]string {
	out := map[string]string{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			log.Fatalf("read pack dir: %v", err)
		}
		for _, e := range entries {
			if filepath.Ext(e.Name()) != ".dsl" {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				log.Fatal(err)
			}
			out[filepath.ToSlash(filepath.Join(dir, e.Name()))] = string(raw)
		}
	}
	return out
}

func check(args []string) {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	pack := fs.String("pack", "", "pack directory, or a comma-separated list of pack directories to load together on one node")
	_ = fs.Parse(args)
	dirs := splitComma(*pack)
	if len(dirs) == 0 {
		log.Fatal("--pack is required")
	}
	model, errs := loadPacks(dirs)
	for _, e := range errs {
		fmt.Println(e.Error())
	}
	if len(errs) > 0 {
		os.Exit(1)
	}
	fmt.Printf("ok: pack %s, %d entities, %d roles\n",
		model.Manifest.Name, len(model.Entities), len(model.Roles))
}

func serve(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	pack := fs.String("pack", "", "genesis pack directory, or a comma-separated list of pack directories to load together on this node (optional: empty node accepts its first pack via propose_change)")
	listen := fs.String("listen", "127.0.0.1:8080", "listen address")
	approver := fs.String("approver", "Owner", "role whose human signature applies definition changes")
	egressDomains := fs.String("egress-domains", os.Getenv("KALITA_EGRESS_DOMAINS"), "comma-separated allowlist of hosts the node's webhook runner may deliver to (KALITA_EGRESS_DOMAINS); empty = no outbound webhooks (fail-closed, Cycle-B1)")
	tlsCert := fs.String("tls-cert", "", "TLS certificate file")
	tlsKey := fs.String("tls-key", "", "TLS key file")
	dataDir := fs.String("data-dir", "kalita-data", "node key directory (node.key/node.pub)")
	uiDir := fs.String("ui-dir", "", "serve a UI directory from disk (e.g. ./web); empty = embedded if built with -tags embedui, else API-only")
	filesDir := fs.String("files-dir", "", "directory for uploaded files (content-addressed); empty = uploads disabled")
	brand := fs.String("brand", "", "white-label product name shown in the UI instead of Kalita")
	brandAccent := fs.String("brand-accent", "", "UI accent color (e.g. #4da3ff)")
	brandTagline := fs.String("brand-tagline", "", "short tagline under the product name")
	devHeaders := fs.Bool("insecure-dev-auth", false, "enable X-Actor-* header identity (local development ONLY)")
	insecureHTTP := fs.Bool("insecure-http", false, "allow plaintext HTTP on non-loopback addresses (NOT recommended)")
	demo := fs.Bool("demo", false, "seed demo users and data on an empty journal, print ready tokens")
	searchBackend := fs.String("search-backend", "", "RAG search worker URL — mounts /api/search (e.g. http://127.0.0.1:8200)")
	searchScope := fs.String("search-scope", "Workspace", "entity whose visible records bound a search")
	searchLog := fs.String("search-log", "SearchQuery", "entity to journal each query into")
	connectorEntity := fs.String("connector-entity", "Connector", "entity holding connector records (name→backend) for the /api/call capability proxy")
	shutdownTimeout := fs.Duration("shutdown-timeout", 25*time.Second, "how long SIGTERM/SIGINT waits for in-flight requests to drain before forcing the listener closed")
	forceDowngrade := fs.Bool("force-downgrade", false, "start even though the journal was last run by a newer version (LLD-03 §1); events of kinds this binary does not recognize are skipped and counted in a warning, and a node.downgraded event is recorded")
	requireHumanSig := fs.Bool("require-human-signature", false, "require a verified WebAuthn passkey signature on every human approve/reject (LLD-02 §5) — the production gate; off by default so demo/dev nodes keep working unchanged")
	logFormat := fs.String("log-format", "text", "text | json — structured JSON log lines for an in-perimeter collector (PROD-READINESS O5)")
	_ = fs.Parse(args)

	if *logFormat == "json" {
		log.SetOutput(&jsonLogWriter{})
	}

	tls := *tlsCert != "" && *tlsKey != ""
	if !tls && !*insecureHTTP && !isLoopback(*listen) {
		log.Fatal("refusing to listen on a non-loopback address without TLS: " +
			"pass --tls-cert/--tls-key, or --insecure-http if a TLS-terminating proxy is in front (SECURITY.md P0)")
	}

	var model *dsl.Model
	var bootSources map[string]string
	if dirs := splitComma(*pack); len(dirs) > 0 {
		var errs []*dsl.Error
		model, errs = loadPacks(dirs)
		if len(errs) > 0 {
			for _, e := range errs {
				fmt.Println(e.Error())
			}
			os.Exit(1)
		}
		bootSources = collectPackSources(dirs)
	} else {
		model, _ = dsl.Compile(map[string]string{})
		log.Print("genesis: empty definition — the first pack arrives via propose_change + signature")
	}

	// ctx is cancelled on SIGTERM/SIGINT (below) so the background tick and
	// checkpoint loops stop cleanly instead of firing mid-shutdown; one-shot
	// boot operations above/below run before cancellation is ever possible.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var store eventstore.Store
	if dsn := resolveDSN(); dsn != "" {
		pg, err := eventstore.NewPGStore(ctx, dsn, os.Getenv("KALITA_PG_SCHEMA"), nil)
		if err != nil {
			log.Fatalf("postgres: %v", err)
		}
		defer pg.Close()
		store = pg
		log.Print("journal: postgresql")
	} else {
		store = eventstore.NewMemStore(nil)
		log.Print("journal: IN-MEMORY (dev only, state is lost on exit; set KALITA_PG_DSN)")
	}

	// LLD-03 §1: refuse to start an older binary on a journal a newer one has
	// already run against, unless the operator forces it — before anything
	// else touches the journal (engine.New's own replay included).
	enforceVersionGuard(ctx, store, *forceDowngrade)

	reg := identity.NewRegistry(store)
	engOpts := []engine.Option{
		engine.WithVerifier(reg.VerifySignature),
		engine.WithDefinitionApprover(*approver),
	}
	if bootSources != nil {
		engOpts = append(engOpts, engine.WithSources(bootSources))
	}
	if *requireHumanSig {
		engOpts = append(engOpts, engine.WithRequireHumanSignature())
	}
	eng, err := engine.New(ctx, model, store, engOpts...)
	if err != nil {
		log.Fatalf("engine: %v", err)
	}
	// node key: loaded here (rather than at the original later boot-stamp
	// spot) because the API layer needs its public key NOW, for LLD-02 §2's
	// node_id — the canonical string component that makes a passkey
	// signature non-portable between nodes.
	nodeKey, err := identity.LoadOrCreateNodeKey(*dataDir)
	if err != nil {
		log.Fatalf("node key: %v", err)
	}
	var apiOpts []api.Option
	apiOpts = append(apiOpts, api.WithNodeID(base64.StdEncoding.EncodeToString(nodeKey.Public().(ed25519.PublicKey))))
	if *requireHumanSig {
		apiOpts = append(apiOpts, api.WithRequireHumanSignature())
		log.Print("--require-human-signature is on: human approve/reject decisions require a verified passkey (LLD-02 §5)")
	}
	if *devHeaders {
		log.Print("WARNING: --insecure-dev-auth is on — X-Actor headers grant any identity. Local development only.")
		apiOpts = append(apiOpts, api.WithDevHeaders())
	}
	if *searchBackend != "" {
		apiOpts = append(apiOpts, api.WithRAGSearch(*searchBackend, *searchScope, *searchLog, "Searcher"))
		log.Printf("RAG search enabled: /api/search -> %s (scope %s)", *searchBackend, *searchScope)
	}
	if *connectorEntity != "" && *connectorEntity != "Connector" {
		apiOpts = append(apiOpts, api.WithCallProxy(*connectorEntity))
	}
	// Brand precedence (LLD-05 §3, PK-4): explicit --brand* flag > KALITA_BRAND
	// env > the pack's product{} manifest as a default > "Kalita". A product
	// pack ships branded out of the box; an operator can still override it.
	{
		name, accent, tagline := *brand, *brandAccent, *brandTagline
		if name == "" {
			name = os.Getenv("KALITA_BRAND")
		}
		if name == "" && model.Product != nil {
			name = model.Product.Name
			if accent == "" {
				accent = model.Product.Colors["accent"]
			}
		}
		if name != "" {
			apiOpts = append(apiOpts, api.WithBrand(name, accent, tagline))
		}
	}
	if secret := os.Getenv("KALITA_BOOTSTRAP_SECRET"); secret != "" {
		roles := []string{"Indexer", "Searcher"}
		if r := os.Getenv("KALITA_BOOTSTRAP_ROLES"); r != "" {
			roles = splitComma(r)
		}
		apiOpts = append(apiOpts, api.WithBootstrap(secret, roles))
		log.Printf("worker bootstrap enabled for roles %v", roles)
	}
	mux := http.NewServeMux()
	// The search_perimeter node-tool shares REST /api/search's governance core
	// (internal/api.SearchPerimeter), so it is wired from the SAME --search-*
	// flags: an agent over MCP gets retrieval only when a human over REST does.
	var mcpOpts []mcp.Option
	mcpOpts = append(mcpOpts, mcp.WithVersion(version)) // OPS-1: serverInfo reports the binary's real version
	if *searchBackend != "" {
		mcpOpts = append(mcpOpts, mcp.WithRAGSearch(*searchBackend, *searchScope, *searchLog, "Searcher"))
	}
	mux.Handle("/mcp", mcp.New(eng, reg, mcpOpts...))
	apiSrv := api.New(eng, reg, apiOpts...)
	mux.Handle("/api/", apiSrv)
	// liveness/readiness live on the API server's mux but must be reachable at
	// the bare top-level paths orchestrators probe (GET /healthz, GET /readyz) —
	// without this they fell through to the UI catch-all and probes got HTML.
	mux.Handle("/healthz", apiSrv)
	mux.Handle("/readyz", apiSrv)
	mux.Handle("/metrics", apiSrv)
	// UI source, by ADR-004: --ui-dir (disk, no rebuild) > embedded (build tag) > API-only
	var ui http.Handler
	switch {
	case webui.DirHandler(*uiDir) != nil:
		ui = webui.DirHandler(*uiDir)
		log.Printf("UI: serving %s from disk", *uiDir)
	case webui.Embedded():
		ui = webui.EmbeddedHandler()
		log.Print("UI: embedded renderer")
	default:
		ui = webui.APIOnly()
		log.Print("UI: none (API-only) — use --ui-dir, -tags embedui, or @kalita/sdk")
	}
	mux.Handle("/", ui)
	// packages/ (kalita-ui, kalita-sdk) is served under its own prefix,
	// separate from --ui-dir, because it is a sibling directory of the UI
	// root, not a subdirectory of it (LLD-05 PK-1: packages/kalita-ui sits
	// next to packages/kalita-sdk at the repo root, matching npm-package
	// layout, while --ui-dir's http.FileServer can only reach files inside
	// its own root — see internal/webui/webui.go). Only mounted when serving
	// the UI from disk; the embedded build instead gets packages/kalita-ui
	// copied INTO the embedded tree by scripts/embed-ui.sh, so the single
	// embedded FileServer already covers it with no second mount needed.
	if *uiDir != "" {
		if pkgs := webui.DirHandler("packages"); pkgs != nil {
			mux.Handle("/packages/", http.StripPrefix("/packages/", pkgs))
		}
	}
	handler := api.Secure(mux)

	if *filesDir != "" {
		bs, err := engine.NewDiskBlobStore(*filesDir)
		if err != nil {
			log.Fatalf("files-dir: %v", err)
		}
		eng.SetBlobStore(bs)
		log.Printf("file uploads enabled: %s", *filesDir)
	}

	if *demo {
		seedDemo(ctx, eng, reg, store)
	}

	// boot stamp: every start is a journaled, versioned event (nodeKey
	// itself was already loaded above, before apiOpts, for LLD-02's node_id)
	_, _ = store.Append(ctx, eventstore.AppendInput{
		Actor:   eventstore.Actor{Type: eventstore.ActorSystem, ID: "node"},
		Kind:    eventstore.NodeStarted,
		Payload: []byte(fmt.Sprintf(`{"version":%q}`, version)),
		Basis:   &eventstore.Basis{Type: "rule", ID: "boot"},
	})

	// the heartbeat: automation rules (schedules, stuck escalations, lease
	// sweeps) fire here — a node without Tick is a node where nothing happens.
	// Exits on ctx cancellation (SIGTERM/SIGINT below), not just process death.
	go func() {
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if err := eng.Tick(context.Background()); err != nil {
					log.Printf("tick: %v", err)
				}
			}
		}
	}()
	// the webhook runner (Cycle-B1 / D-011, English-first hardening): delivers
	// TaskWebhook intentions under the node-level egress whitelist,
	// at-least-once with a per-firing idempotency identity, per-attempt
	// receipts, failure classification and a dead-letter listing.
	// An EMPTY allowlist leaves the runner disabled (fail-closed): tasks stay
	// pending rather than being failed, so configuring KALITA_EGRESS_DOMAINS
	// later does not lose their intent.
	webhookDomains := parseEgressDomains(*egressDomains)
	webhookSecrets, secretsConfigured := loadEgressSecrets()
	if len(webhookDomains) > 0 {
		go func() {
			tick := time.NewTicker(10 * time.Second)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					n := runWebhookDeliveries(context.Background(), eng, webhookDomains, webhookSecrets, secretsConfigured, deliverOnce)
					if n > 0 {
						log.Printf("webhook runner: settled %d task(s)", n)
					}
				}
			}
		}()
		log.Printf("webhook runner: %d allowed egress domain(s) [%s]", len(webhookDomains), strings.Join(webhookDomains, ", "))
	} else {
		log.Printf("webhook runner: DISABLED — KALITA_EGRESS_DOMAINS empty (fail-closed, no outbound delivery)")
	}
	// hourly checkpoint seals the head with the node key (plus one at boot)
	go func() {
		seal := func() {
			if _, err := eventstore.Seal(context.Background(), store, "node", nodeKey); err != nil &&
				err != eventstore.ErrNothingToSeal {
				log.Printf("checkpoint: %v", err)
			}
		}
		seal()
		tick := time.NewTicker(time.Hour)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				seal()
			}
		}
	}()

	packName := "(genesis)"
	if m := eng.Model().Manifest; m != nil {
		packName = m.Name
	}
	log.Printf("pack %s: %d entities, def_version %d", packName, len(eng.Model().Entities), eng.DefVersion())
	log.Printf("listening on %s — UI + REST /api (bearer tokens: kalita user add) + MCP /mcp", *listen)
	if !tls && !isLoopback(*listen) {
		log.Print("WARNING: serving plaintext HTTP on a non-loopback address — bearer tokens travel in the clear. " +
			"Set --tls-cert/--tls-key, or terminate TLS in a reverse proxy in front of the node (SECURITY.md P0-2).")
	}

	// Graceful shutdown: SIGTERM/SIGINT drains in-flight requests via
	// http.Server.Shutdown (stops accepting new connections, waits for active
	// ones) up to --shutdown-timeout, then forces the listener closed. store
	// is closed cleanly by the `defer pg.Close()` above, which now actually
	// runs — this function returns normally instead of exiting through
	// log.Fatal (log.Fatal calls os.Exit, which would have skipped it).
	srv := &http.Server{Addr: *listen, Handler: handler}
	listenFn := srv.ListenAndServe
	if tls {
		listenFn = func() error { return srv.ListenAndServeTLS(*tlsCert, *tlsKey) }
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	gracefulServe(srv, listenFn, *shutdownTimeout, sigCh, cancel)
	log.Print("kalita: stopped serving")
}

// gracefulServe runs listenFn (srv.ListenAndServe or ListenAndServeTLS,
// already bound to srv) until either it returns on its own or a signal
// arrives on stop. On a stop signal: onStop runs first (in serve(), this
// cancels the background tick/checkpoint loops), then srv.Shutdown drains
// in-flight requests up to timeout before forcing srv.Close(). Extracted out
// of serve() so the drain/force-close mechanics are unit-testable against a
// synthetic stop signal — sending a real SIGTERM/SIGINT to one's own test
// process is not portable (Windows in particular has no such delivery path).
func gracefulServe(srv *http.Server, listenFn func() error, timeout time.Duration, stop <-chan os.Signal, onStop func()) {
	serveErr := make(chan error, 1)
	go func() { serveErr <- listenFn() }()

	select {
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	case sig := <-stop:
		log.Printf("received %s: draining in-flight requests (up to %s) before exit", sig, timeout)
		if onStop != nil {
			onStop()
		}
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), timeout)
		defer cancelShutdown()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown did not finish in time, forcing listener closed: %v", err)
			_ = srv.Close()
		}
	}
}

func splitComma(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func isLoopback(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
