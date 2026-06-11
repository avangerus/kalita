package eventstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore is the PostgreSQL journal. Semantics are defined by MemStore and the
// shared test suite: gapless seq, hash chain, idempotency, no anonymous actors.
//
// Tamper protection is enforced by the database itself: a trigger rejects any
// UPDATE or DELETE on the events table regardless of the connecting role.
//
// Note: payload is stored as TEXT, not JSONB. JSONB normalizes key order and
// whitespace, which would change the bytes and silently break the hash chain
// on read-back. Byte fidelity beats queryability here; journal queries go by
// subject/kind/actor (JSONB columns reconstructed from structs on read).
type PGStore struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// appendLockKey serializes appends on a single node (advisory xact lock).
const appendLockKey = 0x4B414C4954410001 // "KALITA" + 01

const schemaSQL = `
CREATE TABLE IF NOT EXISTS events (
    seq             BIGINT PRIMARY KEY,
    event_id        UUID NOT NULL UNIQUE,
    ts              TIMESTAMPTZ NOT NULL,
    actor           JSONB NOT NULL,
    kind            TEXT NOT NULL,
    subject         JSONB NOT NULL,
    payload         TEXT,
    basis           JSONB,
    def_version     BIGINT NOT NULL,
    idempotency_key TEXT UNIQUE,
    prev_hash       BYTEA NOT NULL,
    hash            BYTEA NOT NULL,
    signature       BYTEA
);
CREATE INDEX IF NOT EXISTS events_kind_ts ON events (kind, ts);
CREATE INDEX IF NOT EXISTS events_subject ON events USING GIN (subject);
CREATE INDEX IF NOT EXISTS events_actor_id ON events ((actor->>'id'), ts);

CREATE OR REPLACE FUNCTION events_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'events are append-only';
END $$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS events_no_mutation ON events;
CREATE TRIGGER events_no_mutation
    BEFORE UPDATE OR DELETE ON events
    FOR EACH ROW EXECUTE FUNCTION events_immutable();
`

// NewPGStore connects, optionally pins a schema (search_path), and ensures the
// journal schema exists. schema = "" means the connection default (public).
func NewPGStore(ctx context.Context, dsn, schema string, nowFn func() time.Time) (*PGStore, error) {
	if nowFn == nil {
		nowFn = time.Now
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	if schema != "" {
		cfg.ConnConfig.RuntimeParams["search_path"] = schema
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if schema != "" {
		if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %q`, schema)); err != nil {
			pool.Close()
			return nil, fmt.Errorf("create schema: %w", err)
		}
	}
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ensure schema: %w", err)
	}
	return &PGStore{pool: pool, now: nowFn}, nil
}

func (s *PGStore) Close() { s.pool.Close() }

// Append adds an event under an advisory lock that serializes writers.
func (s *PGStore) Append(ctx context.Context, in AppendInput) (*Event, error) {
	if in.Actor.ID == "" {
		return nil, ErrEmptyActorID
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, appendLockKey); err != nil {
		return nil, err
	}

	if in.IdempotencyKey != "" {
		prior, err := s.queryOne(ctx, tx, `SELECT `+columns+` FROM events WHERE idempotency_key = $1`, in.IdempotencyKey)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if prior != nil {
			return prior, nil
		}
	}

	var lastSeq uint64
	prev := genesisHash
	var lastHash []byte
	err = tx.QueryRow(ctx, `SELECT seq, hash FROM events ORDER BY seq DESC LIMIT 1`).Scan(&lastSeq, &lastHash)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// genesis
	case err != nil:
		return nil, err
	default:
		prev = lastHash
	}

	e, err := buildEvent(in, lastSeq+1, prev, s.now)
	if err != nil {
		return nil, err
	}

	actorJSON, _ := json.Marshal(e.Actor)
	subjectJSON, _ := json.Marshal(e.Subject)
	var basisJSON []byte
	if e.Basis != nil {
		basisJSON, _ = json.Marshal(e.Basis)
	}
	var payload *string
	if len(e.Payload) > 0 {
		p := string(e.Payload)
		payload = &p
	}
	var idem *string
	if e.IdempotencyKey != "" {
		idem = &e.IdempotencyKey
	}

	_, err = tx.Exec(ctx, `
        INSERT INTO events (seq, event_id, ts, actor, kind, subject, payload, basis,
                            def_version, idempotency_key, prev_hash, hash, signature)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		e.Seq, e.EventID, e.TS, actorJSON, e.Kind, subjectJSON, payload, basisJSON,
		e.DefVersion, idem, e.PrevHash, e.Hash, e.Signature)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return e, nil
}

const columns = `seq, event_id, ts, actor, kind, subject, payload, basis,
                 def_version, idempotency_key, prev_hash, hash, signature`

type rowScanner interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s *PGStore) queryOne(ctx context.Context, q rowScanner, sql string, args ...any) (*Event, error) {
	e, err := scanEvent(q.QueryRow(ctx, sql, args...))
	if err != nil {
		return nil, err
	}
	return e, nil
}

func scanEvent(row pgx.Row) (*Event, error) {
	var (
		e           Event
		actorJSON   []byte
		subjectJSON []byte
		basisJSON   []byte
		payload     *string
		idem        *string
	)
	err := row.Scan(&e.Seq, &e.EventID, &e.TS, &actorJSON, &e.Kind, &subjectJSON,
		&payload, &basisJSON, &e.DefVersion, &idem, &e.PrevHash, &e.Hash, &e.Signature)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(actorJSON, &e.Actor); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(subjectJSON, &e.Subject); err != nil {
		return nil, err
	}
	if len(basisJSON) > 0 {
		e.Basis = &Basis{}
		if err := json.Unmarshal(basisJSON, e.Basis); err != nil {
			return nil, err
		}
	}
	if payload != nil {
		e.Payload = json.RawMessage(*payload)
	}
	if idem != nil {
		e.IdempotencyKey = *idem
	}
	e.TS = e.TS.UTC()
	return &e, nil
}

// All returns the full journal ordered by seq.
func (s *PGStore) All(ctx context.Context) ([]*Event, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+columns+` FROM events ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Verify re-reads the whole journal and checks the hash chain. This is the
// round-trip integrity guarantee: what PostgreSQL stored hashes identically
// to what was appended.
func (s *PGStore) Verify(ctx context.Context) error {
	events, err := s.All(ctx)
	if err != nil {
		return err
	}
	return VerifyChain(events)
}
