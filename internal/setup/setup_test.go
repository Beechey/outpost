package setup

import (
	"bytes"
	"os/exec"
	"testing"
)

func TestBootstrapScriptParses(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "bash", "-n")
	cmd.Stdin = bytes.NewReader(bootstrapScript)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("bootstrap.sh: %v\n%s", err, out)
	}
}
