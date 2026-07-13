package policy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"vaultfleet/pkg/protocol"
)

const (
	DefaultDir         = "/etc/vaultfleet"
	PolicyFileName     = "policy.json"
	PoliciesDirName    = "policies"
	PendingResultsFile = "pending_results.json"
)

type Store struct {
	dir string
}

type PendingTaskResult struct {
	MessageID string                     `json:"message_id,omitempty"`
	Payload   protocol.TaskResultPayload `json:"payload"`
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
	return writeFile0600(s.policyPath(policy.PolicyID), data)
}

func (s *Store) LoadPolicy() (*protocol.PolicyPushPayload, error) {
	policies, err := s.LoadPolicies()
	if err != nil {
		return nil, err
	}
	if len(policies) == 0 {
		return nil, os.ErrNotExist
	}
	return policies[len(policies)-1], nil
}

func (s *Store) LoadPolicyByID(policyID string) (*protocol.PolicyPushPayload, error) {
	data, err := os.ReadFile(s.policyPath(policyID))
	if err != nil {
		return nil, err
	}

	var policy protocol.PolicyPushPayload
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, err
	}
	return &policy, nil
}

func (s *Store) LoadPolicies() ([]*protocol.PolicyPushPayload, error) {
	byID := make(map[string]*protocol.PolicyPushPayload)
	legacy, err := s.loadPolicyFile(s.path(PolicyFileName))
	if err == nil {
		byID[policyKey(legacy)] = legacy
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	entries, err := os.ReadDir(s.path(PoliciesDirName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		stored, loadErr := s.loadPolicyFile(filepath.Join(s.path(PoliciesDirName), entry.Name()))
		if loadErr != nil {
			return nil, loadErr
		}
		byID[policyKey(stored)] = stored
	}

	keys := make([]string, 0, len(byID))
	for key := range byID {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	policies := make([]*protocol.PolicyPushPayload, 0, len(keys))
	for _, key := range keys {
		policies = append(policies, byID[key])
	}
	return policies, nil
}

func (s *Store) DeletePolicy(policyID ...string) error {
	id := ""
	if len(policyID) > 0 {
		id = policyID[0]
	}
	if err := os.Remove(s.policyPath(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) loadPolicyFile(path string) (*protocol.PolicyPushPayload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var stored protocol.PolicyPushPayload
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, err
	}
	return &stored, nil
}

func (s *Store) policyPath(policyID string) string {
	policyID = strings.TrimSpace(policyID)
	if policyID == "" {
		return s.path(PolicyFileName)
	}
	return filepath.Join(s.path(PoliciesDirName), filepath.Base(policyID)+".json")
}

func policyKey(stored *protocol.PolicyPushPayload) string {
	if stored == nil || strings.TrimSpace(stored.PolicyID) == "" {
		return "legacy"
	}
	return stored.PolicyID
}

func (s *Store) SavePendingResults(results []PendingTaskResult) error {
	if err := s.ensureDir(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return writeFile0600(s.path(PendingResultsFile), data)
}

func (s *Store) LoadPendingResults() ([]PendingTaskResult, error) {
	data, err := os.ReadFile(s.path(PendingResultsFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var rawResults []json.RawMessage
	if err := json.Unmarshal(data, &rawResults); err != nil {
		return nil, err
	}

	results := make([]PendingTaskResult, 0, len(rawResults))
	for _, raw := range rawResults {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}

		if _, ok := fields["payload"]; ok {
			var result PendingTaskResult
			if err := json.Unmarshal(raw, &result); err != nil {
				return nil, err
			}
			results = append(results, result)
			continue
		}

		var payload protocol.TaskResultPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, err
		}
		results = append(results, PendingTaskResult{Payload: payload})
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
