package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type entry struct {
	RawPrompt string `json:"raw_prompt"`
	RulesHash string `json:"rules_hash"`
	Refined   string `json:"refined"`
}

// Store is a file-based cache for refined prompts.
type Store struct {
	Dir     string
	Enabled bool
}

func NewStore(dir string, enabled bool) *Store {
	return &Store{Dir: dir, Enabled: enabled}
}

func (s *Store) key(rawPrompt, rulesHash string) string {
	h := sha256.Sum256([]byte(rawPrompt + rulesHash))
	return fmt.Sprintf("%x", h)
}

// Get returns a cached refined prompt if it exists.
func (s *Store) Get(rawPrompt, rulesHash string) (string, bool) {
	if !s.Enabled {
		return "", false
	}
	path := filepath.Join(s.Dir, s.key(rawPrompt, rulesHash)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		return "", false
	}
	return e.Refined, true
}

// Put stores a refined prompt in the cache.
func (s *Store) Put(rawPrompt, rulesHash, refined string) error {
	if !s.Enabled {
		return nil
	}
	if err := os.MkdirAll(s.Dir, 0755); err != nil {
		return err
	}
	e := entry{RawPrompt: rawPrompt, RulesHash: rulesHash, Refined: refined}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.Dir, s.key(rawPrompt, rulesHash)+".json")
	return os.WriteFile(path, data, 0644)
}

// Clear removes all cache entries.
func (s *Store) Clear() error {
	return os.RemoveAll(s.Dir)
}

// Stats returns the number of entries and total size.
func (s *Store) Stats() (entries int, sizeBytes int64, err error) {
	err = filepath.Walk(s.Dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // skip errors
		}
		if !info.IsDir() && filepath.Ext(path) == ".json" {
			entries++
			sizeBytes += info.Size()
		}
		return nil
	})
	return
}
