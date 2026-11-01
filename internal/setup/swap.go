package setup

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/ui"
)

// Swap sizes come from the user as typed: "8", "8gb", "0" for none, or "" when
// they haven't said, so outpost asks. Swap is only ever added by choice.

// keepFreeGB is the disk left for code and Docker after a swap file.
const keepFreeGB = 10

// swapFile is the swap file outpost creates, resizes and removes. Other swap,
// such as a partition from the provider, is left alone.
const swapFile = "/swapfile"

// memory is a server's memory, swap and free disk, in GB.
type memory struct {
	ram, swap, file, disk int // file is the part of swap in swapFile
	hasSwap               bool
}

// memoryCmd prints "key value" lines that parseMemory reads.
const memoryCmd = `awk '/^MemTotal:/ {print "ram", int($2/1048576+0.5)} /^SwapTotal:/ {print "swapkb", $2}' /proc/meminfo; ` +
	`echo "file $(( $(stat -c %s ` + swapFile + ` 2>/dev/null || echo 0) / 1073741824 ))"; ` +
	`echo "disk $(df -BG --output=avail / | tail -1 | tr -dc 0-9)"`

func parseMemory(out string) memory {
	var m memory
	for line := range strings.Lines(out) {
		key, val, _ := strings.Cut(strings.TrimSpace(line), " ")
		n, _ := strconv.Atoi(strings.TrimSpace(val))
		switch key {
		case "ram":
			m.ram = n
		case "swapkb":
			m.swap, m.hasSwap = (n+524288)/1048576, n > 0
		case "file":
			m.file = n
		case "disk":
			m.disk = n
		}
	}
	return m
}

func readMemory(ctx context.Context, h remote.Host, user string) (memory, error) {
	out, err := h.Output(ctx, user, memoryCmd)
	if err != nil {
		return memory{}, fmt.Errorf("check memory and disk: %w", err)
	}
	return parseMemory(string(out)), nil
}

// decideSwap turns the user's answer into GB of swap that fit on m, asking
// when there's no answer yet. With no room to ask about, the swap file stays as it is.
func decideSwap(ctx context.Context, m memory, answer string) (int, error) {
	if answer == "" {
		room := m.disk + m.file - keepFreeGB
		if room < 1 {
			lipgloss.Println("  " + ui.Muted.Render(fmt.Sprintf("No room for swap: only %d GB of disk is free", m.disk)))
			return m.file, nil
		}
		now := ""
		if m.hasSwap {
			now = fmt.Sprintf(", %d GB of swap", m.swap)
		}
		var err error
		answer, err = ui.Ask.Input(ctx, "How many GB of disk should the server use as swap?",
			fmt.Sprintf("This server has %d GB of memory%s and %d GB of free disk.\n"+
				"Swap is disk used as backup memory: when agents fill the memory, the server slows\n"+
				"down instead of stopping programs, T3 included. About as much as its memory is plenty.\n"+
				"Enter 0 for none.", m.ram, now, m.disk),
			fmt.Sprintf("0 to %d", room),
			func(s string) error { _, err := swapSize(s, m); return err })
		if err != nil {
			return 0, err
		}
	}
	return swapSize(answer, m)
}

// swapSize reads an answer: a whole number of GB, "GB" optional, that leaves
// keepFreeGB of disk free once the current swap file gives its space back.
func swapSize(answer string, m memory) (int, error) {
	gb, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(answer)), "gb")))
	if err != nil || gb < 0 {
		return 0, fmt.Errorf("%q isn't a size: enter a whole number of GB, or 0 for none", answer)
	}
	if free := m.disk + m.file; gb > 0 && gb > free-keepFreeGB {
		return 0, fmt.Errorf("%d GB of swap doesn't fit: there's %d GB of free disk, and outpost leaves %d GB for your code",
			gb, free, keepFreeGB)
	}
	return gb, nil
}

// installSwap adds the swap an install was asked for, as admin. A server that
// already has swap keeps it; that's what the swap command is for.
func installSwap(ctx context.Context, h remote.Host, admin, answer string) error {
	m, err := readMemory(ctx, h, admin)
	if err != nil {
		return err
	}
	if m.hasSwap {
		lipgloss.Println("  " + ui.Muted.Render(fmt.Sprintf("Swap is already set up (%d GB). To change it: outpost swap %s", m.swap, h)))
		return nil
	}
	gb, err := decideSwap(ctx, m, answer)
	if errors.Is(err, ui.ErrNoTerminal) {
		lipgloss.Println("  " + ui.Muted.Render("No swap added. To add some later: outpost swap "+h.String()))
		return nil
	}
	if err != nil || gb == 0 {
		return err
	}
	return setSwap(ctx, h, admin, gb)
}

// Swap sets a server's swap to the answer's size, asking when it's ""; 0
// removes it. It works on a running server, without a reboot.
func Swap(ctx context.Context, h remote.Host, answer string) error {
	if _, err := openActive(ctx, h); err != nil {
		return err
	}
	m, err := readMemory(ctx, h, DevUser)
	if err != nil {
		return err
	}
	lipgloss.Println("  " + ui.Muted.Render(fmt.Sprintf("%s has %d GB of memory, %d GB of swap and %d GB of free disk", h, m.ram, m.swap, m.disk)))
	gb, err := decideSwap(ctx, m, answer)
	if errors.Is(err, ui.ErrNoTerminal) {
		return fmt.Errorf("run in a terminal to choose, or give the size: outpost swap %s GB", h)
	}
	if err != nil {
		return err
	}
	if gb == m.file {
		ui.Check("Swap", "unchanged")
		return nil
	}
	return setSwap(ctx, h, DevUser, gb)
}

// setSwapCmd replaces swapFile with one of gb, or removes it for 0, and keeps
// the choice at boot. It runs as root.
func setSwapCmd(gb int) string {
	cmd := `set -e
if swapon --show=NAME --noheadings | grep -qx ` + swapFile + `; then swapoff ` + swapFile + `; fi
rm -f ` + swapFile + `
sed -i '\|^` + swapFile + ` |d' /etc/fstab`
	if gb > 0 {
		cmd += fmt.Sprintf(`
fallocate -l %dG %s
chmod 600 %[2]s
mkswap %[2]s >/dev/null
swapon %[2]s
echo '%[2]s none swap sw 0 0' >> /etc/fstab`, gb, swapFile)
	}
	return "sudo bash -c " + quote(cmd)
}

// setSwap makes the server's swap file gb, as user (root, or dev with sudo).
func setSwap(ctx context.Context, h remote.Host, user string, gb int) (err error) {
	p := ui.NewProgress()
	defer func() { p.Done(err) }()
	if gb > 0 {
		p.Task(fmt.Sprintf("Swap (%d GB)", gb))
	} else {
		p.Task("Removing swap")
	}
	if _, err := h.Output(ctx, user, setSwapCmd(gb)); err != nil {
		if strings.Contains(err.Error(), "swapoff") {
			return fmt.Errorf("the old swap couldn't be moved back into memory, which is too full. "+
				"Try again when agents are quieter: %w", err)
		}
		return fmt.Errorf("set swap: %w", err)
	}
	return nil
}
