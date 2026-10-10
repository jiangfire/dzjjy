// Package secretstore keeps platform-managed secret values out of the state
// file, the desired config, and every API response.
//
// Values are supplied out of band (environment variable or an external
// provider) and only the key name ever appears in a dzjjy/v1 manifest. The
// store writes an AES-GCM encrypted file next to the state file so restarts
// can re-resolve a secret without it ever being persisted in clear text.
package secretstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Store resolves secret keys to values loaded from the process environment.
// The environment is read once at construction so a manifest can only ever
// reference keys that were explicitly provided by the operator.
type Store struct {
	mu     sync.RWMutex
	values map[string]string
}

// NewFromEnv builds a store from the supplied environment variables. Keys are
// used verbatim; callers decide the prefix policy.
func NewFromEnv(lookup func(string) (string, bool)) *Store {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		name, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if resolved, ok := lookup(name); ok {
			values[resolved] = value
		}
	}
	return &Store{values: values}
}

// New builds a store from an explicit key/value map. It is primarily used by
// tests and by hosts that inject secrets programmatically.
func New(values map[string]string) *Store {
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return &Store{values: copied}
}

// Resolve returns the secret value for a key.
func (s *Store) Resolve(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.values[key]
	return value, ok
}

// Keys lists the configured secret keys in a stable order.
func (s *Store) Keys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.values))
	for key := range s.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Has reports whether a key is configured.
func (s *Store) Has(key string) bool {
	_, ok := s.Resolve(key)
	return ok
}

// Save writes an encrypted snapshot of the store to path. The encryption key
// is derived from the supplied passphrase, which the operator supplies
// separately from the data.
func (s *Store) Save(path, passphrase string) error {
	s.mu.RLock()
	payload, err := json.Marshal(s.values)
	s.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("failed to encode secrets: %w", err)
	}
	nonce, err := randomBytes(12)
	if err != nil {
		return err
	}
	gcm, err := newCipher(passphrase)
	if err != nil {
		return err
	}
	sealed := gcm.Seal(nil, nonce, payload, nil)
	record := struct {
		Nonce  string `json:"nonce"`
		Cipher string `json:"cipher"`
	}{
		Nonce:  base64.StdEncoding.EncodeToString(nonce),
		Cipher: base64.StdEncoding.EncodeToString(sealed),
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode secret record: %w", err)
	}
	return writeFileAtomic(path, encoded, 0600)
}

// Load reads an encrypted snapshot written by Save.
func Load(path, passphrase string) (*Store, error) {
	raw, err := os.ReadFile(path) // #nosec G304 - path is operator-configured, not request-derived
	if err != nil {
		return nil, fmt.Errorf("failed to read secret store: %w", err)
	}
	var record struct {
		Nonce  string `json:"nonce"`
		Cipher string `json:"cipher"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, fmt.Errorf("failed to decode secret record: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(record.Nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to decode secret nonce: %w", err)
	}
	sealed, err := base64.StdEncoding.DecodeString(record.Cipher)
	if err != nil {
		return nil, fmt.Errorf("failed to decode secret payload: %w", err)
	}
	gcm, err := newCipher(passphrase)
	if err != nil {
		return nil, err
	}
	payload, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt secret store: %w", err)
	}
	values := make(map[string]string)
	if err := json.Unmarshal(payload, &values); err != nil {
		return nil, fmt.Errorf("failed to decode secret values: %w", err)
	}
	return &Store{values: values}, nil
}

func newCipher(passphrase string) (cipher.AEAD, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("secret passphrase is required")
	}
	key := sha256.Sum256([]byte(passphrase))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("failed to initialise cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to initialise GCM: %w", err)
	}
	return gcm, nil
}

func randomBytes(n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}
	return buf, nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create secret directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".secrets-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary secret file: %w", err)
	}
	tempName := temp.Name()
	defer func() { _ = os.Remove(tempName) }()
	if err := temp.Chmod(perm); err != nil {
		_ = temp.Close()
		return fmt.Errorf("failed to set secret file permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("failed to write secret file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("failed to close secret file: %w", err)
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("failed to activate secret file: %w", err)
	}
	return nil
}
