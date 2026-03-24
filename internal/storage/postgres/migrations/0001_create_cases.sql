CREATE TABLE IF NOT EXISTS cases (
	id text PRIMARY KEY,
	status text NOT NULL,
	created_at timestamptz NOT NULL,
	updated_at timestamptz NOT NULL,
	metadata jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_cases_status ON cases(status);
