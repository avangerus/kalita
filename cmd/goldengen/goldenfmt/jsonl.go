package goldenfmt

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/avangerus/kalita/internal/eventstore"
)

// ReadJournal parses a journal.jsonl file: one eventstore.Event per line.
// eventstore.Event's fields are all exported and json-tagged (it is the wire
// format already used for hashing/storage), so this is a plain decode — no
// custom codec is needed on top of it.
func ReadJournal(path string) ([]*eventstore.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []*eventstore.Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) // payloads can be large (embedded pack sources)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(text) == "" {
			continue
		}
		var ev eventstore.Event
		if err := json.Unmarshal([]byte(text), &ev); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		events = append(events, &ev)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return events, nil
}

// WriteJournal writes events as JSONL, one compact JSON object per line, in
// the order given (callers pass seq order). This is the file that gets
// committed as-is (LLD-03 §3): once written it is never reformatted, only
// replayed.
func WriteJournal(path string, events []*eventstore.Event) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, ev := range events {
		if err := enc.Encode(ev); err != nil {
			return err
		}
	}
	return w.Flush()
}
