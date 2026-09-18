// Package opcache memoises 1Password CLI secret reads.
//
// `op read` costs ~900ms per invocation. `op --debug` shows that the time is
// neither authentication nor fetching the secret -- the item itself is a ~2ms
// hit in op's own local cache -- but three server requests (GET
// /api/v2/overview, GET /api/v3/account, POST /api/v3/user/itemusage) totalling
// ~720ms. That cost is per invocation, so it cannot be amortised by asking for
// less; it can only be avoided by not invoking op at all.
package opcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotFound is returned by a Store when a key has no entry.
var ErrNotFound = errors.New("not found")

// Store is the backing key/value store. It is an interface so the tests can
// run without touching a real keychain.
type Store interface {
	Get(key string) ([]byte, error)
	Put(key string, value []byte) error
	Delete(key string) error
	Keys() ([]string, error)
}

// record is what gets written to the store. encoding/json base64-encodes a
// []byte field, so secrets containing NUL or other non-UTF-8 bytes survive.
type record struct {
	Version   int    `json:"version"`
	CreatedAt int64  `json:"created_at"`
	Value     []byte `json:"value"`
}

const recordVersion = 1

// Cache reads secrets through a Store, falling back to Fetch on a miss.
type Cache struct {
	Store Store
	Fetch func(ref string) ([]byte, error)
	Now   func() time.Time
}

func (c *Cache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Read returns the secret for ref, from the store when a usable entry exists
// and from Fetch otherwise. A successful fetch is written back to the store;
// a failure to write is reported through warn but does not fail the read,
// because the caller already has the secret it asked for.
func (c *Cache) Read(ref, key string, ttl time.Duration, refresh bool, warn func(error)) ([]byte, error) {
	if err := ValidateRef(ref); err != nil {
		return nil, err
	}

	if !refresh {
		if value, ok := c.lookup(key, ttl); ok {
			return value, nil
		}
	}

	value, err := c.Fetch(ref)
	if err != nil {
		return nil, err
	}
	if len(value) == 0 {
		return nil, fmt.Errorf("op returned an empty value for %s", ref)
	}

	blob, err := json.Marshal(record{Version: recordVersion, CreatedAt: c.now().Unix(), Value: value})
	if err != nil {
		warn(fmt.Errorf("encoding cache entry: %w", err))
		return value, nil
	}
	if err := c.Store.Put(key, blob); err != nil {
		warn(fmt.Errorf("writing cache entry: %w", err))
	}
	return value, nil
}

// lookup reports whether the store holds a valid, unexpired entry. Any
// malformed or expired record is treated as a miss rather than an error: the
// caller can always recover by fetching, and a corrupt entry must never be
// returned as if it were the secret.
func (c *Cache) lookup(key string, ttl time.Duration) ([]byte, bool) {
	blob, err := c.Store.Get(key)
	if err != nil {
		return nil, false
	}

	var rec record
	if err := json.Unmarshal(blob, &rec); err != nil {
		return nil, false
	}
	if rec.Version != recordVersion || len(rec.Value) == 0 {
		return nil, false
	}

	// A negative age means the entry is stamped in the future -- clock skew, or
	// a tampered record. Without this check such an entry would satisfy any
	// TTL, including zero.
	age := c.now().Sub(time.Unix(rec.CreatedAt, 0))
	if age < 0 || age >= ttl {
		return nil, false
	}
	return rec.Value, true
}

// Purge deletes every entry this tool created, reporting how many went and
// keeping the first failure.
func (c *Cache) Purge() (int, error) {
	keys, err := c.Store.Keys()
	if err != nil {
		return 0, err
	}
	var n int
	var firstErr error
	for _, key := range keys {
		if err := c.Store.Delete(key); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("deleting %s: %w", key, err)
			}
			continue
		}
		n++
	}
	return n, firstErr
}

// ValidateRef rejects references that must not be cached.
func ValidateRef(ref string) error {
	if !strings.HasPrefix(ref, "op://") {
		return fmt.Errorf("not a secret reference: %s", ref)
	}
	// A one-time password is regenerated every 30 seconds, so a cached one is
	// already dead by the time it is read back.
	if strings.Contains(ref, "attribute=otp") {
		return fmt.Errorf("refusing to cache a one-time password: %s", ref)
	}
	return nil
}
