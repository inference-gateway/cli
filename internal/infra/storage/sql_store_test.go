package storage

import (
	"testing"

	assert "github.com/stretchr/testify/assert"
)

func TestSQLStoreRebind(t *testing.T) {
	const stmt = "INSERT INTO t (a, b) VALUES (?, ?) ON CONFLICT(a) DO UPDATE SET b = ?"

	t.Run("postgres numbers placeholders left-to-right", func(t *testing.T) {
		s := &sqlStore{dialect: "postgres"}
		assert.Equal(t,
			"INSERT INTO t (a, b) VALUES ($1, $2) ON CONFLICT(a) DO UPDATE SET b = $3",
			s.rebind(stmt))
	})

	t.Run("sqlite leaves placeholders untouched", func(t *testing.T) {
		s := &sqlStore{dialect: "sqlite"}
		assert.Equal(t, stmt, s.rebind(stmt))
	})

	t.Run("no placeholders is a no-op for both dialects", func(t *testing.T) {
		const q = "SELECT 1"
		assert.Equal(t, q, (&sqlStore{dialect: "postgres"}).rebind(q))
		assert.Equal(t, q, (&sqlStore{dialect: "sqlite"}).rebind(q))
	})
}
