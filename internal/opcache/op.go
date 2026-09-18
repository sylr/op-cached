package opcache

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// OpRead shells out to `op read` for a single reference.
//
// Stdout is returned verbatim, trailing newline included, so op-cached is a
// drop-in replacement for `op read` in constructs such as
// `--vault-pass-file <(op-cached read ...)`.
func OpRead(ref string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("op", "read", ref)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("op read: %s", msg)
		}
		return nil, fmt.Errorf("op read: %w", err)
	}
	return stdout.Bytes(), nil
}
