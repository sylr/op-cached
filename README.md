# op-cached

Cache [1Password CLI](https://developer.1password.com/docs/cli/) secret reads in
the macOS keychain, so repeated `op read` calls cost milliseconds instead of a
second each.

```console
$ time op read "op://Infra/ansible vault/password"
op read  1.02s total

$ time op-cached read "op://Infra/ansible vault/password"
op-cached read  0.02s total
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
brew install --cask sylr/tap/op-cached
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
op-cached read op://Vault/Item/field [--ttl DURATION] [--refresh]
op-cached purge
op-cached version
```

- `--ttl DURATION` overrides the cache lifetime for this call, e.g. `30m`, `12h`.
  Default 12h, or set `OP_CACHE_TTL`.
- `--refresh` ignores any cached value, re-reads from 1Password and updates the
  entry.
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
| `OP_CACHE_TTL` | `43200` | Cache lifetime. A bare integer is seconds; a Go duration such as `90m` also works. |
| `OP_ACCOUNT` | unset | Read by `op` itself, and mixed into the cache key so the same reference under different accounts does not collide. |

## Security

Entries are stored as generic passwords in your default keychain, encrypted at
rest. Understand what that does and does not buy you.

**Any process running as you can still read the cached values back, without a
prompt, while your keychain is unlocked.** This is measured, not assumed:

```console
$ security find-generic-password -s op-cached -w
<returns the data, exit 0, no prompt>
```

A keychain ACL binds to a code signature, and a Homebrew-installed binary is
only ad-hoc signed, so there is no stable code identity for the ACL to restrict
access to. Restricting it properly would need a Developer ID signature and
notarisation, which this project does not do. Treat the cache as "encrypted on
disk, readable by you and anything running as you".

That is a real widening compared with letting `op` hit the network every time,
where authorization is scoped to the terminal session and revoked when 1Password
locks. It is the trade this tool makes; if it is not one you want, use
`op inject` to batch instead.

What it does avoid: the secret is never passed as a command-line argument, so it
does not appear in `ps` output or to execution monitors — unlike a wrapper built
around `security -w`.

**The TTL governs freshness, not revocation.** Rotating a secret in 1Password
does not invalidate a cached copy: callers can keep receiving the old value for
up to the TTL. Run `op-cached purge`, or use `--refresh`, after rotating.

One-time passwords (`?attribute=otp`) are refused outright, since a cached OTP is
already dead.

## Limitations

- macOS only: it links against Security.framework.
- Concurrent refreshes of the same reference can race; the last writer wins.
- The cache key uses `$OP_ACCOUNT`, not the account `op` ultimately resolves, to
  avoid paying for an `op whoami` on every read. If you change your default
  account, run `op-cached purge`.

## Development

```console
nix develop      # go, golangci-lint, goreleaser, gh
go test ./...
```

The tests run against a fake store and a fake fetcher: they never touch a real
keychain and never read a real secret.

## Licence

MIT
