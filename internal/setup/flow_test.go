package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSetupFreshServer(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("fresh", "root").healthy()

	if err := Setup(ctx, srv, AccessUnset, "", "", GatewayOptions{}); err != nil {
		t.Fatal(err)
	}
	if !srv.ran("root: install script") {
		t.Error("the install should run as root on a fresh server")
	}
	if !srv.ran("systemctl reboot") {
		t.Error("a fresh server should be rebooted to prove it starts by itself")
	}
}

func TestSetupCopiesKeyWhenNeeded(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("fresh").healthy() // nobody can log in yet

	if err := Setup(ctx, srv, AccessUnset, "", "", GatewayOptions{}); err != nil {
		t.Fatal(err)
	}
	if !srv.ran("ssh-copy-id root") {
		t.Error("should copy the key to root first")
	}
}

func TestSetupServerInUseSkipsReboot(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("busy", DevUser).healthy().on(cmdT3Installed, "true", nil)

	if err := Setup(ctx, srv, AccessUnset, "", "", GatewayOptions{}); err != nil {
		t.Fatal(err)
	}
	if srv.ran("systemctl reboot") {
		t.Error("a server already running T3 must not be rebooted")
	}
	if !srv.ran("dev: install script") {
		t.Error("a re-run should install as dev")
	}
}

func TestSetupRefusesRetiredServer(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("old", DevUser).healthy().on(cmdMovedTo, "new:22\n", nil)

	err := Setup(ctx, srv, AccessUnset, "", "", GatewayOptions{})
	if err == nil || !strings.Contains(err.Error(), "retired") {
		t.Fatalf("want a retired-server error, got %v", err)
	}
	if srv.ran("install script") {
		t.Error("nothing should be installed on a retired server")
	}
}

func TestSetupReportsServicesThatDontStart(t *testing.T) {
	ctx := testEnv(t)
	saved := servicesTimeout
	servicesTimeout = 0
	t.Cleanup(func() { servicesTimeout = saved })
	srv := newFake("broken", DevUser).on(cmdT3Installed, "true", nil).
		on(cmdServices, "docker active\ntailscaled active\nT3 failed\n", nil)

	err := Setup(ctx, srv, AccessUnset, "", "", GatewayOptions{})
	if err == nil || !strings.Contains(err.Error(), "T3 is failed") {
		t.Fatalf("want T3 named as not running, got %v", err)
	}
}

// movable is an old server with work to move.
func movable(name string) *fakeHost {
	return newFake(name, DevUser).on(surveyCmd(), "path code\npath .t3/userdata\nt3 yes\nprojects 2\nsessions 5\nsize 1.2G\n", nil)
}

func TestMoveCopiesAndRetiresOld(t *testing.T) {
	ctx := testEnv(t)
	old, dst := movable("old"), newFake("new", "root").healthy()

	if err := Move(ctx, old, dst, true, AccessUnset, "", ""); err != nil {
		t.Fatal(err)
	}
	if !old.ran("copy to new") {
		t.Error("should copy to the new server")
	}
	if !dst.ran(clearCmd()) {
		t.Error("should clear T3 data on the new server before copying")
	}
	if !old.ran("t3 service uninstall") || !old.ran("echo 'new' > "+movedToFile) {
		t.Error("should retire T3 on the old server and record where it moved")
	}
	if !dst.ran("systemctl reboot") {
		t.Error("a first move should reboot the new server")
	}
}

func TestMoveCopyFailureRestartsOld(t *testing.T) {
	ctx := testEnv(t)
	old, dst := movable("old"), newFake("new", "root").healthy()
	old.copyErr = errors.New("disk full")

	err := Move(ctx, old, dst, true, AccessUnset, "", "")
	if err == nil || !strings.Contains(err.Error(), "running on old again") {
		t.Fatalf("want a restarted-old error, got %v", err)
	}
	if !old.ran(cmdT3Start) {
		t.Error("T3 should be started again on the old server")
	}
	if old.ran("t3 service uninstall") {
		t.Error("the old server must not be retired after a failed copy")
	}
}

func TestMoveCopyFailureSaysWhenRestartFails(t *testing.T) {
	ctx := testEnv(t)
	old, dst := movable("old"), newFake("new", "root").healthy()
	old.copyErr = errors.New("disk full")
	old.on(cmdT3Start, "", errors.New("unit failed"))

	err := Move(ctx, old, dst, true, AccessUnset, "", "")
	if err == nil || !strings.Contains(err.Error(), "still stopped") {
		t.Fatalf("want a still-stopped error, got %v", err)
	}
}

func TestMoveRerunDoesntCopyOrReboot(t *testing.T) {
	ctx := testEnv(t)
	old := movable("old").on(surveyCmd(), "t3 yes\nmoved new\n", nil)
	dst := newFake("new", DevUser).healthy().on(cmdT3Installed, "true", nil)

	if err := Move(ctx, old, dst, true, AccessUnset, "", ""); err != nil {
		t.Fatal(err)
	}
	if old.ran("copy to") {
		t.Error("a finished move must not copy again")
	}
	if dst.ran("systemctl reboot") {
		t.Error("a re-run must not reboot a server agents may be using")
	}
}

func TestMoveRefusesServerMovedElsewhere(t *testing.T) {
	ctx := testEnv(t)
	old := movable("old").on(surveyCmd(), "t3 yes\nmoved other\n", nil)
	dst := newFake("new", "root").healthy()

	err := Move(ctx, old, dst, true, AccessUnset, "", "")
	if err == nil || !strings.Contains(err.Error(), "already moved to other") {
		t.Fatalf("want an already-moved error, got %v", err)
	}
	if dst.ran("install script") {
		t.Error("nothing should happen on the new server")
	}
}

func TestMoveNeedsConfirmationWithoutTerminal(t *testing.T) {
	ctx := testEnv(t)
	old, dst := movable("old"), newFake("new", "root").healthy()

	err := Move(ctx, old, dst, false, AccessUnset, "", "") // tests have no terminal
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("want a confirm error, got %v", err)
	}
	if old.ran(cmdT3Stop) {
		t.Error("T3 on the old server must not stop before the move is confirmed")
	}
}

func TestSetupTailscaleMode(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("fresh", "root").healthy().on(t3Logins[AccessTailscale].Check, "", errors.New("not yet"))

	if err := Setup(ctx, srv, AccessTailscale, "", "", GatewayOptions{}); err != nil {
		t.Fatal(err)
	}
	if !srv.ran("t3 up --lan --no-connect") || !srv.ran("t3 pair --tailscale") {
		t.Error("should serve T3 on the tailnet only and print a pairing link")
	}
	if srv.ran("--connect --no-lan") {
		t.Error("must not turn T3 Connect on")
	}
}

func TestSetupKeepsTailscaleOnRerun(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("busy", DevUser).healthy() // every check passes: already on the tailnet only

	if err := Setup(ctx, srv, AccessUnset, "", "", GatewayOptions{}); err != nil {
		t.Fatal(err)
	}
	if srv.ran("t3 up") {
		t.Error("a re-run must keep the server's access as it is")
	}
}

func TestSetupSwitchesToConnect(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("busy", DevUser).healthy().on(t3Logins[AccessConnect].Check, "", errors.New("off"))

	if err := Setup(ctx, srv, AccessConnect, "", "", GatewayOptions{}); err != nil {
		t.Fatal(err)
	}
	if !srv.ran("t3 up --connect --no-lan") {
		t.Error("--connect should switch T3 Connect on")
	}
}

func TestPairRefusesConnectServer(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("box", DevUser).on(t3Logins[AccessTailscale].Check, "", errors.New("connect"))

	err := Pair(ctx, srv)
	if err == nil || !strings.Contains(err.Error(), "T3 Connect") {
		t.Fatalf("want a uses-T3-Connect error, got %v", err)
	}
	if srv.ran("t3 pair") {
		t.Error("must not pair a T3 Connect server")
	}
}

func TestPairTailscaleServer(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("box", DevUser)

	if err := Pair(ctx, srv); err != nil {
		t.Fatal(err)
	}
	if !srv.ran("t3 pair --tailscale") {
		t.Error("should print a pairing link")
	}
}

// addsSwap reports whether a swap file of gb was made.
func addsSwap(f *fakeHost, gb int) bool { return f.ran(fmt.Sprintf("fallocate -l %dG", gb)) }

// madeSwap reports whether any swap file was made.
func madeSwap(f *fakeHost) bool { return f.ran("fallocate") }

// roomy answers the memory check: 4 GB of memory, no swap, 50 GB of free disk.
func roomy(f *fakeHost) *fakeHost {
	return f.on(memoryCmd, "ram 4\nswapkb 0\nfile 0\ndisk 50\n", nil)
}

func TestSetupAddsSwapWhenChosen(t *testing.T) {
	ctx := testEnv(t)
	srv := roomy(newFake("fresh", "root").healthy())

	if err := Setup(ctx, srv, AccessUnset, "4", "", GatewayOptions{}); err != nil {
		t.Fatal(err)
	}
	if !addsSwap(srv, 4) {
		t.Error("should add 4 GB of swap")
	}
}

func TestSetupAddsNoSwapUnlessChosen(t *testing.T) {
	for _, gb := range []string{"", "0"} { // unanswered (no terminal), or none
		ctx := testEnv(t)
		srv := roomy(newFake("fresh", "root").healthy())

		if err := Setup(ctx, srv, AccessUnset, gb, "", GatewayOptions{}); err != nil {
			t.Fatal(err)
		}
		if madeSwap(srv) {
			t.Errorf("swap %q: no swap should be added without the user choosing it", gb)
		}
	}
}

func TestSetupKeepsExistingSwap(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("busy", DevUser).healthy().on(memoryCmd, "ram 4\nswapkb 2097148\nfile 2\ndisk 50\n", nil)

	if err := Setup(ctx, srv, AccessUnset, "8", "", GatewayOptions{}); err != nil {
		t.Fatal(err)
	}
	if madeSwap(srv) {
		t.Error("a server with swap keeps it as it is")
	}
}

func TestSetupRefusesSwapThatDoesntFit(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("small", "root").healthy().on(memoryCmd, "ram 2\nswapkb 0\nfile 0\ndisk 14\n", nil)

	err := Setup(ctx, srv, AccessUnset, "8", "", GatewayOptions{})
	if err == nil || !strings.Contains(err.Error(), "doesn't fit") {
		t.Fatalf("want a doesn't-fit error, got %v", err)
	}
	if srv.ran("install script") {
		t.Error("nothing should be installed")
	}
}

func TestMoveWithYesAddsNoSwap(t *testing.T) {
	ctx := testEnv(t)
	old, dst := movable("old"), roomy(newFake("new", "root").healthy())

	if err := Move(ctx, old, dst, true, AccessUnset, "", ""); err != nil {
		t.Fatal(err)
	}
	if madeSwap(dst) {
		t.Error("--yes asks nothing, so no swap unless --swap is given")
	}
}

func TestBackupPausesT3AndSavesFile(t *testing.T) {
	ctx := testEnv(t)
	srv := movable("box")
	file := filepath.Join(t.TempDir(), "box.tar.gz")

	if err := Backup(ctx, srv, file, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(file); string(got) != "archive" {
		t.Errorf("file holds %q, want the downloaded archive", got)
	}
	if !srv.ran(cmdT3Stop) || !srv.ran(cmdT3Start) {
		t.Error("T3 should pause for the download, then start again")
	}
}

func TestBackupFailureLeavesNoFileAndRestartsT3(t *testing.T) {
	ctx := testEnv(t)
	srv := movable("box")
	srv.copyErr = errors.New("connection lost")
	dir := t.TempDir()

	if err := Backup(ctx, srv, filepath.Join(dir, "box.tar.gz"), true); err == nil {
		t.Fatal("want the download error")
	}
	if left, _ := os.ReadDir(dir); len(left) > 0 {
		t.Errorf("a failed backup left %s behind", left[0].Name())
	}
	if !srv.ran(cmdT3Start) {
		t.Error("T3 should start again after a failed download")
	}
}

func TestBackupRefusesExistingFile(t *testing.T) {
	ctx := testEnv(t)
	srv := movable("box")
	file := filepath.Join(t.TempDir(), "box.tar.gz")
	if err := os.WriteFile(file, []byte("older backup"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := Backup(ctx, srv, file, true)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("want an already-exists error, got %v", err)
	}
	if srv.ran(cmdT3Stop) {
		t.Error("T3 must not pause when there's nowhere to save")
	}
}

func TestBackupNeedsConfirmationWithoutTerminal(t *testing.T) {
	ctx := testEnv(t)
	srv := movable("box")

	err := Backup(ctx, srv, filepath.Join(t.TempDir(), "box.tar.gz"), false)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("want a confirm error, got %v", err)
	}
	if srv.ran(cmdT3Stop) {
		t.Error("T3 must not pause before the backup is confirmed")
	}
}

func TestBackupDoesntRestartStoppedT3(t *testing.T) {
	ctx := testEnv(t)
	srv := movable("retired").on(cmdT3Running, "", errors.New("inactive"))

	if err := Backup(ctx, srv, filepath.Join(t.TempDir(), "box.tar.gz"), false); err != nil {
		t.Fatal(err) // nothing to pause, so nothing to confirm
	}
	if srv.ran(cmdT3Start) {
		t.Error("T3 that wasn't running must not be started")
	}
}

func TestSwapResizesLater(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("box", DevUser).on(memoryCmd, "ram 4\nswapkb 4194300\nfile 4\ndisk 20\n", nil)

	if err := Swap(ctx, srv, "8"); err != nil {
		t.Fatal(err)
	}
	if !srv.ran("dev: sudo bash -c") || !srv.ran("swapoff "+swapFile) || !addsSwap(srv, 8) {
		t.Error("should turn the old swap off and make an 8 GB one, as dev with sudo")
	}
}

func TestSwapCountsTheFileItReplaces(t *testing.T) {
	ctx := testEnv(t)
	// 12 GB free plus the 4 GB file being replaced leaves room for 6 GB, not 2.
	srv := newFake("box", DevUser).on(memoryCmd, "ram 4\nswapkb 4194300\nfile 4\ndisk 12\n", nil)

	if err := Swap(ctx, srv, "6"); err != nil {
		t.Fatal(err)
	}
	if err := Swap(ctx, srv, "7"); err == nil || !strings.Contains(err.Error(), "doesn't fit") {
		t.Fatalf("want a doesn't-fit error, got %v", err)
	}
}

func TestSwapRemoves(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("box", DevUser).on(memoryCmd, "ram 4\nswapkb 4194300\nfile 4\ndisk 20\n", nil)

	if err := Swap(ctx, srv, "0"); err != nil {
		t.Fatal(err)
	}
	if !srv.ran("swapoff "+swapFile) || madeSwap(srv) {
		t.Error("0 should remove the swap file and make no new one")
	}
}

func TestSwapLeavesSameSizeAlone(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("box", DevUser).on(memoryCmd, "ram 4\nswapkb 4194300\nfile 4\ndisk 20\n", nil)

	if err := Swap(ctx, srv, "4"); err != nil {
		t.Fatal(err)
	}
	if srv.ran("sudo bash -c") {
		t.Error("nothing should change when the size is the same")
	}
}

func TestSwapNeedsSizeWithoutTerminal(t *testing.T) {
	ctx := testEnv(t)
	srv := roomy(newFake("box", DevUser))

	err := Swap(ctx, srv, "")
	if err == nil || !strings.Contains(err.Error(), "outpost swap box GB") {
		t.Fatalf("want a give-the-size error, got %v", err)
	}
}

func TestSetupNamesTheServerFirst(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("fresh", "root").healthy()

	if err := Setup(ctx, srv, AccessUnset, "", "devbox", GatewayOptions{}); err != nil {
		t.Fatal(err)
	}
	// T3 Connect keeps the name it first sees, so the rename comes before the install and logins.
	named, installed := -1, -1
	for i, c := range srv.cmds {
		if named < 0 && strings.Contains(c, "hostnamectl set-hostname devbox") {
			named = i
		}
		if installed < 0 && strings.Contains(c, "install script") {
			installed = i
		}
	}
	if named < 0 || installed < 0 || named > installed {
		t.Errorf("should rename to devbox before installing (rename at %d, install at %d)", named, installed)
	}
}

func TestSetupRefusesABadName(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("fresh", "root").healthy()

	if err := Setup(ctx, srv, AccessUnset, "", "My Server", GatewayOptions{}); err == nil || !strings.Contains(err.Error(), "can't be a server name") {
		t.Fatalf("want a bad-name error, got %v", err)
	}
	if len(srv.cmds) > 0 {
		t.Error("nothing should run on the server")
	}
}

func TestSetupRerunKeepsTheName(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("busy", DevUser).healthy()
	asked := answer(t)

	if err := Setup(ctx, srv, AccessUnset, "", "", GatewayOptions{}); err != nil {
		t.Fatal(err)
	}
	if srv.ran("hostnamectl") || slices.Contains(asked.asked, "What should this server be called?") {
		t.Error("a set-up server keeps its name unless --name is given")
	}
}

func TestPairOpensLinkOnThisComputer(t *testing.T) {
	ctx := testEnv(t)
	answer(t, "here")
	srv := newFake("box", DevUser).on(tailscalePair, "Pairing URL: https://box/pair#token=X\nApp link: t3code://pair?x=1\nToken: X\n", nil)
	var opened string
	saved := openURL
	openURL = func(_ context.Context, url string) error { opened = url; return nil }
	t.Cleanup(func() { openURL = saved })

	if err := Pair(ctx, srv); err != nil {
		t.Fatal(err)
	}
	if opened != "t3code://pair?x=1" {
		t.Errorf("should open the app link here, opened %q", opened)
	}
	if srv.ran("(interactive)") {
		t.Error("must not also show a QR code: the code works once")
	}
}
