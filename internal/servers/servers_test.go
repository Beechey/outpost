package servers

import (
	"testing"

	"github.com/Beechey/outpost/internal/remote"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in   string
		want remote.Server
		ok   bool
	}{
		{"203.0.113.7", remote.Server{Addr: "203.0.113.7"}, true},
		{"box.local:2222", remote.Server{Addr: "box.local", Port: 2222}, true},
		{" box \n", remote.Server{Addr: "box"}, true},
		{"2001:db8::1", remote.Server{Addr: "2001:db8::1"}, true},
		{"box:ssh", remote.Server{}, false},
		{"box:99999", remote.Server{}, false},
		{"-oProxyCommand=x", remote.Server{}, false},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if (err == nil) != tt.ok || (tt.ok && got != tt.want) {
			t.Errorf("Parse(%q) = %+v, %v; want %+v, ok=%v", tt.in, got, err, tt.want, tt.ok)
		}
	}
}

func TestRememberAndForget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	a, b := remote.Server{Addr: "a"}, remote.Server{Addr: "b", Port: 2222}

	for _, srv := range []remote.Server{a, b, a} {
		if err := Remember(srv); err != nil {
			t.Fatal(err)
		}
	}
	if got := List(); len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("List() = %v, want [a b:2222], most recent first, no duplicates", got)
	}
	if err := Forget(a); err != nil {
		t.Fatal(err)
	}
	if got := List(); len(got) != 1 || got[0] != b {
		t.Fatalf("after Forget: %v", got)
	}
}
