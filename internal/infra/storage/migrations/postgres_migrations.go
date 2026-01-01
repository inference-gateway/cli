package migrations

// GetPostgresMigrations returns all PostgreSQL migrations in order
func GetPostgresMigrations() []Migration {
	return []Migration{
		{
			Version:     "001",
			Description: "Initial schema - conversations and entries tables",
			UpSQL: `
				CREATE TABLE IF NOT EXISTS conversations (
					id VARCHAR(255) PRIMARY KEY,
					title TEXT NOT NULL,
					created_at TIMESTAMP WITH TIME ZONE NOT NULL,
					updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
					message_count INTEGER NOT NULL DEFAULT 0,
					model VARCHAR(255),
					tags JSONB,
					token_stats JSONB,
					cost_stats JSONB,
					title_generated BOOLEAN DEFAULT FALSE,
					title_invalidated BOOLEAN DEFAULT FALSE,
					title_generation_time TIMESTAMP WITH TIME ZONE
				);

				CREATE TABLE IF NOT EXISTS conversation_entries (
					id BIGSERIAL PRIMARY KEY,
					conversation_id VARCHAR(255) NOT NULL,
					entry_data JSONB NOT NULL,
					sequence_number INTEGER NOT NULL,
					created_at TIMESTAMP WITH TIME ZONE NOT NULL,
					FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
				);

				CREATE INDEX IF NOT EXISTS idx_conversations_updated_at ON conversations(updated_at DESC);
				CREATE INDEX IF NOT EXISTS idx_conversations_created_at ON conversations(created_at DESC);
				CREATE INDEX IF NOT EXISTS idx_conversation_entries_conversation_id ON conversation_entries(conversation_id);
				CREATE INDEX IF NOT EXISTS idx_conversation_entries_sequence ON conversation_entries(conversation_id, sequence_number);
				CREATE INDEX IF NOT EXISTS idx_conversations_tags ON conversations USING gin(tags);
				CREATE INDEX IF NOT EXISTS idx_conversations_title_invalidated ON conversations(title_invalidated, title_generated);
			`,
			DownSQL: `
				DROP INDEX IF EXISTS idx_conversations_title_invalidated;
				DROP INDEX IF EXISTS idx_conversations_tags;
				DROP INDEX IF EXISTS idx_conversation_entries_sequence;
				DROP INDEX IF EXISTS idx_conversation_entries_conversation_id;
				DROP INDEX IF EXISTS idx_conversations_created_at;
				DROP INDEX IF EXISTS idx_conversations_updated_at;
				DROP TABLE IF EXISTS conversation_entries;
				DROP TABLE IF EXISTS conversations;
			`,
		},
	}
}
