#!/usr/bin/env bash
# Test suite for op-cached.
#
# Runs entirely against stubbed `op` and `security` binaries: no real keychain
# is touched and no real secret is read, so this is safe in CI and locally.

set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OC="$ROOT/op-cached"
TB="$(mktemp -d)"
trap 'rm -rf "$TB"' EXIT

mkdir -p "$TB/bin" "$TB/cache" "$TB/kc"

cat > "$TB/bin/op" <<'EOF'
#!/bin/bash
case "$OP_STUB_MODE" in
  fail_partial) printf 'partial\n\n'; exit 7 ;;
  ok)           printf 'SECRET-VALUE\n'; exit 0 ;;
  *)            exit 1 ;;
esac
EOF

cat > "$TB/bin/security" <<'EOF'
#!/bin/bash
D="$SEC_STORE"; mkdir -p "$D"
prev=""; s=""
for a in "$@"; do [[ $prev == -s ]] && s=$a; prev=$a; done
case "$1" in
  find-generic-password)   [[ -f "$D/$s" ]] && { cat "$D/$s"; exit 0; }; exit 44 ;;
  add-generic-password)    [[ "${SEC_PUT_FAIL:-0}" == 1 ]] && exit 1
                           cat > "$D/$s"; echo stdin >> "$D/.log"; exit 0 ;;
  delete-generic-password) [[ "${SEC_DEL_FAIL:-0}" == 1 ]] && exit 1
                           rm -f "$D/$s"; exit 0 ;;
esac
exit 1
EOF
chmod +x "$TB/bin/op" "$TB/bin/security"

export PATH="$TB/bin:$PATH" XDG_CACHE_HOME="$TB/cache" SEC_STORE="$TB/kc"
REF="op://Infra/Test/password"
INDEX="$TB/cache/op-cached/index"

pass=0; fail=0
ok()   { pass=$((pass+1)); printf '  ok   %s\n' "$1"; }
bad()  { fail=$((fail+1)); printf '  FAIL %s\n    want: %s\n    got:  %s\n' "$1" "$2" "$3"; }
is()   { [[ "$2" == "$3" ]] && ok "$1" || bad "$1" "$2" "$3"; }
has()  { [[ "$2" == *"$3"* ]] && ok "$1" || bad "$1" "*$3*" "$2"; }
reset(){ rm -rf "$TB/kc" "$TB/cache"; mkdir -p "$TB/kc"; }

echo "== a failing op must not be cached or reported as success =="
reset; export OP_STUB_MODE=fail_partial
out="$(bash "$OC" read "$REF" 2>/dev/null)"; rc=$?
is "exit code is non-zero"        "1"  "$rc"
is "no partial stdout is emitted" ""   "$out"
is "nothing was cached"           ""   "$(ls -A "$TB/kc" 2>/dev/null)"

echo "== happy path =="
reset; export OP_STUB_MODE=ok
out="$(bash "$OC" read "$REF" 2>/dev/null)"; rc=$?
is "exit code is zero"                   "0"              "$rc"
is "trailing newline is preserved"       "SECRET-VALUE"   "$out"
is "payload reaches security via stdin"  "stdin"          "$(cat "$TB/kc/.log" 2>/dev/null)"

echo "== a warm read is served from cache, not from op =="
export OP_STUB_MODE=broken   # any call to op now fails
is "cached value is returned" "SECRET-VALUE" "$(bash "$OC" read "$REF" 2>/dev/null)"

echo "== malformed cache records fall back to a fresh fetch =="
export OP_STUB_MODE=ok
KEY="$(ls "$TB/kc" | grep '^op-cached:' | head -1)"
NOW="$(date +%s)"
for rec in "$NOW" "$NOW:" "$NOW:bad!!base64" "$((NOW + 99999)):U0VDUkVU"; do
  printf '%s' "$rec" > "$TB/kc/$KEY"
  is "refetched after record '${rec:0:18}...'" "SECRET-VALUE" "$(bash "$OC" read "$REF" 2>/dev/null)"
done

echo "== a future timestamp does not satisfy --ttl 0 =="
printf '%s' "$((NOW + 99999)):U0VDUkVU" > "$TB/kc/$KEY"
is "refetched despite ttl 0" "SECRET-VALUE" "$(bash "$OC" read "$REF" --ttl 0 2>/dev/null)"

echo "== argument validation =="
has "--ttl without a value"   "$(bash "$OC" read "$REF" --ttl 2>&1)"                        "--ttl requires a value"
has "--ttl is not arithmetic" "$(bash "$OC" read "$REF" --ttl 'x[$(echo hi >&2)0]' 2>&1)"   "must be a non-negative integer"
has "a second reference"      "$(bash "$OC" read "$REF" op://V/J/g 2>&1)"                   "expected exactly one reference"
has "a non-reference"         "$(bash "$OC" read nonsense 2>&1)"                            "not a secret reference"
has "a one-time password"     "$(bash "$OC" read 'op://V/I/f?attribute=otp' 2>&1)"          "refusing to cache"
has "an unknown option"       "$(bash "$OC" read "$REF" --nope 2>&1)"                       "unknown option"
has "purge with arguments"    "$(bash "$OC" purge --wat 2>&1)"                              "takes no arguments"
has "no subcommand"           "$(bash "$OC" 2>&1)"                                          "usage: op-cached"
is  "--ttl 08 is not octal"   "SECRET-VALUE" "$(bash "$OC" read "$REF" --ttl 08 2>/dev/null)"

echo "== purge =="
reset; bash "$OC" read "$REF" >/dev/null 2>&1
has "a failed delete retains the entry" "$(SEC_DEL_FAIL=1 bash "$OC" purge 2>&1)" "1 retained"
is  "the index survives"                "yes" "$([ -f "$INDEX" ] && echo yes || echo no)"
printf 'unrelated-service\n' >> "$INDEX"
has "foreign entries are skipped"       "$(bash "$OC" purge 2>&1)" "skipping unrecognised index entry"
is  "our secret is gone"                ""    "$(ls -A "$TB/kc" | grep '^op-cached:' || true)"
reset; has "purge with nothing cached"  "$(bash "$OC" purge 2>&1)" "nothing cached"

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[[ "$fail" -eq 0 ]]
