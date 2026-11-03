package ui

import (
	"strings"
	"testing"
)

func TestQR(t *testing.T) {
	q := QR("http://devbox.test")
	lines := strings.Split(strings.TrimRight(q, "\n"), "\n")
	// Square: each line holds two rows of modules.
	width := len([]rune(strings.TrimPrefix(lines[0], "  ")))
	if width < 25 || len(lines) != (width+1)/2 {
		t.Fatalf("not square: %d lines of %d\n%s", len(lines), width, q)
	}
	if strings.Trim(lines[0], " █") != "" {
		t.Error("the first line is quiet zone: all light")
	}
}
