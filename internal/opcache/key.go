package opcache

import (
	"crypto/sha256"
	"encoding/hex"
)

// Key derives the store key for a reference.
//
// The account is part of the identity: the same op:// reference can resolve to
// a different secret under a different 1Password account, and a key built from
// the reference alone would hand back the wrong one.
func Key(account, ref string) string {
	sum := sha256.Sum256([]byte(account + "\n" + ref))
	return hex.EncodeToString(sum[:])[:32]
}
