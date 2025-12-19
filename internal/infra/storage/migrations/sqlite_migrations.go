package migrations

// GetSQLiteMigrations returns all SQLite migrations in order
func GetSQLiteMigrations() []Migration {
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
					optimized_messages TEXT,
					total_input_tokens INTEGER NOT NULL DEFAULT 0,
					total_output_tokens INTEGER NOT NULL DEFAULT 0,
					request_count INTEGER NOT NULL DEFAULT 0,
					cost_stats TEXT DEFAULT '{}',
					models TEXT DEFAULT '[]',
					tags TEXT DEFAULT '[]',
					summary TEXT DEFAULT '',
					title_generated BOOLEAN DEFAULT FALSE,
					title_invalidated BOOLEAN DEFAULT FALSE,
					title_generation_time DATETIME,
					created_at DATETIME NOT NULL,
					updated_at DATETIME NOT NULL
				);

				CREATE INDEX IF NOT EXISTS idx_conversations_updated_at ON conversations(updated_at DESC);
			`,
			DownSQL: `
				DROP INDEX IF EXISTS idx_conversations_updated_at;
				DROP TABLE IF EXISTS conversations;
			`,
		},
	}
}
