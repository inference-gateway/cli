// Package scheduler provides cron-driven background execution of agent
// prompts on behalf of the Schedule tool. Jobs are persisted as YAML files
// under the configured storage directory and hot-reloaded by Service via
// fsnotify.
package scheduler

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	domain "github.com/inference-gateway/cli/internal/domain"
	yaml "gopkg.in/yaml.v3"
)

// ErrNotFound is returned when a job ID does not exist in the store.
var ErrNotFound = errors.New("scheduled job not found")

// Store persists ScheduledJob records as YAML files in a directory.
// One file per job, named "<id>.yaml". Writes are atomic (write to .tmp,
// then rename) so partial writes never expose half-formed YAML to the
// daemon's fsnotify watcher.
type Store struct {
	dir string
}

// NewStore creates the storage directory if it does not exist and returns a
// Store ready to use.
func NewStore(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("storage directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create schedule storage dir %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// Dir returns the storage directory path.
func (s *Store) Dir() string {
	return s.dir
}

// Path returns the YAML path for a given job ID.
func (s *Store) Path(id string) string {
	return filepath.Join(s.dir, id+".yaml")
}

// Save writes the job to disk atomically.
func (s *Store) Save(job *domain.ScheduledJob) error {
	if job == nil {
		return errors.New("job is nil")
	}
	if job.ID == "" {
		return errors.New("job ID is required")
	}
	data, err := yaml.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job %s: %w", job.ID, err)
	}
	final := s.Path(job.ID)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("failed to write temp file %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to rename %s -> %s: %w", tmp, final, err)
	}
	return nil
}

// Load reads a single job by ID. Returns ErrNotFound if the file does not exist.
func (s *Store) Load(id string) (*domain.ScheduledJob, error) {
	if id == "" {
		return nil, errors.New("job ID is required")
	}
	data, err := os.ReadFile(s.Path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to read job %s: %w", id, err)
	}
	job := &domain.ScheduledJob{}
	if err := yaml.Unmarshal(data, job); err != nil {
		return nil, fmt.Errorf("failed to parse job %s: %w", id, err)
	}
	return job, nil
}

// LoadFromPath reads a job from an explicit path. Useful when the fsnotify
// watcher reports a change without a clean ID extraction.
func (s *Store) LoadFromPath(path string) (*domain.ScheduledJob, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to read job file %s: %w", path, err)
	}
	job := &domain.ScheduledJob{}
	if err := yaml.Unmarshal(data, job); err != nil {
		return nil, fmt.Errorf("failed to parse job file %s: %w", path, err)
	}
	return job, nil
}

// List returns all jobs sorted by CreatedAt ascending. Files that fail to
// parse are skipped (the corresponding errors are returned alongside the
// successfully-loaded jobs).
func (s *Store) List() ([]*domain.ScheduledJob, []error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, []error{fmt.Errorf("failed to read schedule dir: %w", err)}
	}
	var jobs []*domain.ScheduledJob
	var errs []error
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") {
			continue
		}
		id := strings.TrimSuffix(name, ".yaml")
		job, err := s.Load(id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
	})
	return jobs, errs
}

// Delete removes a job by ID. Returns ErrNotFound if it did not exist.
func (s *Store) Delete(id string) error {
	if id == "" {
		return errors.New("job ID is required")
	}
	if err := os.Remove(s.Path(id)); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return fmt.Errorf("failed to delete job %s: %w", id, err)
	}
	return nil
}

// IDFromPath returns the job ID derived from a YAML file path. Returns the
// empty string when the path is not a job file.
func IDFromPath(path string) string {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".yaml") {
		return ""
	}
	return strings.TrimSuffix(base, ".yaml")
}
