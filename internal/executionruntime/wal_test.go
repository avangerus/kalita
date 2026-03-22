package executionruntime

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryWALAppendListBySessionPreservesOrdering(t *testing.T) {
	t.Parallel()
	wal := NewInMemoryWAL()
	records := []WALRecord{{ID: "wal-1", ExecutionSessionID: "session-1", Type: WALStepIntent, CreatedAt: time.Date(2026, 3, 22, 16, 0, 0, 0, time.UTC)}, {ID: "wal-2", ExecutionSessionID: "session-2", Type: WALStepIntent, CreatedAt: time.Date(2026, 3, 22, 16, 0, 1, 0, time.UTC)}, {ID: "wal-3", ExecutionSessionID: "session-1", Type: WALStepResult, CreatedAt: time.Date(2026, 3, 22, 16, 0, 2, 0, time.UTC)}}
	for _, record := range records {
		if err := wal.Append(context.Background(), record); err != nil {
			t.Fatalf("Append error = %v", err)
		}
	}
	got, err := wal.ListBySession(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("ListBySession error = %v", err)
	}
	if len(got) != 2 || got[0].ID != "wal-1" || got[1].ID != "wal-3" {
		t.Fatalf("records = %#v", got)
	}
}
