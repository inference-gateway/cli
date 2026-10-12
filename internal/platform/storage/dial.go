package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	redis "github.com/go-redis/redis/v8"
)

// OpenSQLite opens a SQLite database handle with the conversation store's
// connection options. The caller owns the returned handle and applies the
// schema migrations it needs.
func OpenSQLite(path string) (*sql.DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=1000&_timeout=30000&_busy_timeout=30000")
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)
	return db, nil
}

// OpenPostgres opens a verified PostgreSQL handle, pinged before handing it
// back so startup fails fast on an unreachable server. The caller owns it.
func OpenPostgres(pg PostgresConfig) (*sql.DB, error) {
	if err := verifyPostgresAvailable(pg); err != nil {
		return nil, err
	}

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		pg.Host, pg.Port, pg.Username, pg.Password, pg.Database, pg.SSLMode)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open PostgreSQL connection: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping PostgreSQL: %w", err)
	}
	return db, nil
}

// OpenRedis dials and pings a Redis server so startup fails fast on an
// unreachable one. The caller owns the returned client.
func OpenRedis(conn RedisConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", conn.Host, conn.Port),
		DB:       conn.Database,
		Password: conn.Password,
		Username: conn.Username,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis connection failed: %w\n\n"+
			"Redis server not available. Verify:\n"+
			"  - Redis server is running\n"+
			"  - Connection details are correct\n"+
			"  - Network connectivity to %s:%d", err, conn.Host, conn.Port)
	}
	return client, nil
}