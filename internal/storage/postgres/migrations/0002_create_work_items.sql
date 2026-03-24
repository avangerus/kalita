CREATE TABLE IF NOT EXISTS work_items (
	id text PRIMARY KEY,
	case_id text NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
	queue_id text NOT NULL,
	type text NOT NULL,
	status text NOT NULL,
	priority text NOT NULL DEFAULT '',
	reason text NOT NULL DEFAULT '',
	assigned_employee_id text,
	plan_id text,
	due_at timestamptz,
	action_plan jsonb NOT NULL DEFAULT 'null'::jsonb,
	created_at timestamptz NOT NULL,
	updated_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_work_items_case_id ON work_items(case_id);
CREATE INDEX IF NOT EXISTS idx_work_items_status ON work_items(status);
