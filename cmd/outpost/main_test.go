package main

import "testing"

func TestBinDir(t *testing.T) {
	for in, want := range map[string]string{
		"\n/Users/me/go\n":           "/Users/me/go/bin", // GOBIN unset: GOPATH's bin
		"/opt/gobin\n/Users/me/go\n": "/opt/gobin",
	} {
		if got := binDir(in); got != want {
			t.Errorf("binDir(%q) = %q, want %q", in, got, want)
		}
	}
}
