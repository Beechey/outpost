package setup

import (
	"errors"
	"strings"
	"testing"

	"github.com/Beechey/outpost/internal/ui"
)

// The commands that change a server after setup: access, login, update, status.

// onConnect is a set-up server whose devices come in through T3 Connect.
func onConnect(name string) *fakeHost {
	return newFake(name, DevUser).healthy().on(t3Logins[AccessTailscale].Check, "", errors.New("connect"))
}

func TestAccessSwitchesToTailscale(t *testing.T) {
	ctx := testEnv(t)
	srv := onConnect("box")

	if err := SetAccess(ctx, srv, AccessTailscale); err != nil {
		t.Fatal(err)
	}
	if !srv.ran("t3 up --lan --no-connect") {
		t.Error("should serve T3 on the tailnet only")
	}
	if srv.ran("install script") || srv.ran("systemctl reboot") {
		t.Error("switching access must not reinstall or reboot")
	}
}

func TestAccessLeavesSameChoiceAlone(t *testing.T) {
	ctx := testEnv(t)
	srv := onConnect("box")

	if err := SetAccess(ctx, srv, AccessConnect); err != nil {
		t.Fatal(err)
	}
	if srv.ran("t3 up") {
		t.Error("nothing should change when the choice is the same")
	}
}

func TestAccessNeedsChoiceWithoutTerminal(t *testing.T) {
	ctx := testEnv(t)
	err := SetAccess(ctx, onConnect("box"), AccessUnset)
	if err == nil || !strings.Contains(err.Error(), "outpost access box connect") {
		t.Fatalf("want a name-it error, got %v", err)
	}
}

func TestCommandsRefuseRetiredServer(t *testing.T) {
	ctx := testEnv(t)
	for name, run := range map[string]func(*fakeHost) error{
		"access": func(f *fakeHost) error { return SetAccess(ctx, f, AccessTailscale) },
		"login":  func(f *fakeHost) error { return Login(ctx, f, "t3") },
		"update": func(f *fakeHost) error { return Update(ctx, f, true) },
		"swap":   func(f *fakeHost) error { return Swap(ctx, f, "8") },
		"pair":   func(f *fakeHost) error { return Pair(ctx, f) },
	} {
		srv := newFake("old", DevUser).on(cmdMovedTo, "new\n", nil)
		if err := run(srv); err == nil || !strings.Contains(err.Error(), "retired") {
			t.Errorf("%s: want a retired error, got %v", name, err)
		}
		if srv.ran("t3 up") || srv.ran("t3 update") || srv.ran("t3 pair") || madeSwap(srv) {
			t.Errorf("%s: T3 must not come back on a retired server", name)
		}
	}
}

func TestCommandsNeedASetUpServer(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("fresh", "root") // no dev user yet
	if err := Status(ctx, srv); err == nil || !strings.Contains(err.Error(), "outpost setup fresh") {
		t.Fatalf("want a set-it-up-first error, got %v", err)
	}
}

func TestLoginNamedAccountSignsInAgain(t *testing.T) {
	ctx := testEnv(t)
	srv := onConnect("box") // every check passes: signed in everywhere

	if err := Login(ctx, srv, "Claude"); err != nil {
		t.Fatal(err)
	}
	if !srv.ran("(interactive): " + userPath + "claude auth login") {
		t.Error("a named login should run even when it looks signed in")
	}
	if srv.ran("gh auth login") {
		t.Error("only the named account should sign in")
	}
}

func TestLoginWithoutNameFillsGaps(t *testing.T) {
	ctx := testEnv(t)
	srv := onConnect("box").on(loginNamed("GitHub").Check, "", errors.New("logged out"))

	if err := Login(ctx, srv, ""); err != nil {
		t.Fatal(err)
	}
	if !srv.ran("gh auth login") || srv.ran("claude auth login") {
		t.Error("should sign in to GitHub only, the one that's missing")
	}
}

func TestLoginUnknownAccount(t *testing.T) {
	ctx := testEnv(t)
	err := Login(ctx, onConnect("box"), "gitlab")
	if err == nil || !strings.Contains(err.Error(), "tailscale, claude, github or t3") {
		t.Fatalf("want an unknown-account error, got %v", err)
	}
}

func TestUpdateRunsEachUpdate(t *testing.T) {
	ctx := testEnv(t)
	srv := onConnect("box")

	if err := Update(ctx, srv, true); err != nil {
		t.Fatal(err)
	}
	for _, u := range updates {
		if !srv.ran(u.cmd) {
			t.Errorf("should update %s", u.name)
		}
	}
}

func TestUpdateNeedsConfirmationWithoutTerminal(t *testing.T) {
	ctx := testEnv(t)
	srv := onConnect("box")

	err := Update(ctx, srv, false)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("want a confirm error, got %v", err)
	}
	if srv.ran("t3 update") {
		t.Error("nothing should update before it's confirmed")
	}
}

func TestStatusChangesNothing(t *testing.T) {
	ctx := testEnv(t)
	srv := onConnect("box").on(memoryCmd, "ram 4\nswapkb 0\nfile 0\ndisk 50\n", nil).
		on("claude auth status", "", errors.New("logged out"))

	if err := Status(ctx, srv); err != nil {
		t.Fatal(err)
	}
	for _, c := range srv.cmds {
		if strings.Contains(c, "interactive") || strings.Contains(c, "sudo") || strings.Contains(c, "t3 up") {
			t.Errorf("status must only look, but ran: %s", c)
		}
	}
}

// The questions outpost asks, answered.

func TestSetupAsksForSwap(t *testing.T) {
	ctx := testEnv(t)
	srv := roomy(newFake("fresh", "root").healthy())
	answer(t, "", "6") // keep the name, then 6 GB of swap

	if err := Setup(ctx, srv, AccessUnset, "", ""); err != nil {
		t.Fatal(err)
	}
	if !addsSwap(srv, 6) {
		t.Error("should add the 6 GB the user typed")
	}
}

func TestSwapAnswerMustFit(t *testing.T) {
	ctx := testEnv(t)
	srv := roomy(newFake("box", DevUser)) // 50 GB free: room for 40
	answer(t, "45")

	if err := Swap(ctx, srv, ""); err == nil || !strings.Contains(err.Error(), "doesn't fit") {
		t.Fatalf("want a doesn't-fit error, got %v", err)
	}
	if madeSwap(srv) {
		t.Error("nothing should change")
	}
}

func TestAccessAsksWhichWay(t *testing.T) {
	ctx := testEnv(t)
	srv := onConnect("box")
	answer(t, "tailscale")

	if err := SetAccess(ctx, srv, AccessUnset); err != nil {
		t.Fatal(err)
	}
	if !srv.ran(t3Logins[AccessTailscale].Run) {
		t.Error("should switch to the chosen way in")
	}
}

func TestMoveConfirmedAtPrompt(t *testing.T) {
	ctx := testEnv(t)
	old, dst := movable("old"), newFake("new", "root").healthy()
	answer(t, "yes", "", "0") // move, keep the name, then no swap

	if err := Move(ctx, old, dst, false, AccessUnset, "", ""); err != nil {
		t.Fatal(err)
	}
	if !old.ran("copy to new") {
		t.Error("a confirmed move should copy")
	}
}

func TestMoveAsksForAName(t *testing.T) {
	ctx := testEnv(t)
	old, dst := movable("old"), newFake("new", "root").healthy()
	answer(t, "yes", "tars", "0") // move, name it tars, no swap

	if err := Move(ctx, old, dst, false, AccessUnset, "", ""); err != nil {
		t.Fatal(err)
	}
	if !dst.ran("hostnamectl set-hostname tars") {
		t.Error("the new server should be named tars")
	}
}

func TestCtrlCAtAQuestionChangesNothing(t *testing.T) {
	ctx := testEnv(t)
	old, dst := movable("old"), newFake("new", "root").healthy()
	answer(t, "^C")

	err := Move(ctx, old, dst, false, AccessUnset, "", "")
	if !errors.Is(err, ui.ErrCancelled) {
		t.Fatalf("want cancelled, got %v", err)
	}
	if old.ran(cmdT3Stop) || dst.ran("install script") {
		t.Error("nothing should change after Ctrl-C")
	}
}

func TestUpdateDeclined(t *testing.T) {
	ctx := testEnv(t)
	srv := onConnect("box")
	answer(t, "no")

	if err := Update(ctx, srv, false); !errors.Is(err, ui.ErrCancelled) {
		t.Fatalf("want cancelled, got %v", err)
	}
	if srv.ran("t3 update") {
		t.Error("nothing should update when declined")
	}
}

// What status finds.

func TestInspectReportsProblems(t *testing.T) {
	ctx := testEnv(t)
	srv := onConnect("box").on(memoryCmd, "ram 8\nswapkb 4194300\nfile 4\ndisk 50\n", nil).
		on(loginNamed("Claude").Check, "", errors.New("logged out")).
		on(cmdServices, "docker active\ntailscaled active\nT3 failed\n", nil)
	d, err := open(ctx, srv)
	if err != nil {
		t.Fatal(err)
	}

	r, err := inspect(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if r.mem.ram != 8 || r.mem.swap != 4 || !r.reachable || r.access != AccessConnect {
		t.Errorf("memory and access: %+v", r)
	}
	if len(r.down) != 1 || r.down[0] != "T3 is failed" {
		t.Errorf("down = %v, want T3 failed", r.down)
	}
	if len(r.loggedOut) != 1 || r.loggedOut[0].Name != "Claude" {
		t.Errorf("logged out = %v, want Claude", r.loggedOut)
	}
	if !r.rebootPending { // the fake says yes to anything it isn't told about
		t.Error("want the pending reboot")
	}
}

func TestInspectRetiredServer(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("old", DevUser).on(cmdMovedTo, "new\n", nil)
	d, err := open(ctx, srv)
	if err != nil {
		t.Fatal(err)
	}

	r, err := inspect(ctx, d)
	if err != nil || r.movedTo != "new" {
		t.Fatalf("want a retired report, got %+v, %v", r, err)
	}
	if srv.ran(memoryCmd) {
		t.Error("a retired server only reports where its work went")
	}
}

func TestParseAccess(t *testing.T) {
	for in, want := range map[string]Access{"": AccessUnset, "connect": AccessConnect, "tailscale": AccessTailscale} {
		if got, err := ParseAccess(in); err != nil || got != want {
			t.Errorf("%q: got %q, %v", in, got, err)
		}
	}
	if _, err := ParseAccess("vpn"); err == nil {
		t.Error("want an error for an unknown way in")
	}
}

func TestRenameASetUpServer(t *testing.T) {
	ctx := testEnv(t)
	srv := newFake("box", DevUser)

	if err := Rename(ctx, srv, "tars"); err != nil {
		t.Fatal(err)
	}
	if !srv.ran("dev: sudo bash -c") || !srv.ran("hostnamectl set-hostname tars") || !srv.ran("tailscale set --hostname=tars") {
		t.Error("should rename the server and Tailscale, as dev with sudo")
	}
}
