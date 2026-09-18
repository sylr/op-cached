# op-cached

Cache [1Password CLI](https://developer.1password.com/docs/cli/) secret reads in
the macOS keychain, so repeated `op read` calls cost milliseconds instead of a
second each.

```console
$ time op read "op://Infra/ansible vault/password"
op read  1.02s total

$ time op-cached read "op://Infra/ansible vault/password"
op-cached read  0.04s total
```

## Why `op read` is slow

It is not authentication, and it is not fetching the secret. `op --debug read`
on 1Password CLI 2.39.0 shows where the time actually goes:

```
server request dur=403ms  GET  /api/v2/overview
server request dur=150ms  GET  /api/v3/account
server request dur=167ms  POST /api/v3/user/itemusage
server requests complete  requests=3 total_request_time=720ms

Item: cache hit on item ... data_source=cache dur=2ms   <- the secret
```

The secret itself comes from `op`'s own local cache in 2ms. The ~900ms is
account-overview sync and usage telemetry, paid once per invocation no matter
what you ask for. There is no offline flag to suppress it.

Two consequences:

- **If you need several secrets at once, batch them.** One `op inject` or
  `op run` pays the ~900ms once. Measured with three distinct items: 2.97s as
  separate `op read` calls versus 0.85s batched. You do not need this tool for
  that — just stop calling `op` in a loop.
- **If you need the same secret repeatedly across separate invocations**, batching
  does not help, and that is what `op-cached` is for.

## Install

Homebrew:

```console
brew install sylr/tap/op-cached
```

Nix flake:

```nix
{
  inputs.op-cached.url = "github:sylr/op-cached";

  # ... then, for example, in a devShell:
  #   packages = [ inputs.op-cached.packages.${system}.default ];
}
```

Or run it straight from the flake:

```console
nix run github:sylr/op-cached -- read "op://Infra/ansible vault/password"
```

## Usage

```console
op-cached read op://Vault/Item/field [--ttl SECONDS] [--refresh]
op-cached purge
```

- `--ttl SECONDS` overrides the cache lifetime for this call. Default 12h, or
  set `OP_CACHE_TTL`.
- `--refresh` bypasses the cache and re-reads from 1Password, updating the entry.
- `purge` deletes every entry this tool created.

Output is byte-identical to `op read`, trailing newline included, so it is a
drop-in replacement:

```bash
ansible-playbook -i inventories/aws/<account> \
  --vault-pass-file <(op-cached read "op://Infra/ansible vault/password") \
  -l <host-pattern> playbook.yml
```

### Environment

| Variable | Default | Meaning |
|---|---|---|
| `OP_CACHE_TTL` | `43200` | Cache lifetime in seconds. |
| `OP_CACHE_KEYCHAIN` | `~/Library/Keychains/login.keychain-db` | Keychain to store entries in. |
| `OP_CACHE_CONFIRM` | unset | Set to `1` to create entries with `-T ""`, forcing an approval dialog on every access. |
| `OP_ACCOUNT` | unset | Part of the cache key, so the same reference under different accounts does not collide. |
| `XDG_CACHE_HOME` | `~/.cache` | Parent of the index file, which holds key hashes only — never secrets. |

## Security

Cached values are encrypted at rest in the keychain, but understand what that
does and does not buy you.

**By default, any process running as you can read them back without a prompt.**
The keychain ACL trusts the application that created the entry, which is
`/usr/bin/security` — not this script. Any other program can invoke the same
binary while your keychain is unlocked. This is a real widening compared with
letting `op` hit the network each time, where authorization is scoped to the
terminal session and revoked when 1Password locks.

Set `OP_CACHE_CONFIRM=1` to create entries with `-T ""` instead, which removes
that automatic trust and forces an approval dialog per access. Granting "Always
Allow" at that dialog puts you back where you started.

The payload is passed to `security` on stdin rather than in `argv`, so it is not
visible to `ps` or to execution monitors.

**The TTL governs freshness, not revocation.** Rotating a secret in 1Password
does not invalidate a cached copy: callers can keep receiving the old value for
up to the TTL. Run `op-cached purge`, or use `--refresh`, after rotating.

One-time passwords (`?attribute=otp`) are refused outright, since a cached OTP is
already dead.

## Limitations

- macOS only: it depends on `/usr/bin/security` and a macOS keychain.
- Secrets containing NUL bytes are not preserved — a shell variable cannot hold
  them. Text secrets are fine.
- Concurrent refreshes of the same reference can race; the last writer wins.
- The cache key uses `$OP_ACCOUNT`, not the resolved account, to avoid paying for
  an `op whoami` on every read. If you change your default account, run
  `op-cached purge`.

## Development

```console
nix develop           # shellcheck, goreleaser, gh
./test/op-cached_test.sh
```

The test suite runs entirely against stubbed `op` and `security` binaries: it
never touches a real keychain and never reads a real secret.

## Licence

MIT
