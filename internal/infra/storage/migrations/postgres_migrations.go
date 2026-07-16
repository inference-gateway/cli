package migrations

// GetPostgresMigrations returns all PostgreSQL migrations in order.
//
// The schema mirrors GetSQLiteMigrations one-for-one (single conversations
// table with messages stored as an embedded JSON blob) so both dialects share
// the same SQL core (see issue #839); only the column types differ (TIMESTAMP
// WITH TIME ZONE for datetimes). JSON columns are TEXT because the application
// marshals/unmarshals JSON itself — switch to JSONB only if server-side JSON
// querying is ever needed.
func GetPostgresMigrations() []Migration {
	return []Migration{
		{
			Version:     "001",
			Description: "Initial schema - conversations table",
			UpSQL: `
				CREATE TABLE IF NOT EXISTS conversations (
					id TEXT PRIMARY KEY,
					title TEXT NOT NULL,
					count INTEGER NOT NULL DEFAULT 0,
					messages TEXT NOT NULL,
					total_input_tokens INTEGER NOT NULL DEFAULT 0,
					total_output_tokens INTEGER NOT NULL DEFAULT 0,
					request_count INTEGER NOT NULL DEFAULT 0,
					cost_stats TEXT DEFAULT '{}',
					models TEXT DEFAULT '[]',
					tags TEXT DEFAULT '[]',
					title_generated BOOLEAN DEFAULT FALSE,
					title_invalidated BOOLEAN DEFAULT FALSE,
					title_generation_time TIMESTAMP WITH TIME ZONE,
					created_at TIMESTAMP WITH TIME ZONE NOT NULL,
					updated_at TIMESTAMP WITH TIME ZONE NOT NULL
				);

				CREATE INDEX IF NOT EXISTS idx_conversations_updated_at ON conversations(updated_at DESC);
			`,
			DownSQL: `
				DROP INDEX IF EXISTS idx_conversations_updated_at;
				DROP TABLE IF EXISTS conversations;
			`,
		},
		{
			Version:     "002",
			Description: "Session groups index for channel-keyed rollover",
			UpSQL: `
				CREATE TABLE IF NOT EXISTS session_groups (
					group_key          TEXT PRIMARY KEY,
					current_session_id TEXT NOT NULL,
					history            TEXT NOT NULL DEFAULT '[]',
					last_rollover      TIMESTAMP WITH TIME ZONE,
					updated_at         TIMESTAMP WITH TIME ZONE NOT NULL
				);
			`,
			DownSQL: `
				DROP TABLE IF EXISTS session_groups;
			`,
		},
		{
			Version:     "003",
			Description: "Scheduled jobs table",
			UpSQL: `
				CREATE TABLE IF NOT EXISTS scheduled_jobs (
					id              TEXT PRIMARY KEY,
					name            TEXT NOT NULL DEFAULT '',
					description     TEXT NOT NULL DEFAULT '',
					cron_expression TEXT NOT NULL,
					prompt          TEXT NOT NULL,
					channel         TEXT NOT NULL DEFAULT '',
					recipient_id    TEXT NOT NULL DEFAULT '',
					model           TEXT NOT NULL DEFAULT '',
					run_once        BOOLEAN NOT NULL DEFAULT FALSE,
					created_at      TIMESTAMP WITH TIME ZONE NOT NULL,
					updated_at      TIMESTAMP WITH TIME ZONE NOT NULL,
					last_run        TIMESTAMP WITH TIME ZONE,
					last_error      TEXT NOT NULL DEFAULT ''
				);
			`,
			DownSQL: `
				DROP TABLE IF EXISTS scheduled_jobs;
			`,
		},
		{
			Version:     "004",
			Description: "Plans table for plan-mode storage",
			UpSQL: `
				CREATE TABLE IF NOT EXISTS plans (
					id         TEXT PRIMARY KEY,
					title      TEXT NOT NULL,
					body       TEXT NOT NULL,
					created_at TIMESTAMP WITH TIME ZONE NOT NULL
				);
			`,
			DownSQL: `
				DROP TABLE IF EXISTS plans;
			`,
		},
		{
			Version:     "005",
			Description: "Shell history table",
			UpSQL: `
				CREATE TABLE IF NOT EXISTS shell_history (
					id        SERIAL PRIMARY KEY,
					command   TEXT NOT NULL,
					created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
				);
			`,
			DownSQL: `
				DROP TABLE IF EXISTS shell_history;
			`,
		},
	}
}
