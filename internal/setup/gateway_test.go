package setup

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Beechey/outpost/internal/tailnet"
)

// The gateway's model: routes, settings, names and what they render to.

func TestNewRoutes(t *testing.T) {
	rs, err := NewRoutes("myapp", "3001", []string{"/stream=3002"}, []string{"files.myapp=8333"})
	if err != nil {
		t.Fatal(err)
	}
	want := []route{
		{Project: "myapp", Name: "myapp", Port: 3001, Paths: []pathRoute{{"/stream", 3002}}},
		{Project: "myapp", Name: "files.myapp", Port: 8333},
	}
	if len(rs) != 2 || rs[0].Name != want[0].Name || rs[0].Paths[0] != want[0].Paths[0] || rs[1].Name != want[1].Name || rs[1].Port != 8333 {
		t.Errorf("got %+v", rs)
	}
	// What reaches Caddy, Tailscale and the shell is checked here.
	for _, bad := range [][]string{
		{"Myapp", "3001"}, {"sig net", "3001"}, {"myapp", "0"}, {"myapp", "70000"}, {"myapp", "3001", "stream=1"},
		{"myapp", "3001", "/a b=1"}, {"myapp", "3001", "/x;rm=1"}, {"myapp", "3001", "", "files'=1"}, {"-x", "1"},
	} {
		var paths, names []string
		if len(bad) > 2 && bad[2] != "" {
			paths = []string{bad[2]}
		}
		if len(bad) > 3 {
			names = []string{bad[3]}
		}
		if _, err := NewRoutes(bad[0], bad[1], paths, names); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestRoutesFileRoundTrip(t *testing.T) {
	rs, _ := NewRoutes("myapp", "3001", []string{"/stream=3002", "/api/v1=4000"}, []string{"files.myapp=8333"})
	got := parseRoutes(formatRoutes(rs) + "junk line\nbad name 1; rm -rf /\n")
	if len(got) != 2 || got[0].Paths[1].Path != "/api/v1" || got[1].Name != "files.myapp" {
		t.Errorf("round trip lost something, or let bad lines in: %+v", got)
	}
}

func TestGatewayEnvRoundTrip(t *testing.T) {
	c := gatewayConfig{Mode: GatewayDomain, Domain: "devbox.test", Suffix: "devbox", Service: "svc:gateway-devbox", Address: "100.64.0.9", IP: "100.100.1.1"}
	if got := parseGatewayEnv(c.env()); got != c {
		t.Errorf("got %+v", got)
	}
	if got := parseGatewayEnv("MODE=domain\nGATEWAY_DOMAIN=$(reboot)\n"); got.Domain != "" {
		t.Error("unsafe values must be dropped")
	}
	if got := parseGatewayEnv("MODE=sideways\n"); got.Mode != GatewayUnset {
		t.Error("an unknown mode reads as unset")
	}
}

func TestNames(t *testing.T) {
	ts := gatewayConfig{Mode: GatewayTailscale, Suffix: "devbox", Tailnet: "tail1234.ts.net"}
	dom := gatewayConfig{Mode: GatewayDomain, Domain: "devbox.test"}
	files := route{Name: "files.myapp"}
	if got := ts.url(files); got != "https://files-myapp-devbox.tail1234.ts.net" {
		t.Errorf("Tailscale name: %s", got)
	}
	if got := dom.url(files); got != "https://files.myapp.devbox.test" {
		t.Errorf("domain name: %s", got)
	}
	long := []route{{Name: strings.Repeat("a", 60)}}
	if err := checkServiceNames(long, "devbox"); err == nil {
		t.Error("a Service name over 63 characters should be refused")
	}
}

func TestCheckDomain(t *testing.T) {
	for d, public := range map[string]bool{"devbox.test": false, "dev.internal": false, "box.home.arpa": false, "dev.example.com": true} {
		got, err := checkDomain(d)
		if err != nil || got != public {
			t.Errorf("%s: public=%v err=%v", d, got, err)
		}
	}
	for _, bad := range []string{"devbox.local", "local", "test", "DEVBOX TEST", "a..b", "$(x).test"} {
		if _, err := checkDomain(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestCaddySite(t *testing.T) {
	rs, _ := NewRoutes("myapp", "3001", []string{"/stream=3002"}, []string{"files.myapp=8333"})
	site := caddySite(rs[0])
	for _, want := range []string{"myapp.{$GATEWAY_DOMAIN} {", "@path0 path /stream /stream/*", "reverse_proxy 127.0.0.1:3002", "handle {\n\t\treverse_proxy 127.0.0.1:3001"} {
		if !strings.Contains(site, want) {
			t.Errorf("site lacks %q:\n%s", want, site)
		}
	}
	if s := caddySite(rs[1]); !strings.Contains(s, "files.myapp.{$GATEWAY_DOMAIN} {\n\treverse_proxy 127.0.0.1:8333\n}") {
		t.Errorf("plain site:\n%s", s)
	}
}

func TestServeCmdKeepsPath(t *testing.T) {
	rs, _ := NewRoutes("myapp", "3001", []string{"/stream=3002"}, nil)
	cmd := serveCmd(gatewayConfig{Mode: GatewayTailscale, Suffix: "devbox"}, rs[0])
	for _, want := range []string{
		"--service=svc:myapp-devbox --https=443 --yes http://127.0.0.1:3001",
		// Tailscale strips the path before passing it on; the target puts it back.
		"--set-path=/stream --yes http://127.0.0.1:3002/stream",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("serve command lacks %q: %s", want, cmd)
		}
	}
}

func testRoot(t *testing.T) []byte {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "outpost devbox.test"},
		NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		PermittedDNSDomains: []string{"devbox.test"}, PermittedDNSDomainsCritical: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestPageFiles(t *testing.T) {
	c := gatewayConfig{Mode: GatewayDomain, Domain: "devbox.test"}
	rs, _ := NewRoutes("myapp", "3001", nil, []string{"files.myapp=8333"})
	files, err := pageFiles("devbox", "203.0.113.7", c, rs, testRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	profile := files[gatewayDir+"/www/outpost.mobileconfig"]
	if !strings.Contains(profile, "com.apple.security.root") || !strings.Contains(profile, "<data>MII") {
		t.Errorf("profile should carry the root:\n%s", profile)
	}
	html := files[gatewayDir+"/www/index.html"]
	if !strings.Contains(html, "https://myapp.devbox.test") || !strings.Contains(html, "on devbox") {
		t.Error("the page should name the server and link each project")
	}
	if !strings.Contains(html, "outpost gateway trust 203.0.113.7") {
		t.Error("commands should use the address outpost reaches the server at")
	}
	if strings.Count(html, `class="name"`) != 1 || !strings.Contains(html, "Also uses <code>files.myapp.devbox.test</code>") {
		t.Error("extra names belong beside their project, not as projects of their own")
	}
	if _, err := mobileconfig("devbox.test", []byte("nope")); err == nil {
		t.Error("want an error for a non-PEM root")
	}
}

func TestWriteFilesCmdQuotes(t *testing.T) {
	cmd := writeFilesCmd(map[string]string{".outpost/gateway/x": "it's $(fine)"})
	if !strings.Contains(cmd, `'it'\''s $(fine)'`) || !strings.Contains(cmd, "mkdir -p '.outpost/gateway'") {
		t.Errorf("content must reach the file as is: %s", cmd)
	}
}

// The gateway's flows, against a fake server and a fake tailnet admin.

// fakeAdmin records what the tailnet was asked for.
type fakeAdmin struct {
	prepared, finished []tailnet.Want
	forgot             []string
}

func (f *fakeAdmin) prepare(_ context.Context, w tailnet.Want) (map[string]string, error) {
	f.prepared = append(f.prepared, w)
	addrs := map[string]string{}
	for s := range w.Services {
		addrs[s] = "100.64.0.9"
	}
	return addrs, nil
}

func (f *fakeAdmin) finish(_ context.Context, w tailnet.Want) error {
	f.finished = append(f.finished, w)
	return nil
}

func (f *fakeAdmin) forget(_ context.Context, services, domains []string) error {
	f.forgot = append(f.forgot, append(services, domains...)...)
	return nil
}

func withAdmin(t *testing.T) *fakeAdmin {
	t.Helper()
	f := &fakeAdmin{}
	saved := pickAdmin
	pickAdmin = func(context.Context, dev, gatewayConfig, bool, bool) (tailnetAdmin, error) { return f, nil }
	t.Cleanup(func() { pickAdmin = saved })
	return f
}

const selfJSON = `{"id":"node1","dns":"devbox.tail1234.ts.net","ip":"100.100.1.1","tailnet":"tail1234.ts.net","tags":[],"https":true,"magicdns":true}`

// onTailnet is a set-up server signed in to a tailnet with MagicDNS and HTTPS on.
func onTailnet(name string) *fakeHost {
	return onConnect(name).on(cmdTailscaleSelf, selfJSON, nil).on("hostname -s", "devbox\n", nil)
}

func withGateway(f *fakeHost, c gatewayConfig, rs []route) *fakeHost {
	return f.on(cmdReadGateway, c.env()+"::routes\n"+formatRoutes(rs), nil)
}

var myapp, _ = NewRoutes("myapp", "3001", []string{"/stream=3002"}, []string{"files.myapp=8333"})

func TestGatewayTailscaleOn(t *testing.T) {
	ctx := testEnv(t)
	adm := withAdmin(t)
	srv := onTailnet("box")
	if err := Gateway(ctx, srv, GatewayOptions{Mode: GatewayTailscale}); err != nil {
		t.Fatal(err)
	}
	if !srv.ran("MODE=tailscale") || !srv.ran("SUFFIX=devbox") || !srv.ran("TAILNET=tail1234.ts.net") {
		t.Error("settings should be saved")
	}
	if len(adm.prepared) != 1 || adm.prepared[0].Device != "node1" || len(adm.finished) != 1 {
		t.Errorf("the tailnet should be prepared for this device, then finished: %+v", adm)
	}
	if srv.ran("install script") {
		t.Error("Tailscale names install nothing on the server")
	}
}

func TestGatewayAddTailscale(t *testing.T) {
	ctx := testEnv(t)
	adm := withAdmin(t)
	c := gatewayConfig{Mode: GatewayTailscale, Suffix: "devbox", Tailnet: "tail1234.ts.net"}
	srv := withGateway(onTailnet("box"), c, nil)

	if err := GatewayAdd(ctx, srv, "myapp", "3001", []string{"/stream=3002"}, []string{"files.myapp=8333"}, true); err != nil {
		t.Fatal(err)
	}
	w := adm.prepared[0]
	if len(w.Services) != 2 || w.Services["svc:myapp-devbox"] == nil || w.Services["svc:files-myapp-devbox"] == nil {
		t.Errorf("want a Service per name: %v", w.Services)
	}
	if !srv.ran("--service=svc:myapp-devbox --https=443 --set-path=/stream --yes http://127.0.0.1:3002/stream") {
		t.Error("the path should be published with its prefix kept")
	}
	if !srv.ran("myapp myapp 3001 /stream=3002") {
		t.Error("the routes should be saved")
	}
	if !srv.ran("curl") {
		t.Error("each new name should be checked, which also warms its certificate")
	}
}

func TestGatewayAddRefusesTakenName(t *testing.T) {
	ctx := testEnv(t)
	withAdmin(t)
	c := gatewayConfig{Mode: GatewayTailscale, Suffix: "devbox", Tailnet: "tail1234.ts.net"}
	srv := withGateway(onTailnet("box"), c, myapp)
	err := GatewayAdd(ctx, srv, "other", "4000", nil, []string{"files.myapp=9000"}, true)
	if err == nil || !strings.Contains(err.Error(), "already a name of myapp") {
		t.Fatalf("want a taken-name error, got %v", err)
	}
}

func TestGatewayDomainOn(t *testing.T) {
	ctx := testEnv(t)
	adm := withAdmin(t)
	srv := onTailnet("box").on("GATEWAY_DOMAIN='devbox.test'; "+cmdGatewayRootPEM, string(testRoot(t)), nil)
	if err := Gateway(ctx, srv, GatewayOptions{Mode: GatewayDomain, Domain: "devbox.test", Yes: true}); err != nil {
		t.Fatal(err)
	}
	if !srv.ran("install script") {
		t.Error("the domain gateway installs Caddy and CoreDNS")
	}
	for _, want := range []string{"GATEWAY_DOMAIN=devbox.test", "GATEWAY_ADDRESS=100.64.0.9", "TAILSCALE_IP=100.100.1.1", "SERVICE=svc:gateway-devbox",
		"--service=svc:gateway-devbox --tcp=443", "outpost.mobileconfig"} {
		if !srv.ran(want) {
			t.Errorf("want %q to reach the server", want)
		}
	}
	if w := adm.finished[0]; w.SplitDNS["devbox.test"] != "100.100.1.1" {
		t.Errorf("split DNS should send devbox.test to the server: %v", w.SplitDNS)
	}
}

func TestGatewayRefusesLocal(t *testing.T) {
	ctx := testEnv(t)
	withAdmin(t)
	err := Gateway(ctx, onTailnet("box"), GatewayOptions{Mode: GatewayDomain, Domain: "devbox.local", Yes: true})
	if err == nil || !strings.Contains(err.Error(), "mDNS") {
		t.Fatalf("want a .local error, got %v", err)
	}
}

func TestGatewayTurnsOnHTTPSCertificates(t *testing.T) {
	ctx := testEnv(t)
	adm := withAdmin(t)
	srv := onTailnet("box").on(cmdTailscaleSelf, strings.Replace(selfJSON, `"https":true`, `"https":false`, 1), nil)
	err := Gateway(ctx, srv, GatewayOptions{Mode: GatewayTailscale})
	if len(adm.prepared) != 1 || !adm.prepared[0].HTTPS || adm.prepared[0].MagicDNS {
		t.Errorf("HTTPS certificates, and only they, should be turned on: %+v", adm.prepared)
	}
	// The fake server never hears of it, so outpost gives up, saying where to look.
	if err == nil || !strings.Contains(err.Error(), "admin/dns") {
		t.Fatalf("want a still-off error, got %v", err)
	}
}

func TestGatewayWithoutKeyNeedsFeaturesOn(t *testing.T) {
	ctx := testEnv(t)
	saved := pickAdmin
	pickAdmin = func(_ context.Context, _ dev, c gatewayConfig, _, _ bool) (tailnetAdmin, error) {
		return noAdmin{c}, nil
	}
	t.Cleanup(func() { pickAdmin = saved })
	srv := onTailnet("box").on(cmdTailscaleSelf, strings.Replace(selfJSON, `"magicdns":true`, `"magicdns":false`, 1), nil)
	err := Gateway(ctx, srv, GatewayOptions{Mode: GatewayTailscale})
	if err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("want a turn-on-or-give-a-key error, got %v", err)
	}
}

func TestGatewayOffTearsDown(t *testing.T) {
	ctx := testEnv(t)
	adm := withAdmin(t)
	c := gatewayConfig{Mode: GatewayTailscale, Suffix: "devbox", Tailnet: "tail1234.ts.net"}
	srv := withGateway(onTailnet("box"), c, myapp)
	if err := Gateway(ctx, srv, GatewayOptions{Mode: GatewayOff, Yes: true}); err != nil {
		t.Fatal(err)
	}
	if !srv.ran("tailscale serve clear svc:myapp-devbox") || !slices.Contains(adm.forgot, "svc:files-myapp-devbox") {
		t.Errorf("each Service should stop and be deleted: %v", adm.forgot)
	}
	if !srv.ran("MODE=off") || !srv.ran("myapp files.myapp 8333") {
		t.Error("off should be saved, keeping the routes for later")
	}
}

func TestGatewaySwitchToDomainTakesDownTailscaleNames(t *testing.T) {
	ctx := testEnv(t)
	adm := withAdmin(t)
	c := gatewayConfig{Mode: GatewayTailscale, Suffix: "devbox", Tailnet: "tail1234.ts.net"}
	srv := withGateway(onTailnet("box"), c, myapp).on("GATEWAY_DOMAIN='devbox.test'; "+cmdGatewayRootPEM, string(testRoot(t)), nil)
	if err := Gateway(ctx, srv, GatewayOptions{Mode: GatewayDomain, Domain: "devbox.test", Yes: true}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(adm.forgot, "svc:myapp-devbox") {
		t.Error("the old Tailscale names should go")
	}
	if !srv.ran("sites/myapp.caddy") {
		t.Error("the projects should carry over to the domain")
	}
}

func TestGatewayRemoveDomain(t *testing.T) {
	ctx := testEnv(t)
	withAdmin(t)
	c := gatewayConfig{Mode: GatewayDomain, Domain: "devbox.test", Suffix: "devbox", Service: "svc:gateway-devbox", Address: "100.64.0.9", IP: "100.100.1.1"}
	srv := withGateway(onTailnet("box"), c, myapp).on("GATEWAY_DOMAIN='devbox.test'; "+cmdGatewayRootPEM, string(testRoot(t)), nil)
	if err := GatewayRemove(ctx, srv, "myapp", true); err != nil {
		t.Fatal(err)
	}
	if !srv.ran(cmdClearSites) || !srv.ran("systemctl reload outpost-caddy") {
		t.Error("the site should go and Caddy reload")
	}
	if srv.ran("myapp myapp 3001") {
		t.Error("the routes should no longer include the project")
	}
}

func TestGatewayRemoveUnknownProject(t *testing.T) {
	ctx := testEnv(t)
	c := gatewayConfig{Mode: GatewayTailscale, Suffix: "devbox", Tailnet: "tail1234.ts.net"}
	err := GatewayRemove(ctx, withGateway(onTailnet("box"), c, myapp), "nope", true)
	if err == nil || !strings.Contains(err.Error(), "myapp") {
		t.Fatalf("want an error naming the projects, got %v", err)
	}
}

func TestSetupAsksGatewayTree(t *testing.T) {
	ctx := testEnv(t)
	adm := withAdmin(t)
	s := answer(t, "yes", string(GatewayTailscale))
	srv := onTailnet("box")
	d, _ := open(ctx, srv)
	if err := gatewayStep(ctx, d, AccessTailscale, GatewayOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(s.asked) != 2 || !strings.Contains(s.asked[0], "other devices") || !strings.Contains(s.asked[1], "Which names") {
		t.Errorf("want the two questions of the tree, got %q", s.asked)
	}
	if len(adm.prepared) != 1 {
		t.Error("the gateway should be set up")
	}
}

func TestSetupGatewayNoTurnsItOff(t *testing.T) {
	ctx := testEnv(t)
	withAdmin(t)
	answer(t, "no")
	srv := onTailnet("box")
	d, _ := open(ctx, srv)
	if err := gatewayStep(ctx, d, AccessConnect, GatewayOptions{}); err != nil {
		t.Fatal(err)
	}
	if !srv.ran("MODE=off") {
		t.Error("no should be remembered, so setup doesn't ask again")
	}
}

func TestSetupRepairsGateway(t *testing.T) {
	ctx := testEnv(t)
	adm := withAdmin(t)
	s := answer(t)
	c := gatewayConfig{Mode: GatewayTailscale, Suffix: "devbox", Tailnet: "tail1234.ts.net"}
	srv := withGateway(onTailnet("box"), c, myapp)
	d, _ := open(ctx, srv)
	if err := gatewayStep(ctx, d, AccessTailscale, GatewayOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(s.asked) != 0 {
		t.Errorf("a set-up gateway is checked, not asked about: %q", s.asked)
	}
	if !srv.ran("--service=svc:myapp-devbox") || len(adm.forgot) != 0 {
		t.Error("a repair publishes again without taking anything down")
	}
}

func TestMoveGatewayHandsOver(t *testing.T) {
	ctx := testEnv(t)
	adm := withAdmin(t)
	c := gatewayConfig{Mode: GatewayTailscale, Suffix: "devbox", Tailnet: "tail1234.ts.net"}
	old := withGateway(onTailnet("old"), c, myapp)
	dst := withGateway(onTailnet("new"), c, myapp)
	from, _ := open(ctx, old)
	to, _ := open(ctx, dst)
	if err := moveGateway(ctx, from, to, true); err != nil {
		t.Fatal(err)
	}
	if !old.ran("tailscale serve clear svc:myapp-devbox") || old.ran("--https=443") {
		t.Error("the old server should stop publishing, and not publish again")
	}
	if !dst.ran("--service=svc:myapp-devbox --https=443") {
		t.Error("the new server should take the names over")
	}
	if len(adm.forgot) != 0 {
		t.Error("the Services carry over; nothing is deleted")
	}
}

func TestGatewayStatusFindsProblems(t *testing.T) {
	ctx := testEnv(t)
	c := gatewayConfig{Mode: GatewayDomain, Domain: "devbox.test", Service: "svc:gateway-devbox"}
	srv := withGateway(onTailnet("box"), c, myapp).
		on(cmdGatewayUnits, "outpost-caddy active\noutpost-dns failed\n", nil).
		on(cmdGatewayServed, "", nil)
	d, _ := open(ctx, srv)
	r, err := inspect(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.gatewayDown, ";") != "outpost-dns is failed;svc:gateway-devbox isn't served" {
		t.Errorf("gateway problems: %v", r.gatewayDown)
	}
}

func TestDomainServeCmdPorts(t *testing.T) {
	cmd := domainServeCmd(gatewayConfig{Service: "svc:gateway-devbox"})
	for _, want := range []string{"--tcp=443 --yes tcp://127.0.0.1:44343", "--tcp=80 --yes tcp://127.0.0.1:44380"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("want %q in %s", want, cmd)
		}
	}
}

func TestDomainTreeAsksToTrustHere(t *testing.T) {
	ctx := testEnv(t)
	withAdmin(t)
	var ran []string
	saved := runLocal
	runLocal = func(_ context.Context, name string, args ...string) error {
		ran = append(ran, name+" "+strings.Join(args, " "))
		return nil
	}
	t.Cleanup(func() { runLocal = saved })
	srv := onTailnet("box").on("GATEWAY_DOMAIN='devbox.test'; "+cmdGatewayRootPEM, string(testRoot(t)), nil)
	d, _ := open(ctx, srv)

	s := answer(t, "yes", string(GatewayDomain), "devbox.test", "no")
	if err := gatewayStep(ctx, d, AccessTailscale, GatewayOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(s.asked) != 4 || !strings.Contains(s.asked[3], "Trust devbox.test on this computer") {
		t.Fatalf("trusting it here should be the tree's last question, got %q", s.asked)
	}
	if len(ran) != 0 {
		t.Errorf("no means nothing changes on this computer, but ran %q", ran)
	}

	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return
	}
	srv2 := onTailnet("box").on("GATEWAY_DOMAIN='devbox.test'; "+cmdGatewayRootPEM, string(testRoot(t)), nil)
	d2, _ := open(ctx, srv2)
	answer(t, "yes", string(GatewayDomain), "devbox.test", "yes")
	if err := gatewayStep(ctx, d2, AccessTailscale, GatewayOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || !strings.HasPrefix(ran[0], "sudo ") {
		t.Errorf("yes should add the certificate with sudo, got %q", ran)
	}
}

func TestUseSystemCertificatesOnce(t *testing.T) {
	dir := t.TempDir()
	p := firefoxProfile{"Zen", dir}
	if err := os.WriteFile(filepath.Join(dir, "user.js"), []byte(`user_pref("a", 1);`), 0o644); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := useSystemCertificates(p); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(filepath.Join(dir, "user.js"))
	if strings.Count(string(b), enterpriseRoots) != 1 || !strings.HasPrefix(string(b), `user_pref("a", 1);`+"\n") {
		t.Errorf("want the setting once, after what was there:\n%s", b)
	}
	if err := os.WriteFile(filepath.Join(dir, "user.js"), []byte(`user_pref("security.enterprise_roots.enabled", false);`), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = useSystemCertificates(p)
	if b, _ := os.ReadFile(filepath.Join(dir, "user.js")); strings.Contains(string(b), "true") {
		t.Error("a choice already made in the profile must be left alone")
	}
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "prefs.js"), []byte(`user_pref("security.enterprise_roots.enabled", false);`), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = useSystemCertificates(firefoxProfile{"Zen", other})
	if _, err := os.Stat(filepath.Join(other, "user.js")); err == nil {
		t.Error("a choice made in about:config must be left alone too")
	}
}
