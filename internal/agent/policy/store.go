package policy

import (
	"encoding/json"
	"os"
	"path/filepath"

	"vaultfleet/pkg/protocol"
)

const (
	DefaultDir         = "/etc/vaultfleet"
	PolicyFileName     = "policy.json"
	PendingResultsFile = "pending_results.json"
)

type Store struct {
	dir string
}

func NewStore(dir string) *Store {
	if dir == "" {
		dir = DefaultDir
	}
	return &Store{dir: dir}
}

func (s *Store) SavePolicy(policy *protocol.PolicyPushPayload) error {
	if err := s.ensureDir(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return err
	}
	return writeFile0600(s.path(PolicyFileName), data)
}

func (s *Store) LoadPolicy() (*protocol.PolicyPushPayload, error) {
	data, err := os.ReadFile(s.path(PolicyFileName))
	if err != nil {
		return nil, err
	}

	var policy protocol.PolicyPushPayload
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, err
	}
	return &policy, nil
}

func (s *Store) SavePendingResults(results []protocol.TaskResultPayload) error {
	if err := s.ensureDir(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return writeFile0600(s.path(PendingResultsFile), data)
}

func (s *Store) LoadPendingResults() ([]protocol.TaskResultPayload, error) {
	data, err := os.ReadFile(s.path(PendingResultsFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var results []protocol.TaskResultPayload
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *Store) ClearPendingResults() error {
	if err := os.Remove(s.path(PendingResultsFile)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) ensureDir() error {
	return os.MkdirAll(s.dir, 0o700)
}

func (s *Store) path(name string) string {
	return filepath.Join(s.dir, name)
}

func writeFile0600(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
