package setup

import (
	"bytes"
	"os/exec"
	"testing"
)

func TestValidateServer(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
		ok   bool
	}{
		{"ip", "203.0.113.7", 0, true},
		{"hostname with port", "box.local", 2222, true},
		{"ipv6", "2001:db8::1", 22, true},
		{"ssh option injection", "-oProxyCommand=evil", 0, false},
		{"shell in host", "1.2.3.4;rm", 0, false},
		{"command substitution", "$(reboot)", 0, false},
		{"port too high", "1.2.3.4", 70000, false},
		{"negative port", "1.2.3.4", -1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateServer(tt.host, tt.port); (err == nil) != tt.ok {
				t.Errorf("validateServer(%q, %d) = %v, want ok=%v", tt.host, tt.port, err, tt.ok)
			}
		})
	}
}

func TestMoveRejectsSameServer(t *testing.T) {
	if err := Move(t.Context(), MoveOptions{Old: "1.2.3.4", New: "1.2.3.4"}); err == nil {
		t.Error("Move to the same server should fail")
	}
}

func TestScriptsParse(t *testing.T) {
	for name, script := range map[string][]byte{"bootstrap.sh": bootstrapScript, "survey.sh": surveyScript} {
		cmd := exec.CommandContext(t.Context(), "bash", "-n")
		cmd.Stdin = bytes.NewReader(script)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s", name, err, out)
		}
	}
}
