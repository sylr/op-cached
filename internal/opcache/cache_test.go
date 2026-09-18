package opcache

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type fakeStore struct {
	data      map[string][]byte
	putErr    error
	deleteErr error
	keysErr   error
}

func newFakeStore() *fakeStore { return &fakeStore{data: map[string][]byte{}} }

func (f *fakeStore) Get(key string) ([]byte, error) {
	v, ok := f.data[key]
	if !ok {
		return nil, ErrNotFound
	}
	return v, nil
}

func (f *fakeStore) Put(key string, value []byte) error {
	if f.putErr != nil {
		return f.putErr
	}
	f.data[key] = value
	return nil
}

func (f *fakeStore) Delete(key string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.data, key)
	return nil
}

func (f *fakeStore) Keys() ([]string, error) {
	if f.keysErr != nil {
		return nil, f.keysErr
	}
	keys := make([]string, 0, len(f.data))
	for k := range f.data {
		keys = append(keys, k)
	}
	return keys, nil
}

const ref = "op://Infra/Test/password"

func newCache(store Store, fetch func(string) ([]byte, error), now time.Time) *Cache {
	return &Cache{Store: store, Fetch: fetch, Now: func() time.Time { return now }}
}

func noWarn(error) {}

func fetches(value []byte, err error, calls *int) func(string) ([]byte, error) {
	return func(string) ([]byte, error) {
		*calls++
		return value, err
	}
}

// A failing op must not be cached, and must not be reported as a success. The
// shell implementation this replaces returned partial stdout with exit 0.
func TestReadFetchFailureIsNotCached(t *testing.T) {
	store := newFakeStore()
	calls := 0
	c := newCache(store, fetches([]byte("partial"), errors.New("boom"), &calls), time.Unix(1000, 0))

	value, err := c.Read(ref, "k", time.Hour, false, noWarn)
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if value != nil {
		t.Errorf("want no value on failure, got %q", value)
	}
	if len(store.data) != 0 {
		t.Errorf("want nothing cached, got %v", store.data)
	}
}

func TestReadEmptyValueIsRejected(t *testing.T) {
	store := newFakeStore()
	calls := 0
	c := newCache(store, fetches([]byte{}, nil, &calls), time.Unix(1000, 0))

	if _, err := c.Read(ref, "k", time.Hour, false, noWarn); err == nil {
		t.Fatal("want an error for an empty value, got nil")
	}
	if len(store.data) != 0 {
		t.Errorf("want nothing cached, got %v", store.data)
	}
}

// Byte fidelity matters: op-cached substitutes for `op read` in shell
// constructs, so the trailing newline and any non-UTF-8 bytes must survive.
func TestReadPreservesExactBytes(t *testing.T) {
	for name, secret := range map[string][]byte{
		"trailing newline": []byte("hunter2\n"),
		"several newlines": []byte("a\n\n\n"),
		"trailing x":       []byte("secretx"),
		"nul bytes":        {'a', 0, 'b', '\n'},
		"invalid utf-8":    {0xff, 0xfe, '\n'},
	} {
		t.Run(name, func(t *testing.T) {
			store := newFakeStore()
			calls := 0
			c := newCache(store, fetches(secret, nil, &calls), time.Unix(1000, 0))

			cold, err := c.Read(ref, "k", time.Hour, false, noWarn)
			if err != nil {
				t.Fatalf("cold read: %v", err)
			}
			if !bytes.Equal(cold, secret) {
				t.Errorf("cold: want %q, got %q", secret, cold)
			}

			warm, err := c.Read(ref, "k", time.Hour, false, noWarn)
			if err != nil {
				t.Fatalf("warm read: %v", err)
			}
			if !bytes.Equal(warm, secret) {
				t.Errorf("warm: want %q, got %q", secret, warm)
			}
			if calls != 1 {
				t.Errorf("want the warm read served from cache, got %d fetches", calls)
			}
		})
	}
}

func TestReadRefreshBypassesCache(t *testing.T) {
	store := newFakeStore()
	calls := 0
	c := newCache(store, fetches([]byte("v\n"), nil, &calls), time.Unix(1000, 0))

	if _, err := c.Read(ref, "k", time.Hour, false, noWarn); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(ref, "k", time.Hour, true, noWarn); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("want --refresh to re-fetch, got %d fetches", calls)
	}
}

// Anything unreadable must degrade to a fetch, never to a wrong answer.
func TestReadMalformedRecordsFallBackToFetch(t *testing.T) {
	now := time.Unix(10000, 0)
	valid := func(t *testing.T, r record) []byte {
		t.Helper()
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	for name, blob := range map[string]func(*testing.T) []byte{
		"not json":       func(*testing.T) []byte { return []byte("{{{") },
		"empty":          func(*testing.T) []byte { return nil },
		"truncated json": func(*testing.T) []byte { return []byte(`{"version":1,`) },
		"wrong version": func(t *testing.T) []byte {
			return valid(t, record{Version: 99, CreatedAt: now.Unix(), Value: []byte("x")})
		},
		"empty value": func(t *testing.T) []byte {
			return valid(t, record{Version: recordVersion, CreatedAt: now.Unix(), Value: nil})
		},
		"stamped in the future": func(t *testing.T) []byte {
			return valid(t, record{Version: recordVersion, CreatedAt: now.Add(time.Hour).Unix(), Value: []byte("stale")})
		},
		"expired": func(t *testing.T) []byte {
			return valid(t, record{Version: recordVersion, CreatedAt: now.Add(-2 * time.Hour).Unix(), Value: []byte("stale")})
		},
	} {
		t.Run(name, func(t *testing.T) {
			store := newFakeStore()
			store.data["k"] = blob(t)
			calls := 0
			c := newCache(store, fetches([]byte("fresh\n"), nil, &calls), now)

			value, err := c.Read(ref, "k", time.Hour, false, noWarn)
			if err != nil {
				t.Fatalf("want a fallback fetch, got error %v", err)
			}
			if string(value) != "fresh\n" {
				t.Errorf("want the freshly fetched value, got %q", value)
			}
			if calls != 1 {
				t.Errorf("want exactly one fetch, got %d", calls)
			}
		})
	}
}

// A future timestamp yields a negative age, which must not satisfy any TTL.
func TestReadFutureTimestampDoesNotSatisfyZeroTTL(t *testing.T) {
	now := time.Unix(10000, 0)
	store := newFakeStore()
	blob, err := json.Marshal(record{Version: recordVersion, CreatedAt: now.Add(time.Hour).Unix(), Value: []byte("stale")})
	if err != nil {
		t.Fatal(err)
	}
	store.data["k"] = blob

	calls := 0
	c := newCache(store, fetches([]byte("fresh\n"), nil, &calls), now)
	value, err := c.Read(ref, "k", 0, false, noWarn)
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "fresh\n" {
		t.Errorf("want a fresh fetch, got %q", value)
	}
}

// Losing the secret because the cache could not be written would be worse than
// not caching it.
func TestReadReturnsValueWhenStoreWriteFails(t *testing.T) {
	store := newFakeStore()
	store.putErr = errors.New("keychain is locked")
	calls := 0
	c := newCache(store, fetches([]byte("v\n"), nil, &calls), time.Unix(1000, 0))

	var warned error
	value, err := c.Read(ref, "k", time.Hour, false, func(e error) { warned = e })
	if err != nil {
		t.Fatalf("want the value despite the write failure, got %v", err)
	}
	if string(value) != "v\n" {
		t.Errorf("want v\\n, got %q", value)
	}
	if warned == nil {
		t.Error("want a warning about the failed write")
	}
}

func TestPurge(t *testing.T) {
	store := newFakeStore()
	store.data["a"] = []byte("1")
	store.data["b"] = []byte("2")
	c := &Cache{Store: store}

	n, err := c.Purge()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("want 2 purged, got %d", n)
	}
	if len(store.data) != 0 {
		t.Errorf("want the store empty, got %v", store.data)
	}
}

func TestPurgeReportsDeleteFailure(t *testing.T) {
	store := newFakeStore()
	store.data["a"] = []byte("1")
	store.deleteErr = errors.New("denied")
	c := &Cache{Store: store}

	n, err := c.Purge()
	if err == nil {
		t.Fatal("want an error when a delete fails")
	}
	if n != 0 {
		t.Errorf("want 0 counted as purged, got %d", n)
	}
}

func TestValidateRef(t *testing.T) {
	for name, tc := range map[string]struct {
		ref     string
		wantErr bool
	}{
		"plain reference": {"op://Vault/Item/field", false},
		"query attribute": {"op://Vault/Item/field?ssh-format=openssh", false},
		"not a reference": {"nonsense", true},
		"a file path":     {"/etc/passwd", true},
		"empty":           {"", true},
		"one-time code":   {"op://Vault/Item/field?attribute=otp", true},
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidateRef(tc.ref)
			if tc.wantErr != (err != nil) {
				t.Errorf("ValidateRef(%q) = %v, wantErr %v", tc.ref, err, tc.wantErr)
			}
		})
	}
}

// The same reference under a different account must not collide.
func TestKeyIncludesAccount(t *testing.T) {
	if Key("work", ref) == Key("personal", ref) {
		t.Error("want different keys for different accounts")
	}
	if Key("work", ref) != Key("work", ref) {
		t.Error("want a stable key for the same inputs")
	}
	if Key("", ref) == Key("", "op://Infra/Other/password") {
		t.Error("want different keys for different references")
	}
	if got := len(Key("", ref)); got != 32 {
		t.Errorf("want a 32-character key, got %d", got)
	}
}
