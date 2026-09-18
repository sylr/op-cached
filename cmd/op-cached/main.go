// Command op-cached memoises `op read` in the macOS keychain.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/sylr/op-cached/internal/opcache"
)

// Set by goreleaser at build time.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const defaultTTL = 12 * time.Hour

const usage = `op-cached memoises 1Password CLI secret reads in the macOS keychain.

usage:
  op-cached read op://Vault/Item/field [--ttl DURATION] [--refresh]
  op-cached purge
  op-cached version

flags:
  --ttl DURATION   cache lifetime, e.g. 30m, 12h (default 12h, or $OP_CACHE_TTL)
  --refresh        ignore any cached value and re-read from 1Password
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "op-cached: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "read":
		return cmdRead(args[1:])
	case "purge":
		return cmdPurge(args[1:])
	case "version", "--version", "-v":
		fmt.Printf("op-cached %s (%s, %s)\n", version, commit, date)
		return nil
	case "help", "--help", "-h":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
	}
}

func cmdRead(args []string) error {
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	ttl := fs.Duration("ttl", 0, "cache lifetime")
	refresh := fs.Bool("refresh", false, "ignore any cached value")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Exactly one reference: silently reading the second of two would be a
	// quiet way to hand back the wrong secret.
	switch fs.NArg() {
	case 1:
	case 0:
		return errors.New("read needs a secret reference, e.g. op://Vault/Item/field")
	default:
		return fmt.Errorf("read takes exactly one reference, got %d", fs.NArg())
	}
	ref := fs.Arg(0)

	lifetime, err := resolveTTL(*ttl)
	if err != nil {
		return err
	}

	account := os.Getenv("OP_ACCOUNT")
	cache := &opcache.Cache{Store: opcache.Keychain{}, Fetch: opcache.OpRead}
	warn := func(err error) { fmt.Fprintf(os.Stderr, "op-cached: warning: %v\n", err) }

	value, err := cache.Read(ref, opcache.Key(account, ref), lifetime, *refresh, warn)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(value)
	return err
}

// resolveTTL takes the --ttl flag when given, else $OP_CACHE_TTL, else the
// default. A bare integer in OP_CACHE_TTL is read as seconds, which is what the
// shell implementation this replaces accepted.
func resolveTTL(flagValue time.Duration) (time.Duration, error) {
	if flagValue != 0 {
		if flagValue < 0 {
			return 0, fmt.Errorf("--ttl must not be negative: %s", flagValue)
		}
		return flagValue, nil
	}

	raw, ok := os.LookupEnv("OP_CACHE_TTL")
	if !ok || raw == "" {
		return defaultTTL, nil
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		if secs < 0 {
			return 0, fmt.Errorf("OP_CACHE_TTL must not be negative: %s", raw)
		}
		return time.Duration(secs) * time.Second, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("OP_CACHE_TTL is not a number of seconds or a duration: %s", raw)
	}
	if d < 0 {
		return 0, fmt.Errorf("OP_CACHE_TTL must not be negative: %s", raw)
	}
	return d, nil
}

func cmdPurge(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("purge takes no arguments, got %q", args[0])
	}
	cache := &opcache.Cache{Store: opcache.Keychain{}}
	n, err := cache.Purge()
	fmt.Printf("op-cached: purged %d entries\n", n)
	return err
}
