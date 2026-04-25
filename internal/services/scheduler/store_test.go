package scheduler

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
)

func newJob(id string, created time.Time) *domain.ScheduledJob {
	return &domain.ScheduledJob{
		ID:             id,
		Name:           "job-" + id,
		CronExpression: "*/5 * * * *",
		Prompt:         "do the thing",
		Channel:        "telegram",
		RecipientID:    "12345",
		CreatedAt:      created,
		UpdatedAt:      created,
	}
}

func TestNewStore_RequiresDir(t *testing.T) {
	if _, err := NewStore(""); err == nil {
		t.Fatal("expected error for empty dir")
	}
}

func TestStore_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	job := newJob("abc-123", time.Now().UTC().Truncate(time.Second))
	if err := s.Save(job); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := s.Load(job.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ID != job.ID || loaded.Channel != job.Channel || !loaded.CreatedAt.Equal(job.CreatedAt) {
		t.Fatalf("round-trip mismatch: got %+v want %+v", loaded, job)
	}
}

func TestStore_Save_NilOrMissingID(t *testing.T) {
	s, _ := NewStore(t.TempDir())
	if err := s.Save(nil); err == nil {
		t.Fatal("expected error for nil job")
	}
	if err := s.Save(&domain.ScheduledJob{}); err == nil {
		t.Fatal("expected error for empty ID")
	}
}

func TestStore_Load_NotFound(t *testing.T) {
	s, _ := NewStore(t.TempDir())
	_, err := s.Load("missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStore_List_SortedByCreatedAt(t *testing.T) {
	s, _ := NewStore(t.TempDir())
	now := time.Now().UTC().Truncate(time.Second)
	a := newJob("a", now.Add(2*time.Minute))
	b := newJob("b", now.Add(1*time.Minute))
	c := newJob("c", now)
	for _, j := range []*domain.ScheduledJob{a, b, c} {
		if err := s.Save(j); err != nil {
			t.Fatalf("Save %s: %v", j.ID, err)
		}
	}
	got, errs := s.List()
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 jobs, got %d", len(got))
	}
	if got[0].ID != "c" || got[1].ID != "b" || got[2].ID != "a" {
		t.Fatalf("expected order c,b,a; got %s,%s,%s", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestStore_List_SkipsNonYamlFiles(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	if err := s.Save(newJob("ok", time.Now())); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("ignore me"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	got, errs := s.List()
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1, got %d", len(got))
	}
}

func TestStore_Delete(t *testing.T) {
	s, _ := NewStore(t.TempDir())
	if err := s.Save(newJob("x", time.Now())); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Delete("x"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.Delete("x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on second delete, got %v", err)
	}
}

func TestStore_AtomicWrite_NoTmpResidueOnSuccess(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	if err := s.Save(newJob("atomic", time.Now())); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("temp file leaked: %s", e.Name())
		}
	}
}

func TestIDFromPath(t *testing.T) {
	cases := map[string]string{
		"/foo/bar/abc-123.yaml": "abc-123",
		"plain.yaml":            "plain",
		"not-yaml.txt":          "",
	}
	for in, want := range cases {
		if got := IDFromPath(in); got != want {
			t.Errorf("IDFromPath(%q) = %q, want %q", in, got, want)
		}
	}
}
