package setup

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestParseMemory(t *testing.T) {
	m := parseMemory("ram 4\nswapkb 0\nfile 0\ndisk 50\n")
	if m.ram != 4 || m.disk != 50 || m.hasSwap {
		t.Errorf("parsed %+v", m)
	}
	if m := parseMemory("swapkb 4194300\nfile 4\n"); !m.hasSwap || m.swap != 4 || m.file != 4 {
		t.Errorf("parsed %+v; want 4 GB of swap, all in the swap file", m)
	}
}

func TestParseSwapGB(t *testing.T) {
	m := memory{ram: 4, disk: 50} // room for up to 40 GB
	if gb, err := swapSize("0", memory{}); err != nil || gb != 0 {
		t.Errorf("0 must always fit, even on a full disk: got %d, %v", gb, err)
	}
	for _, ok := range []struct {
		in   string
		want int
	}{{"4", 4}, {" 8 ", 8}, {"8GB", 8}, {"8 gb", 8}, {"0", 0}, {"40", 40}} {
		if got, err := swapSize(ok.in, m); err != nil || got != ok.want {
			t.Errorf("%q: got %d, %v; want %d", ok.in, got, err, ok.want)
		}
	}
	for in, want := range map[string]string{"": "whole number", "four": "whole number", "2.5": "whole number", "-1": "whole number", "41": "doesn't fit"} {
		if _, err := swapSize(in, m); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: want an error about %q, got %v", in, want, err)
		}
	}
}

// TestSetSwapCmd runs the swap command's file handling on a scratch fstab,
// with the commands that need root stubbed out.
func TestSetSwapCmd(t *testing.T) {
	if out, _ := exec.Command("sed", "--version").Output(); !strings.Contains(string(out), "GNU") {
		t.Skip("needs GNU sed, as on the servers")
	}
	dir := t.TempDir()
	fstab := dir + "/fstab"
	if err := os.WriteFile(fstab, []byte("UUID=abc / ext4 defaults 0 1\n/swapfile none swap sw 0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// sudo runs the script in this shell, where the stubs are.
	stubs := `sudo() { shift 2; eval "$1"; }; swapon() { :; }; swapoff() { :; }; fallocate() { :; }; chmod() { :; }; mkswap() { :; }; rm() { :; }; `
	for _, gb := range []int{8, 0} {
		cmd := strings.ReplaceAll(setSwapCmd(gb), "/etc/fstab", fstab)
		if out, err := exec.CommandContext(t.Context(), "bash", "-c", stubs+cmd).CombinedOutput(); err != nil {
			t.Fatalf("%d GB: %v\n%s", gb, err, out)
		}
		got, _ := os.ReadFile(fstab)
		if n := strings.Count(string(got), "/swapfile "); n != min(gb, 1) {
			t.Errorf("%d GB: fstab has %d swap file lines, want %d:\n%s", gb, n, min(gb, 1), got)
		}
		if !strings.Contains(string(got), "UUID=abc") {
			t.Errorf("%d GB: other fstab lines must be kept", gb)
		}
	}
}
