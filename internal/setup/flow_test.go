package setup

import (
	"errors"
	"strings"
	"testing"
)

const installedCheck = ".service.installed"

func TestSetupFreshServer(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("fresh", "root").healthy()

	if err := setup(ctx, srv); err != nil {
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

	if err := setup(ctx, srv); err != nil {
		t.Fatal(err)
	}
	if !srv.ran("ssh-copy-id root") {
		t.Error("should copy the key to root first")
	}
}

func TestSetupServerInUseSkipsReboot(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("busy", DevUser).healthy().on(installedCheck, "true", nil)

	if err := setup(ctx, srv); err != nil {
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
	srv := newFake("old", DevUser).healthy().on("cat "+movedToFile, "new:22\n", nil)

	err := setup(ctx, srv)
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
	srv := newFake("broken", DevUser).on(installedCheck, "true", nil).
		on("is-active", "docker active\ntailscaled active\nT3 failed\n", nil)

	err := setup(ctx, srv)
	if err == nil || !strings.Contains(err.Error(), "T3 is failed") {
		t.Fatalf("want T3 named as not running, got %v", err)
	}
}

// movable is an old server with work to move.
func movable(name string) *fakeHost {
	return newFake(name, DevUser).on("for p in", "path code\npath .t3/userdata\nt3 yes\nprojects 2\nsessions 5\nsize 1.2G\n", nil)
}

func TestMoveCopiesAndRetiresOld(t *testing.T) {
	ctx := testEnv(t)
	old, dst := movable("old"), newFake("new", "root").healthy()

	if err := move(ctx, old, dst, true); err != nil {
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

	err := move(ctx, old, dst, true)
	if err == nil || !strings.Contains(err.Error(), "running on old again") {
		t.Fatalf("want a restarted-old error, got %v", err)
	}
	if !old.ran(service("start")) {
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
	old.on(service("start"), "", errors.New("unit failed"))

	err := move(ctx, old, dst, true)
	if err == nil || !strings.Contains(err.Error(), "still stopped") {
		t.Fatalf("want a still-stopped error, got %v", err)
	}
}

func TestMoveRerunDoesntCopyOrReboot(t *testing.T) {
	ctx := testEnv(t)
	old := movable("old").on("for p in", "t3 yes\nmoved new\n", nil)
	dst := newFake("new", DevUser).healthy().on(installedCheck, "true", nil)

	if err := move(ctx, old, dst, true); err != nil {
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
	old := movable("old").on("for p in", "t3 yes\nmoved other\n", nil)
	dst := newFake("new", "root").healthy()

	err := move(ctx, old, dst, true)
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

	err := move(ctx, old, dst, false) // tests have no terminal
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("want a confirm error, got %v", err)
	}
	if old.ran(service("stop")) {
		t.Error("T3 on the old server must not stop before the move is confirmed")
	}
}
