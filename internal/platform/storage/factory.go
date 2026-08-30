package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	config "github.com/inference-gateway/cli/config"
	project "github.com/inference-gateway/cli/internal/platform/project"
)

// NewStorageFromConfig creates a storage configuration from app config
func NewStorageFromConfig(cfg *config.Config) StorageConfig {
	if !cfg.Storage.Enabled {
		return StorageConfig{Type: config.StorageTypeMemory}
	}

	switch cfg.Storage.Type {
	case config.StorageTypeSQLite:
		return StorageConfig{
			Type: config.StorageTypeSQLite,
			SQLite: SQLiteConfig{
				// Shared machine-global database; grouping by project
				// happens via the metadata project field, not per-project files.
				Path: orDefault(cfg.Storage.SQLite.Path, defaultSQLitePath()),
			},
		}
	case config.StorageTypePostgres:
		return StorageConfig{
			Type: config.StorageTypePostgres,
			Postgres: PostgresConfig{
				Host:     cfg.Storage.Postgres.Host,
				Port:     cfg.Storage.Postgres.Port,
				Database: cfg.Storage.Postgres.Database,
				Username: cfg.Storage.Postgres.Username,
				Password: cfg.Storage.Postgres.Password,
				SSLMode:  cfg.Storage.Postgres.SSLMode,
			},
		}
	case config.StorageTypeRedis:
		return StorageConfig{
			Type: config.StorageTypeRedis,
			Redis: RedisConfig{
				Host:     cfg.Storage.Redis.Host,
				Port:     cfg.Storage.Redis.Port,
				Password: cfg.Storage.Redis.Password,
				Database: cfg.Storage.Redis.DB,
			},
		}
	case config.StorageTypeD1:
		return StorageConfig{
			Type: config.StorageTypeD1,
			D1: D1Config{
				AccountID:  cfg.Storage.D1.AccountID,
				DatabaseID: cfg.Storage.D1.DatabaseID,
				APIToken:   cfg.Storage.D1.APIToken,
				BaseURL:    cfg.Storage.D1.BaseURL,
			},
		}
	case config.StorageTypeJsonl:
		jsonl := JsonlStorageConfig{PlansPath: userPlansDir()}
		if path := cfg.Storage.Jsonl.Path; path != "" {
			// Explicit user-set path wins as-is and only ever lists itself.
			jsonl.Path = path
		} else {
			jsonl.Path = defaultConversationsDir()
			jsonl.ProjectsPath = defaultProjectsDir()
		}
		return StorageConfig{
			Type:  config.StorageTypeJsonl,
			Jsonl: jsonl,
		}
	default:
		return StorageConfig{Type: config.StorageTypeMemory}
	}
}

// homeConfigDir returns ~/.infer, or the bare project-relative .infer when no
// home directory can be resolved (paths then land next to the working dir).
func homeConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return config.ConfigDirName
	}
	return filepath.Join(home, config.ConfigDirName)
}

// orDefault returns the explicit path when set, else the derived default:
// an empty storage.*.path always means "use the default location".
func orDefault(explicit, fallback string) string {
	if explicit != "" {
		return explicit
	}
	return fallback
}

// defaultProjectsDir is the root of the per-project conversation layout.
func defaultProjectsDir() string {
	return filepath.Join(homeConfigDir(), "projects")
}

// projectSlug maps the absolute working directory to a flat directory name,
// the scheme Claude Code uses: /home/alice/repo -> -home-alice-repo. Headless
// and scheduler runs share the cwd of interactive runs, so they resolve the
// same slug.
func projectSlug() string {
	slug := strings.ReplaceAll(project.Path(), string(filepath.Separator), "-")
	if strings.TrimSpace(slug) == "" {
		return "default"
	}
	return slug
}

// defaultConversationsDir is where the current project's conversations go when
// storage.jsonl.path is unset: ~/.infer/projects/<project-slug>/conversations.
func defaultConversationsDir() string {
	return filepath.Join(defaultProjectsDir(), projectSlug(), "conversations")
}

// defaultSQLitePath is the shared machine-global SQLite database:
// ~/.infer/conversations.db.
func defaultSQLitePath() string {
	return filepath.Join(homeConfigDir(), "conversations.db")
}

// userPlansDir returns the userspace plans directory (~/.infer/plans).
func userPlansDir() string {
	return filepath.Join(homeConfigDir(), "plans")
}

// fullBackend is the set of storage interfaces every backend implements; it
// lets NewStorage build the Stores aggregate from a single value.
type fullBackend interface {
	ConversationStorage
	SessionGroupStorage
	ScheduledJobStorage
	ScheduledRunStorage
	PlanStorage
	ShellHistoryStorage
}

// NewStorage creates a new storage instance based on the provided configuration
func NewStorage(config StorageConfig) (*Stores, error) {
	backend, err := newBackend(config)
	if err != nil {
		return nil, err
	}
	return &Stores{
		Conversations: backend,
		SessionGroups: backend,
		ScheduledJobs: backend,
		ScheduledRuns: backend,
		Plans:         backend,
		ShellHistory:  backend,
	}, nil
}

// newBackend constructs the configured backend.
func newBackend(cfg StorageConfig) (fullBackend, error) {
	switch cfg.Type {
	case config.StorageTypeSQLite:
		return NewSQLiteStorage(cfg.SQLite)
	case config.StorageTypePostgres:
		return NewPostgresStorage(cfg.Postgres)
	case config.StorageTypeRedis:
		return NewRedisStorage(cfg.Redis)
	case config.StorageTypeD1:
		return NewD1Storage(cfg.D1)
	case config.StorageTypeJsonl:
		return NewJsonlStorage(cfg.Jsonl)
	case config.StorageTypeMemory:
		return NewMemoryStorage(), nil
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", cfg.Type)
	}
}
