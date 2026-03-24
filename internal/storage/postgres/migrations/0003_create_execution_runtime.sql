CREATE TABLE IF NOT EXISTS execution_sessions (
    id text PRIMARY KEY,
    action_plan_id text NOT NULL DEFAULT '',
    case_id text NOT NULL DEFAULT '',
    work_item_id text NOT NULL DEFAULT '',
    coordination_decision_id text NOT NULL DEFAULT '',
    policy_decision_id text NOT NULL DEFAULT '',
    execution_constraints_id text NOT NULL DEFAULT '',
    status text NOT NULL,
    current_step_index integer NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    failure_reason text NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_execution_sessions_work_item_id
    ON execution_sessions (work_item_id, created_at, id);

CREATE TABLE IF NOT EXISTS step_executions (
    id text PRIMARY KEY,
    execution_session_id text NOT NULL REFERENCES execution_sessions(id) ON DELETE CASCADE,
    action_id text NOT NULL DEFAULT '',
    step_index integer NOT NULL,
    status text NOT NULL,
    started_at timestamptz,
    finished_at timestamptz,
    failure_reason text NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_step_executions_session_id
    ON step_executions (execution_session_id, step_index, id);

CREATE TABLE IF NOT EXISTS wal_entries (
    seq bigserial PRIMARY KEY,
    id text NOT NULL UNIQUE,
    execution_session_id text NOT NULL REFERENCES execution_sessions(id) ON DELETE CASCADE,
    step_execution_id text REFERENCES step_executions(id) ON DELETE CASCADE,
    action_id text NOT NULL DEFAULT '',
    entry_type text NOT NULL,
    created_at timestamptz NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_wal_entries_session_seq
    ON wal_entries (execution_session_id, seq);

CREATE TABLE IF NOT EXISTS compensation_log (
    seq bigserial PRIMARY KEY,
    wal_entry_id text NOT NULL UNIQUE REFERENCES wal_entries(id) ON DELETE CASCADE,
    execution_session_id text NOT NULL REFERENCES execution_sessions(id) ON DELETE CASCADE,
    step_execution_id text REFERENCES step_executions(id) ON DELETE CASCADE,
    action_id text NOT NULL DEFAULT '',
    entry_type text NOT NULL,
    created_at timestamptz NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_compensation_log_session_seq
    ON compensation_log (execution_session_id, seq);

CREATE OR REPLACE FUNCTION forbid_append_only_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'append-only table % does not allow %', TG_TABLE_NAME, TG_OP;
END;
$$;

DROP TRIGGER IF EXISTS wal_entries_append_only ON wal_entries;
CREATE TRIGGER wal_entries_append_only
BEFORE UPDATE OR DELETE ON wal_entries
FOR EACH ROW
EXECUTE FUNCTION forbid_append_only_mutation();

DROP TRIGGER IF EXISTS compensation_log_append_only ON compensation_log;
CREATE TRIGGER compensation_log_append_only
BEFORE UPDATE OR DELETE ON compensation_log
FOR EACH ROW
EXECUTE FUNCTION forbid_append_only_mutation();
