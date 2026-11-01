package setup

import (
	"bytes"
	"os/exec"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		ok   bool
	}{
		{"ip", Options{Host: "203.0.113.7", Port: 22}, true},
		{"hostname and repos", Options{Host: "box.local", Port: 2222, Repos: []string{"me/app", "org/api.v2"}}, true},
		{"ipv6", Options{Host: "2001:db8::1", Port: 22}, true},
		{"ssh option injection", Options{Host: "-oProxyCommand=evil", Port: 22}, false},
		{"shell in host", Options{Host: "1.2.3.4;rm", Port: 22}, false},
		{"bad port", Options{Host: "1.2.3.4", Port: 0}, false},
		{"shell in repo", Options{Host: "1.2.3.4", Port: 22, Repos: []string{"me/app;rm -rf ~"}}, false},
		{"repo without owner", Options{Host: "1.2.3.4", Port: 22, Repos: []string{"app"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.opts.Validate(); (err == nil) != tt.ok {
				t.Errorf("Validate() = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}

func TestScriptsParse(t *testing.T) {
	for name, script := range map[string][]byte{"bootstrap.sh": bootstrapScript, "finish.sh": finishScript} {
		cmd := exec.CommandContext(t.Context(), "bash", "-n")
		cmd.Stdin = bytes.NewReader(script)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s", name, err, out)
		}
	}
}
