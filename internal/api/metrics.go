package api

import (
	"fmt"
	"net/http"
	"runtime"
	"time"
)

// processStart is the process lifetime anchor for kalita_uptime_seconds.
var processStart = time.Now()

// metrics serves the node's Prometheus-format metrics (PROD-READINESS O5). It
// is public like /healthz (an in-network scraper has no bearer token) and
// reports the red states an operator's dashboard watches: journal head, the
// HITL approval backlog, parked pack/mutation proposals, and ingest flags —
// all from the engine, zero external dependencies.
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	g := func(help, typ, name string, value int64) string {
		return fmt.Sprintf("# HELP %s %s\n# TYPE %s %s\n%s %d\n", name, help, name, typ, name, value)
	}

	head, err := s.eng.HeadSeq(r.Context())
	if err != nil {
		head = 0
	}

	approvals := 0
	if m := s.eng.Model(); m != nil {
		for role := range m.Roles {
			approvals += len(s.eng.PendingApprovals(role))
		}
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	egress := s.eng.EgressMetrics(time.Now())

	w.Write([]byte(
		g("Append-only journal head sequence (events written).", "gauge", "kalita_journal_seq", int64(head)) +
			g("Compiled definition version of the loaded packs.", "gauge", "kalita_def_version", int64(s.eng.DefVersion())) +
			g("Human-in-the-loop approvals awaiting a signature.", "gauge", "kalita_pending_approvals", int64(approvals)) +
			g("Pack proposals parked for a human signature.", "gauge", "kalita_pending_proposals", int64(len(s.eng.PendingProposals()))) +
			g("Mutation proposals parked for review.", "gauge", "kalita_pending_mutation_proposals", int64(len(s.eng.PendingMutationProposals()))) +
			g("Ingest flags raised by the perimeter detector.", "gauge", "kalita_ingest_flags", int64(len(s.eng.IngestFlags()))) +
			g("Seconds since the node process started.", "gauge", "kalita_uptime_seconds", int64(time.Since(processStart).Seconds())) +
			g("Number of goroutines.", "gauge", "kalita_goroutines", int64(runtime.NumGoroutine())) +
			g("Bytes of allocated heap objects.", "gauge", "kalita_go_memstats_alloc_bytes", int64(ms.Alloc)) +
			// D-011 egress observability: queued / delivered / dead-lettered
			// deliveries, per-attempt receipts, retries, queue age and the
			// latency sum+count pair (an average = sum/count, Prometheus-safe).
			g("Open webhook deliveries awaiting the runner.", "gauge", "kalita_egress_queued", int64(egress.Queued)) +
			g("Webhook deliveries acknowledged by the destination.", "gauge", "kalita_egress_delivered", int64(egress.Delivered)) +
			g("Webhook deliveries dead-lettered after exhausting retries.", "gauge", "kalita_egress_dead_lettered", int64(egress.DeadLettered)) +
			g("Delivery attempts journaled as webhook.attempt receipts.", "gauge", "kalita_egress_attempts", int64(egress.Attempts)) +
			g("Delivery attempts that were retries (attempt > 1).", "gauge", "kalita_egress_retries", int64(egress.Retries)) +
			g("Seconds since the oldest open delivery was journaled.", "gauge", "kalita_egress_oldest_pending_seconds", int64(egress.OldestPendingSec)) +
			g("Sum of delivered-attempt latencies in milliseconds.", "gauge", "kalita_egress_latency_sum_ms", egress.LatencySumMS) +
			g("Count of delivered attempts (average latency = sum/count).", "gauge", "kalita_egress_latency_count", egress.LatencyCount),
	))
}
