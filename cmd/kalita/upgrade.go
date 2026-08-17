package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/avangerus/kalita/internal/eventstore"
)

// enforceVersionGuard is LLD-03 §1's startup gate, wired into `kalita serve`
// (main.go, right before engine.New — before anything else touches the
// journal): refuse to run an older binary against a journal a newer one has
// already recorded node.upgraded against, unless --force-downgrade is given.
func enforceVersionGuard(ctx context.Context, store eventstore.Store, forceDowngrade bool) {
	events, err := store.All(ctx)
	if err != nil {
		log.Fatalf("version guard: reading journal: %v", err)
	}
	res := eventstore.GuardStart(events, version, forceDowngrade)
	if res.Blocked {
		log.Fatal(res.RefusalMessage(version))
	}
	if !res.Downgrade {
		return
	}
	// forced downgrade: never silent (LLD-03 §1, I-3) — count and print
	// every kind this binary does not recognize before proceeding.
	for k, n := range res.UnknownKinds {
		log.Printf("WARNING: --force-downgrade: %d event(s) of kind %q are unknown to this binary and will be skipped when folding the journal", n, k)
	}
	payload, _ := json.Marshal(eventstore.DowngradedPayload{From: res.JournalVersion, To: version, Forced: true})
	if _, err := store.Append(ctx, eventstore.AppendInput{
		Actor:   eventstore.Actor{Type: eventstore.ActorSystem, ID: "node"},
		Kind:    eventstore.NodeDowngraded,
		Payload: payload,
		Basis:   &eventstore.Basis{Type: "human", ID: "operator:force-downgrade"},
		SchemaV: eventstore.CurrentSchemaV,
	}); err != nil {
		log.Fatalf("version guard: recording node.downgraded: %v", err)
	}
	log.Printf("--force-downgrade: journal was last run at %s, this binary is %s — node.downgraded recorded, starting anyway", res.JournalVersion, version)
}

// upgradeReport is the operator-facing account of the run, printed at the
// end regardless of outcome (LLD-03 §2: "отчёт оператору" / "отчёт FAILURE")
// — the runbook's steps must be readable FROM the command's own output, not
// a separate document, since the operator running it may not be technical.
type upgradeReport struct {
	steps []string
	start time.Time
}

func (r *upgradeReport) step(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	r.steps = append(r.steps, line)
	fmt.Printf("[%s] %s\n", time.Since(r.start).Round(time.Millisecond), line)
}

func (r *upgradeReport) print(ok bool, summary string) {
	fmt.Println(strings.Repeat("-", 60))
	if ok {
		fmt.Printf("UPGRADE OK: %s\n", summary)
	} else {
		fmt.Printf("UPGRADE FAILURE: %s\n", summary)
	}
	for i, s := range r.steps {
		fmt.Printf("  %2d. %s\n", i+1, s)
	}
}

// upgradeInsecureSkipTLSVerifyDefault is the single source of truth for
// --insecure-skip-tls-verify's default (SEC-3, Cycle-A3): FALSE — a
// self-signed deployment must pass the flag explicitly. The regression test
// pins this default so it can never silently drift back to true.
const upgradeInsecureSkipTLSVerifyDefault = false

// upgradeCmd is `kalita upgrade` (LLD-03 §2), the docker-compose-first
// orchestrator: preflight -> backup -> pull+verify -> stop -> start new ->
// smoke -> node.upgraded, with an automatic rollback to the old image on a
// failed start/smoke. Nothing is journaled before the final step succeeds
// (LLD-03 §2: "провал = ноль следов, кроме бэкапа") — every step up to and
// including the smoke check only touches docker and the filesystem.
func upgradeCmd(args []string) {
	fs := flag.NewFlagSet("upgrade", flag.ExitOnError)
	to := fs.String("to", "", "target version tag to upgrade to (required)")
	dryRun := fs.Bool("dry-run", false, "run preflight + backup + pull/verify only (steps 1-3), then report — no stop/start/journal")
	image := fs.String("image", "", "full image repository to pull, WITHOUT a tag (e.g. ghcr.io/avangerus/kalita) — required, no registry is assumed")
	composeFile := fs.String("compose-file", "docker-compose.yml", "compose file for the deployment")
	service := fs.String("service", "kalita", "compose service name of the node")
	pgService := fs.String("postgres-service", "postgres", "compose service name of postgres")
	pgUser := fs.String("postgres-user", "postgres", "postgres role used for pg_dump/psql")
	pgDB := fs.String("postgres-db", "kalita", "postgres database name")
	backupDir := fs.String("backup-dir", "backups", "host directory (a mounted volume) to write the pg_dump backup into")
	cosignKey := fs.String("cosign-key", "cosign.pub", "public key file to verify the release image's cosign signature")
	skipSigVerify := fs.Bool("skip-signature-verify", false, "proceed even if the image signature cannot be verified (cosign/key missing) — NOT recommended, use only until release signing is set up")
	readyTimeout := fs.Duration("ready-timeout", 10*time.Minute, "how long to wait for /readyz after starting the new version")
	healthURL := fs.String("health-url", "https://127.0.0.1:8443", "base URL of the node for the smoke checks (/healthz, /readyz)")
	insecureSkipTLSVerify := fs.Bool("insecure-skip-tls-verify", upgradeInsecureSkipTLSVerifyDefault, "skip TLS certificate verification for smoke checks (a self-signed deployment must pass this EXPLICITLY)")
	_ = fs.Parse(args)

	if *to == "" {
		log.Fatal("--to is required (a version tag, or 'latest')")
	}
	if *image == "" {
		log.Fatal("--image is required: this repository does not yet publish to a registry by default " +
			"(no CI publish job, no cosign key committed) — set up image publishing first, then pass its repository here")
	}

	ctx := context.Background()
	rep := &upgradeReport{start: time.Now()}

	// --- step 1: preflight -------------------------------------------------
	dsn := resolveDSN()
	if dsn == "" {
		log.Fatal("KALITA_PG_DSN is required: upgrade inspects the journal directly")
	}
	store, err := eventstore.NewPGStore(ctx, dsn, os.Getenv("KALITA_PG_SCHEMA"), nil)
	if err != nil {
		log.Fatalf("preflight FAILED: connect to journal: %v", err)
	}
	defer store.Close()

	headSeq, headHash, err := store.Head(ctx)
	if err != nil {
		log.Fatalf("preflight FAILED: journal head: %v", err)
	}
	rep.step("preflight: journal head is seq %d (%x)", headSeq, headHash)

	events, err := store.All(ctx)
	if err != nil {
		log.Fatalf("preflight FAILED: reading journal: %v", err)
	}
	if err := eventstore.VerifyTailFromLastCheckpoint(events); err != nil {
		rep.print(false, "preflight: journal verify failed, refusing to touch anything: "+err.Error())
		os.Exit(1)
	}
	recordsBefore := countRecordEvents(events)
	fromVersion := eventstore.CurrentRunningVersion(events)
	rep.step("preflight: verify ok (tail from last checkpoint), currently running %s, %d record event(s) so far", fromVersion, recordsBefore)

	if free, err := freeDiskBytes(*backupDir); err != nil {
		rep.step("preflight: could not determine free disk space for %s (%v) — continuing, check manually", *backupDir, err)
	} else {
		rep.step("preflight: %.1f GiB free at %s", float64(free)/(1<<30), *backupDir)
		const minFreeBytes = 1 << 30 // 1 GiB floor — a pg_dump plus room to write it
		if free < minFreeBytes {
			rep.print(false, fmt.Sprintf("preflight: only %.2f GiB free at %s, refusing to start a backup", float64(free)/(1<<30), *backupDir))
			os.Exit(1)
		}
	}

	if pgVer, err := composeExecOutput(*composeFile, *pgService, "psql", "-U", *pgUser, "-tAc", "show server_version"); err != nil {
		rep.step("preflight: could not read postgres version via docker compose exec (%v) — continuing", err)
	} else {
		rep.step("preflight: postgres server_version %s", strings.TrimSpace(pgVer))
	}

	if *dryRun {
		rep.step("--dry-run: stopping after preflight (steps 1-3 only)")
	}

	// --- step 2: backup -----------------------------------------------------
	if err := os.MkdirAll(*backupDir, 0o755); err != nil {
		rep.print(false, "backup: could not create backup dir: "+err.Error())
		os.Exit(1)
	}
	backupName := fmt.Sprintf("%s_from-%s.sql", time.Now().UTC().Format("20060102T150405Z"), fromVersion)
	backupPath := filepath.Join(*backupDir, backupName)
	if err := pgDumpTo(*composeFile, *pgService, *pgUser, *pgDB, backupPath); err != nil {
		rep.print(false, "backup FAILED, refusing to proceed: "+err.Error())
		os.Exit(1)
	}
	rep.step("backup: pg_dump written to %s", backupPath)

	// --- step 3: pull + verify signature -------------------------------------
	imageRef := fmt.Sprintf("%s:%s", *image, *to)
	if out, err := runCapture("docker", "pull", imageRef); err != nil {
		rep.print(false, fmt.Sprintf("pull FAILED for %s: %v\n%s", imageRef, err, out))
		os.Exit(1)
	}
	rep.step("pull: %s", imageRef)

	if _, err := os.Stat(*cosignKey); err != nil {
		if *skipSigVerify {
			rep.step("WARNING: signature verification skipped (--skip-signature-verify, no %s in repo)", *cosignKey)
		} else {
			rep.print(false, fmt.Sprintf("signature verify FAILED: %s not found — either commit the release cosign key, "+
				"or pass --skip-signature-verify (not recommended) to proceed without it", *cosignKey))
			os.Exit(1)
		}
	} else if out, err := runCapture("cosign", "verify", "--key", *cosignKey, imageRef); err != nil {
		if *skipSigVerify {
			rep.step("WARNING: signature verify failed but --skip-signature-verify was given: %v", err)
		} else {
			rep.print(false, fmt.Sprintf("signature verify FAILED for %s: %v\n%s", imageRef, err, out))
			os.Exit(1)
		}
	} else {
		rep.step("signature verify ok: %s", imageRef)
	}

	if *dryRun {
		rep.print(true, fmt.Sprintf("dry run complete: %s is pulled and verified, ready for a real upgrade", imageRef))
		return
	}

	// From here on, a failure triggers automatic rollback (LLD-03 §2): the
	// journal has not been touched yet (steps 1-3 are read-only plus the
	// backup, which lives outside the journal), so restarting the OLD image
	// replays it back to exactly where it was.
	rollback := func(reason string) {
		rep.step("ROLLBACK: %s", reason)
		if _, err := runCapture("docker", "compose", "-f", *composeFile, "up", "-d", "--no-deps", *service); err != nil {
			rep.step("ROLLBACK FAILED to restart the old version: %v — manual intervention required, restore from %s if needed", err, backupPath)
			rep.print(false, "rollback itself failed — see backup at "+backupPath)
			os.Exit(1)
		}
		if err := waitReady(*healthURL, *insecureSkipTLSVerify, 2*time.Minute); err != nil {
			rep.step("old version restarted but did not become ready: %v", err)
		} else {
			rep.step("old version (%s) is back up and ready — journal was never touched by this failed attempt", fromVersion)
		}
		rep.print(false, reason)
		os.Exit(1)
	}

	// --- step 4: stop the node (postgres stays up) ---------------------------
	if out, err := runCapture("docker", "compose", "-f", *composeFile, "stop", *service); err != nil {
		rep.print(false, fmt.Sprintf("stop FAILED: %v\n%s", err, out))
		os.Exit(1)
	}
	rep.step("stopped %s (postgres untouched)", *service)

	// --- step 5: start the new version, wait for /readyz ---------------------
	env := append(os.Environ(), "KALITA_IMAGE="+imageRef)
	if out, err := runCaptureEnv(env, "docker", "compose", "-f", *composeFile, "up", "-d", "--no-deps", "--no-build", *service); err != nil {
		rollback(fmt.Sprintf("start %s FAILED: %v\n%s", imageRef, err, out))
		return
	}
	rep.step("started %s", imageRef)
	if err := waitReady(*healthURL, *insecureSkipTLSVerify, *readyTimeout); err != nil {
		rollback(fmt.Sprintf("new version never became ready within %s: %v", *readyTimeout, err))
		return
	}
	rep.step("new version is ready (/readyz)")

	// --- step 6: smoke -------------------------------------------------------
	if err := smokeCheck(*healthURL, *insecureSkipTLSVerify); err != nil {
		rollback("smoke check FAILED: " + err.Error())
		return
	}
	rep.step("smoke: /healthz, /readyz ok")

	afterEvents, err := store.All(ctx)
	if err != nil {
		rollback("smoke: could not read journal after start: " + err.Error())
		return
	}
	recordsAfter := countRecordEvents(afterEvents)
	if recordsAfter != recordsBefore {
		rollback(fmt.Sprintf("smoke FAILED: record event count changed across the upgrade (%d -> %d) — this must never happen", recordsBefore, recordsAfter))
		return
	}
	rep.step("smoke: record event count unchanged (%d)", recordsAfter)

	// --- step 7: append node.upgraded, report --------------------------------
	sha := imageDigest(imageRef)
	payload, _ := json.Marshal(eventstore.UpgradedPayload{From: fromVersion, To: *to, BinarySHA256: sha})
	if _, err := store.Append(ctx, eventstore.AppendInput{
		Actor:   eventstore.Actor{Type: eventstore.ActorSystem, ID: "node"},
		Kind:    eventstore.NodeUpgraded,
		Payload: payload,
		Basis:   &eventstore.Basis{Type: "human", ID: "operator:upgrade"},
		SchemaV: eventstore.CurrentSchemaV,
	}); err != nil {
		// The new binary is already live and smoke-tested; the ONLY thing
		// that failed is recording the fact. Report loudly rather than
		// silently rolling back a working, verified upgrade.
		rep.print(false, "node.upgraded FAILED to journal (the new version IS running and passed smoke — fix the journal write and record it manually): "+err.Error())
		os.Exit(1)
	}
	rep.step("recorded node.upgraded {from: %s, to: %s}", fromVersion, *to)
	rep.print(true, fmt.Sprintf("upgraded %s -> %s", fromVersion, *to))
}

// countRecordEvents counts record.created/record.updated/record.imported
// events — the "счётчик записей" of LLD-03 §2 step 6 ("== до"), read
// straight from the journal so the smoke check needs no /api/meta endpoint
// (none exists yet in internal/api — out of this change's zone; see report).
func countRecordEvents(events []*eventstore.Event) int {
	n := 0
	for _, ev := range events {
		switch ev.Kind {
		case eventstore.RecordCreated, eventstore.RecordUpdated, eventstore.RecordImported:
			n++
		}
	}
	return n
}

func smokeCheck(baseURL string, insecureSkipTLSVerify bool) error {
	client := httpClient(insecureSkipTLSVerify, 10*time.Second)
	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := client.Get(strings.TrimRight(baseURL, "/") + path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return fmt.Errorf("%s: status %d", path, resp.StatusCode)
		}
	}
	return nil
}

func waitReady(baseURL string, insecureSkipTLSVerify bool, timeout time.Duration) error {
	client := httpClient(insecureSkipTLSVerify, 5*time.Second)
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(strings.TrimRight(baseURL, "/") + "/readyz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("/readyz did not become ready within %s: %v", timeout, lastErr)
}

func httpClient(insecureSkipTLSVerify bool, timeout time.Duration) *http.Client {
	tr := &http.Transport{}
	if insecureSkipTLSVerify {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // self-signed dev/compose cert by design
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}

// pgDumpTo runs pg_dump INSIDE the postgres compose service (so it shares
// that container's credentials/network — no DSN parsing needed here) and
// writes its stdout to path.
func pgDumpTo(composeFile, pgService, pgUser, pgDB, path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.Command("docker", "compose", "-f", composeFile, "exec", "-T", pgService,
		"pg_dump", "-U", pgUser, "-d", pgDB)
	cmd.Stdout = f
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}

func composeExecOutput(composeFile, service string, cmdArgs ...string) (string, error) {
	args := append([]string{"compose", "-f", composeFile, "exec", "-T", service}, cmdArgs...)
	return runCapture("docker", args...)
}

func runCapture(name string, args ...string) (string, error) {
	return runCaptureEnv(nil, name, args...)
}

func runCaptureEnv(env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// freeDiskBytes shells out to `df` (the deploy target for this docker-first
// upgrade is a Linux host; df is universal there). If df is unavailable
// (e.g. a non-Linux dev box), the caller treats the error as best-effort and
// continues rather than blocking preflight on an informational check.
func freeDiskBytes(dir string) (int64, error) {
	out, err := runCapture("df", "-Pk", dir)
	if err != nil {
		return 0, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return 0, fmt.Errorf("unexpected df output: %s", out)
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, fmt.Errorf("unexpected df output: %s", out)
	}
	var availKB int64
	if _, err := fmt.Sscanf(fields[3], "%d", &availKB); err != nil {
		return 0, err
	}
	return availKB * 1024, nil
}

// imageDigest resolves the pulled image's content digest for
// UpgradedPayload.BinarySHA256 (LLD-03 §1) — the provable "which exact bits
// are running" identity, stronger than a mutable tag.
func imageDigest(imageRef string) string {
	out, err := runCapture("docker", "inspect", "--format={{index .RepoDigests 0}}", imageRef)
	if err != nil {
		return ""
	}
	digest := strings.TrimSpace(out)
	if i := strings.LastIndex(digest, "@sha256:"); i >= 0 {
		return digest[i+len("@sha256:"):]
	}
	return digest
}
