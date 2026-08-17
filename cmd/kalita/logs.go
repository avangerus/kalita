package main

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// jsonLogWriter emits structured JSON log lines (Master Goal §10,
// PROD-READINESS O5) instead of plain text: {"ts","level","msg"}. An
// operator's in-perimeter collector (Loki/ELK) parses these directly; zero
// external SaaS dependencies (SECURITY rule 6).
type jsonLogWriter struct {
	mu sync.Mutex
}

func (w *jsonLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	msg := string(p)
	for len(msg) > 0 && (msg[len(msg)-1] == '\n' || msg[len(msg)-1] == '\r') {
		msg = msg[:len(msg)-1]
	}
	line, err := json.Marshal(map[string]any{
		"ts": time.Now().UTC().Format(time.RFC3339Nano), "level": "info", "msg": msg,
	})
	if err != nil {
		return len(p), err
	}
	line = append(line, '\n')
	if _, err := os.Stderr.Write(line); err != nil {
		return len(p), err
	}
	return len(p), nil
}
