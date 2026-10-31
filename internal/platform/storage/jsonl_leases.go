package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// leasesFileName holds the session leases of this machine. JSONL conversations
// never leave the machine, so neither do their leases: the file sits in the
// userspace config dir, one for every project.
const leasesFileName = "leases.json"

type leasesFile struct {
	Leases map[string]agentdomain.Lease `json:"leases"`
}

func (s *JsonlStorage) leasesPath() string {
	return filepath.Join(config.UserSpaceConfigDir(), leasesFileName)
}

func (s *JsonlStorage) loadLeasesLocked() (map[string]agentdomain.Lease, error) {
	data, err := os.ReadFile(s.leasesPath())
	if errors.Is(err, os.ErrNotExist) {
		return map[string]agentdomain.Lease{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the session leases: %w", err)
	}
	var file leasesFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decoding the session leases: %w", err)
	}
	if file.Leases == nil {
		file.Leases = map[string]agentdomain.Lease{}
	}
	return file.Leases, nil
}

func (s *JsonlStorage) saveLeasesLocked(leases map[string]agentdomain.Lease) error {
	path := s.leasesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating the session leases directory: %w", err)
	}
	data, err := json.MarshalIndent(leasesFile{Leases: leases}, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the session leases: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), leasesFileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("writing the session leases: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("writing the session leases: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("writing the session leases: %w", err)
	}
	return os.Rename(tmp.Name(), path)
}

func (s *JsonlStorage) GetLease(_ context.Context, conversationID string) (agentdomain.Lease, bool, error) {
	s.leasesMu.Lock()
	defer s.leasesMu.Unlock()
	leases, err := s.loadLeasesLocked()
	if err != nil {
		return agentdomain.Lease{}, false, err
	}
	lease, ok := leases[conversationID]
	return lease, ok, nil
}

func (s *JsonlStorage) ClaimLease(_ context.Context, lease agentdomain.Lease) (agentdomain.Lease, bool, error) {
	s.leasesMu.Lock()
	defer s.leasesMu.Unlock()
	leases, err := s.loadLeasesLocked()
	if err != nil {
		return agentdomain.Lease{}, false, err
	}
	if current, ok := leases[lease.ConversationID]; ok && !current.Yields(lease, time.Now()) {
		return current, false, nil
	}
	leases[lease.ConversationID] = lease
	return lease, true, s.saveLeasesLocked(leases)
}

func (s *JsonlStorage) RenewLease(_ context.Context, conversationID, workspace string, until time.Time) error {
	s.leasesMu.Lock()
	defer s.leasesMu.Unlock()
	leases, err := s.loadLeasesLocked()
	if err != nil {
		return err
	}
	current, ok := leases[conversationID]
	if !ok || current.Workspace != workspace {
		return agentdomain.ErrLeaseLost
	}
	current.Until = until
	leases[conversationID] = current
	return s.saveLeasesLocked(leases)
}

func (s *JsonlStorage) ReleaseLease(_ context.Context, conversationID, workspace string) error {
	s.leasesMu.Lock()
	defer s.leasesMu.Unlock()
	leases, err := s.loadLeasesLocked()
	if err != nil {
		return err
	}
	current, ok := leases[conversationID]
	if !ok || current.Workspace != workspace {
		return nil
	}
	delete(leases, conversationID)
	return s.saveLeasesLocked(leases)
}
