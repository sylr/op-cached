package main

import (
	"strings"
	"testing"
	"time"
)

func TestResolveTTL(t *testing.T) {
	for name, tc := range map[string]struct {
		flag    time.Duration
		env     string
		envSet  bool
		want    time.Duration
		wantErr bool
	}{
		"default":                   {0, "", false, defaultTTL, false},
		"flag wins over env":        {30 * time.Minute, "3600", true, 30 * time.Minute, false},
		"bare integer is seconds":   {0, "3600", true, time.Hour, false},
		"duration string":           {0, "90m", true, 90 * time.Minute, false},
		"empty env falls back":      {0, "", true, defaultTTL, false},
		"zero seconds is valid":     {0, "0", true, 0, false},
		"negative flag":             {-time.Second, "", false, 0, true},
		"negative env":              {0, "-5", true, 0, true},
		"negative env duration":     {0, "-5m", true, 0, true},
		"nonsense env":              {0, "tomorrow", true, 0, true},
		"env is not shell arith":    {0, "ts[$(echo hi)0]", true, 0, true},
		"leading zero is not octal": {0, "08", true, 8 * time.Second, false},
	} {
		t.Run(name, func(t *testing.T) {
			if tc.envSet {
				t.Setenv("OP_CACHE_TTL", tc.env)
			}
			got, err := resolveTTL(tc.flag)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %s", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("want %s, got %s", tc.want, got)
			}
		})
	}
}

func TestRunRejectsBadInvocations(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"no arguments":       {nil, "usage"},
		"unknown command":    {[]string{"frobnicate"}, "unknown command"},
		"read without a ref": {[]string{"read"}, "needs a secret reference"},
		"two references":     {[]string{"read", "op://a/b/c", "op://d/e/f"}, "exactly one reference"},
		"unknown flag":       {[]string{"read", "--nope", "op://a/b/c"}, "flag provided but not defined"},
		"purge with an arg":  {[]string{"purge", "--wat"}, "takes no arguments"},
	} {
		t.Run(name, func(t *testing.T) {
			err := run(tc.args)
			if err == nil {
				t.Fatal("want an error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want an error containing %q, got %q", tc.want, err)
			}
		})
	}
}

// These must fail before op or the keychain is touched at all.
func TestRunRejectsBadReferencesWithoutSideEffects(t *testing.T) {
	for name, ref := range map[string]string{
		"not a reference": "nonsense",
		"one-time code":   "op://Vault/Item/field?attribute=otp",
	} {
		t.Run(name, func(t *testing.T) {
			if err := run([]string{"read", ref}); err == nil {
				t.Fatal("want an error, got nil")
			}
		})
	}
}
