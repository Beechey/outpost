package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/tailnet"
	"github.com/Beechey/outpost/internal/ui"
)

// featuresTimeout is how long the server may take to hear MagicDNS and HTTPS
// certificates were turned on. Tests shorten it.
var featuresTimeout = time.Minute

// GatewayOptions are a gateway's answers given in advance, on the command line.
type GatewayOptions struct {
	Mode   GatewayMode // "" asks
	Domain string      // for GatewayDomain; "" asks, or keeps the current one
	Yes    bool        // don't ask: apply policy additions, confirm switches and warnings
	// Trust says whether this computer should trust a domain gateway's
	// certificate: "yes", "no", or "" to ask (or, with Yes, to leave it).
	Trust string
}

func (c gatewayConfig) active() bool { return c.Mode == GatewayTailscale || c.Mode == GatewayDomain }

// sameAs reports whether two settings give projects the same names.
func (c gatewayConfig) sameAs(o gatewayConfig) bool {
	return c.Mode == o.Mode && (c.Mode != GatewayDomain || c.Domain == o.Domain)
}

// Gateway turns a server's gateway on or off, or switches which names its
// projects get, asking when o doesn't say. Choosing what it has already
// checks it and puts right anything that's wrong.
func Gateway(ctx context.Context, h remote.Host, o GatewayOptions) error {
	d, err := openActive(ctx, h)
	if err != nil {
		return err
	}
	cur, rs := d.gateway(ctx)
	lipgloss.Println("  " + ui.Muted.Render("Projects on "+h.String()+": "+describeGateway(cur, rs)))
	access, _ := d.access(ctx)
	want, err := chooseGateway(ctx, d, cur, access, &o, false)
	if errors.Is(err, ui.ErrNoTerminal) {
		return fmt.Errorf("run in a terminal to choose, or name it: outpost gateway %s tailscale (or domain, or off)", h)
	}
	if err != nil {
		return err
	}
	if cur.active() && !want.sameAs(cur) && len(rs) > 0 && !o.Yes {
		if err := confirm(ctx, "Change the names of "+plural(fmt.Sprint(len(projects(rs))), "project")+"?",
			"They move from "+cur.url(rs[0])+" and the like to new names.\nBookmarks to the old names stop working.", "Change them"); err != nil {
			return err
		}
	}
	return runGateway(ctx, d, cur, want, rs, o)
}

// runGateway moves a server's gateway from cur to want, keeping its routes.
func runGateway(ctx context.Context, d dev, cur, want gatewayConfig, rs []route, o GatewayOptions) error {
	repair := want.sameAs(cur)
	if !want.active() {
		if cur.active() {
			adm, err := pickAdmin(ctx, d, cur, o.Yes, true)
			if err != nil {
				return err
			}
			if _, ok := adm.(noAdmin); ok {
				adm = manualAdmin{d} // without a key, say what to tidy up in the tailnet
			}
			if err := teardownGateway(ctx, d, cur, rs, adm); err != nil {
				return err
			}
		}
		want.Mode = GatewayOff
		if err := d.writeFiles(ctx, map[string]string{gatewayEnv: want.env(), gatewayRoutes: formatRoutes(rs)}); err != nil {
			return err
		}
		ui.Check("Gateway", "off. Your projects' names are kept for when you turn it back on: outpost gateway "+d.h.String())
		return nil
	}
	adm, err := pickAdmin(ctx, d, want, o.Yes, repair)
	if err != nil {
		return err
	}
	if cur.active() && !repair {
		if err := teardownGateway(ctx, d, cur, rs, adm); err != nil {
			return err
		}
	}
	got, err := applyGateway(ctx, d, want, rs, adm)
	if err != nil {
		return err
	}
	printGateway(d.h, got, rs)
	if got.Mode == GatewayDomain && !repair {
		if o.Trust == "yes" {
			if err := trustHere(ctx, d, got); err != nil {
				ui.Problem("This computer", err.Error(), "outpost gateway trust "+d.h.String())
			}
		} else {
			lipgloss.Println("  " + ui.Muted.Render("To trust it on this computer any time: outpost gateway trust "+d.h.String()))
		}
	}
	return nil
}

// chooseGateway settles which names a server's projects get, asking what o
// leaves open. During setup it first asks whether to have names at all,
// suggesting yes when devices already use Tailscale.
func chooseGateway(ctx context.Context, d dev, cur gatewayConfig, access Access, o *GatewayOptions, inSetup bool) (gatewayConfig, error) {
	want := cur
	if want.Suffix == "" {
		out, _ := d.output(ctx, "hostname -s")
		want.Suffix = strings.ToLower(strings.TrimSpace(string(out)))
	}
	tailnetName := "tail1234.ts.net"
	if self, err := d.tailscaleSelf(ctx); err == nil && self.Tailnet != "" {
		tailnetName = self.Tailnet
	}
	example := route{Name: "myapp"}
	tsExample := gatewayConfig{Mode: GatewayTailscale, Suffix: want.Suffix, Tailnet: tailnetName}.url(example)

	mode := o.Mode
	if mode == GatewayUnset && inSetup {
		yes, err := askWantGateway(ctx, access)
		if err != nil {
			return cur, err
		}
		if !yes {
			mode = GatewayOff
		}
	}
	if mode == GatewayUnset {
		options := []ui.Option{
			{Label: "Tailscale names, like " + tsExample + ": nothing to set up on devices", Value: string(GatewayTailscale)},
			{Label: "Your own domain, like https://myapp." + want.Suffix + ".test: each device trusts a certificate once", Value: string(GatewayDomain)},
		}
		if !inSetup {
			options = append(options, ui.Option{Label: "Off: no HTTPS names for projects", Value: string(GatewayOff)})
		}
		current := string(cur.Mode)
		if !cur.active() {
			current = string(GatewayTailscale)
		}
		answer, err := ui.Ask.Choose(ctx, "Which names should your projects get?", options, current)
		if err != nil {
			return cur, err
		}
		if mode, err = ParseGatewayMode(answer); err != nil {
			return cur, err
		}
	}
	want.Mode = mode
	if mode != GatewayDomain {
		return want, nil
	}

	domain := strings.ToLower(strings.TrimSpace(o.Domain))
	if domain == "" && (cur.Domain == "" || cur.Mode != GatewayDomain || o.Mode == GatewayUnset) {
		suggest := cur.Domain
		if suggest == "" {
			suggest = want.Suffix + ".test"
		}
		answer, err := ui.Ask.Input(ctx, "Which domain should your projects' names be under?",
			"Each project gets a name under it, like myapp."+suggest+". Leave it empty for "+suggest+".\n"+
				"Best under .test or .internal, which never exist on the internet. Not .local.",
			suggest, func(s string) error {
				if strings.TrimSpace(s) == "" {
					return nil
				}
				_, err := checkDomain(s)
				return err
			})
		if errors.Is(err, ui.ErrNoTerminal) {
			answer, err = "", nil
		}
		if err != nil {
			return cur, err
		}
		if domain = strings.ToLower(strings.TrimSpace(answer)); domain == "" {
			domain = suggest
		}
	}
	if domain == "" {
		domain = cur.Domain
	}
	domain = strings.TrimSuffix(domain, ".")
	public, err := checkDomain(domain)
	if err != nil {
		return cur, err
	}
	if public && !o.Yes {
		if err := confirm(ctx, "Use "+domain+"? It could be a real domain",
			"On your devices, outpost's names under "+domain+" would hide any real sites there.\n"+
				"Names under .test or .internal can't clash with anything.", "Use it"); err != nil {
			return cur, err
		}
	}
	want.Domain = domain
	want.Service = "svc:gateway-" + want.Suffix
	if o.Trust == "" && !o.Yes && !(cur.Mode == GatewayDomain && cur.Domain == domain) {
		if o.Trust, err = askTrust(ctx, domain); err != nil {
			return cur, err
		}
	}
	return want, nil
}

// askTrust is the tree's last question for your own domain: should this
// computer, and its browsers, trust the gateway's certificate now?
func askTrust(ctx context.Context, domain string) (string, error) {
	where := "this computer"
	if browsers := firefoxProfiles(); len(browsers) > 0 {
		where += " and " + strings.Join(browserNames(browsers), ", ")
	}
	answer, err := ui.Ask.Choose(ctx, "Trust "+domain+" on this computer?", []ui.Option{
		{Label: "Yes: " + where + " (asks for your password)", Value: "yes"},
		{Label: "No, not now (outpost gateway trust does it later)", Value: "no"},
	}, "yes")
	if errors.Is(err, ui.ErrNoTerminal) {
		return "", nil
	}
	return answer, err
}

// askWantGateway is the tree's first question: names for projects at all?
func askWantGateway(ctx context.Context, access Access) (bool, error) {
	desc := "Each project gets a private HTTPS address on your tailnet, so web apps you build here\n" +
		"work on your laptop and phone: the clipboard, passkeys and the like need HTTPS."
	current := "yes"
	if access == AccessConnect {
		desc += "\nYour devices reach T3 through T3 Connect, so for this they need Tailscale too."
		current = "no"
	} else {
		desc += "\nYour devices are on your tailnet already."
	}
	answer, err := ui.Ask.Choose(ctx, "Open your projects' web apps from your other devices too?", []ui.Option{
		{Label: "Yes: give each project an HTTPS address", Value: "yes"},
		{Label: "No, not now (outpost gateway turns it on later)", Value: "no"},
	}, current)
	if err != nil {
		return false, err
	}
	return answer == "yes", nil
}

// applyGateway makes the server's gateway match c and rs: the tailnet's side
// through adm, then the server's, then checks each name answers. It returns
// the settings it found on the way, such as the server's tailnet address.
func applyGateway(ctx context.Context, d dev, c gatewayConfig, rs []route, adm tailnetAdmin) (gatewayConfig, error) {
	self, err := d.tailscaleSelf(ctx)
	if err != nil {
		return c, err
	}
	switch c.Mode {
	case GatewayTailscale:
		c.Tailnet = self.Tailnet
		if err := checkServiceNames(rs, c.Suffix); err != nil {
			return c, err
		}
	case GatewayDomain:
		c.IP = self.IP
	}
	// The names need MagicDNS, and Tailscale names need HTTPS certificates: the admin turns them on if they're off.
	w := gatewayWant(c, rs, self.ID)
	w.MagicDNS = !self.MagicDNS
	w.HTTPS = c.Mode == GatewayTailscale && !self.HTTPS
	if _, ok := adm.(noAdmin); ok && (w.MagicDNS || w.HTTPS) {
		return c, errors.New("the gateway needs MagicDNS and HTTPS certificates, and one is off in your tailnet. " +
			"Turn them on at https://login.tailscale.com/admin/dns, or give outpost a Tailscale API key, then run this again")
	}
	addrs, err := adm.prepare(ctx, w)
	if err != nil {
		return c, err
	}
	if w.MagicDNS || w.HTTPS {
		// The server hears of the change in a few seconds; serving names needs it.
		if err := poll(ctx, featuresTimeout, func() bool {
			s, err := d.tailscaleSelf(ctx)
			return err == nil && s.MagicDNS && (!w.HTTPS || s.HTTPS)
		}); err != nil {
			return c, errors.New("MagicDNS or HTTPS certificates are still off for the server a minute later. Check https://login.tailscale.com/admin/dns, then run this again")
		}
	}
	if c.Mode == GatewayDomain {
		if c.Address = addrs[c.Service]; c.Address == "" {
			return c, fmt.Errorf("couldn't find the tailnet address of %s. Check it's defined in your tailnet, then run this again", c.Service)
		}
	}

	if _, err := d.output(ctx, cmdClearSites); err != nil {
		return c, err
	}
	if err := d.writeFiles(ctx, gatewayFiles(c, rs)); err != nil {
		return c, err
	}
	if c.Mode == GatewayDomain {
		if err := installDomainGateway(ctx, d, c); err != nil {
			return c, err
		}
		if err := refreshPage(ctx, d, c, rs); err != nil {
			return c, err
		}
	}
	if err := serveGateway(ctx, d, c, rs); err != nil {
		return c, err
	}
	if err := adm.finish(ctx, w); err != nil {
		return c, err
	}
	checkRoutes(ctx, d, c, rs)
	return c, nil
}

// gatewayWant is the tailnet state a gateway needs.
func gatewayWant(c gatewayConfig, rs []route, device string) tailnet.Want {
	w := tailnet.Want{Device: device, Services: c.services(rs)}
	if c.Mode == GatewayDomain {
		w.SplitDNS = map[string]string{c.Domain: c.IP}
	}
	return w
}

// serveGateway has the server's Tailscale publish the gateway's Services.
func serveGateway(ctx context.Context, d dev, c gatewayConfig, rs []route) (err error) {
	p := ui.NewProgress()
	defer func() { p.Done(err) }()
	if c.Mode == GatewayDomain {
		p.Task("Publishing " + c.Domain + " on your tailnet")
		if _, err := d.output(ctx, domainServeCmd(c)); err != nil {
			return fmt.Errorf("publish %s: %w", c.Service, servedErr(err))
		}
		return nil
	}
	for _, r := range rs {
		p.Task("Publishing " + c.host(r))
		if _, err := d.output(ctx, serveCmd(c, r)); err != nil {
			return fmt.Errorf("publish %s: %w", r.Name, servedErr(err))
		}
	}
	return nil
}

func servedErr(err error) error {
	if strings.Contains(err.Error(), "tagged") {
		return fmt.Errorf("Tailscale only lets tagged machines host Services, and this one isn't tagged %s yet: %w", tailnet.Tag, err)
	}
	return err
}

// checkRoutes asks each name for its page, through the same front door
// devices use, which also has Tailscale make each new name's certificate.
func checkRoutes(ctx context.Context, d dev, c gatewayConfig, rs []route) {
	if len(rs) == 0 {
		return
	}
	p := ui.NewProgress()
	var problems []string
	for _, r := range rs {
		p.Task("Checking " + c.host(r))
		if problem := d.probe(ctx, c, r); problem != "" {
			problems = append(problems, c.host(r)+": "+problem)
		}
	}
	p.Done(nil)
	for _, problem := range problems {
		lipgloss.Println("  " + ui.Muted.Render("• "+problem))
	}
}

// teardownGateway stops a gateway's names: on the server, and in the tailnet through adm.
func teardownGateway(ctx context.Context, d dev, c gatewayConfig, rs []route, adm tailnetAdmin) (err error) {
	p := ui.NewProgress()
	p.Task("Taking down " + c.Mode.String())
	services := sortedKeys(c.services(rs))
	_, _ = d.output(ctx, unserveCmd(services))
	var domains []string
	if c.Mode == GatewayDomain {
		_, _ = d.output(ctx, cmdGatewayStop)
		domains = []string{c.Domain}
	}
	p.Done(nil)
	return adm.forget(ctx, services, domains)
}

// stopServing stops a server publishing a gateway, leaving the tailnet as it
// is: for the old server in a move, whose new server takes the names over.
func stopServing(ctx context.Context, d dev, c gatewayConfig, rs []route) {
	_, _ = d.output(ctx, unserveCmd(sortedKeys(c.services(rs))))
	if c.Mode == GatewayDomain {
		_, _ = d.output(ctx, cmdGatewayStop)
	}
}

// refreshPage writes the page devices trust the domain gateway from.
func refreshPage(ctx context.Context, d dev, c gatewayConfig, rs []route) error {
	root, err := d.rootCert(ctx, c)
	if err != nil {
		return err
	}
	files, err := pageFiles(hostname(ctx, d), d.h.String(), c, rs, root)
	if err != nil {
		return err
	}
	return d.writeFiles(ctx, files)
}

// gatewayStep is setup's gateway step: it asks the tree on a server that
// hasn't answered yet, and checks and repairs one that has.
func gatewayStep(ctx context.Context, d dev, access Access, o GatewayOptions) error {
	cur, rs := d.gateway(ctx)
	if o.Mode == GatewayUnset {
		switch {
		case cur.Mode == GatewayOff:
			ui.Check("Gateway", "off. To give projects HTTPS names: outpost gateway "+d.h.String())
			return nil
		case cur.active():
			return runGateway(ctx, d, cur, cur, rs, GatewayOptions{Yes: true})
		}
	}
	want, err := chooseGateway(ctx, d, cur, access, &o, true)
	if errors.Is(err, ui.ErrNoTerminal) {
		lipgloss.Println("  " + ui.Muted.Render("No HTTPS names for projects. To add them later: outpost gateway "+d.h.String()))
		return nil
	}
	if err != nil {
		return err
	}
	return runGateway(ctx, d, cur, want, rs, o)
}

// moveGateway hands a gateway over from the old server to the new one in a
// move, so the same names reach the same projects there. Its settings, routes
// and certificate authority came across with the work. A problem here doesn't
// fail the move: it says what to run to finish.
func moveGateway(ctx context.Context, from, to dev, yes bool) error {
	c, rs := to.gateway(ctx)
	if !c.active() {
		ui.Check("Gateway", "not in use")
		return nil
	}
	stopServing(ctx, from, c, rs)
	err := func() error {
		adm, err := pickAdmin(ctx, to, c, yes, false)
		if err != nil {
			return err
		}
		got, err := applyGateway(ctx, to, c, rs, adm)
		if err == nil {
			printGateway(to.h, got, rs)
		}
		return err
	}()
	if err != nil {
		ui.Problem("Gateway", "not moved yet: "+err.Error(), "outpost gateway "+to.h.String()+" "+string(c.Mode))
	}
	return nil
}

// GatewayAdd gives a project names on a server's gateway: its own name, and
// any extra ones. With no project it asks for one. A project added again gets
// the new routes in place of its old ones.
func GatewayAdd(ctx context.Context, h remote.Host, project, port string, paths, names []string, yes bool) error {
	d, err := openActive(ctx, h)
	if err != nil {
		return err
	}
	c, rs := d.gateway(ctx)
	if !c.active() {
		lipgloss.Println("  " + ui.Muted.Render("The gateway is off on "+h.String()+", so projects there have no HTTPS names yet."))
		access, _ := d.access(ctx)
		opts := GatewayOptions{Yes: yes}
		want, err := chooseGateway(ctx, d, c, access, &opts, false)
		if errors.Is(err, ui.ErrNoTerminal) {
			return errors.New("turn the gateway on first: outpost gateway " + h.String() + " tailscale (or domain)")
		}
		if err != nil {
			return err
		}
		if !want.active() {
			return ui.ErrCancelled
		}
		if err := runGateway(ctx, d, c, want, rs, opts); err != nil {
			return err
		}
		if c, rs = d.gateway(ctx); !c.active() {
			return errors.New("the gateway didn't turn on; see above")
		}
	}
	if project == "" {
		if project, port, err = askRoute(ctx); err != nil {
			return err
		}
	}
	add, err := NewRoutes(project, port, paths, names)
	if err != nil {
		return err
	}
	for _, r := range add {
		for _, o := range rs {
			if o.Name == r.Name && o.Project != project {
				return fmt.Errorf("%s is already a name of %s. Pick another, or remove that project first: outpost gateway remove %s %s", r.Name, o.Project, h, o.Project)
			}
		}
	}
	if c.Mode == GatewayTailscale {
		if err := checkServiceNames(add, c.Suffix); err != nil {
			return err
		}
	}
	old := slices.DeleteFunc(slices.Clone(rs), func(r route) bool { return r.Project != project })
	all := withRoutes(rs, project, add)
	if err := publishRoutes(ctx, d, c, old, add, all, yes); err != nil {
		return err
	}
	lipgloss.Println("")
	for _, r := range add {
		ui.Check(r.Name, ui.Accent.Render(c.url(r)))
	}
	return nil
}

// publishRoutes changes a gateway's routes from old to add for one project,
// within all of the server's routes after the change.
func publishRoutes(ctx context.Context, d dev, c gatewayConfig, old, add, all []route, yes bool) error {
	if c.Mode == GatewayDomain {
		if _, err := d.output(ctx, cmdClearSites); err != nil {
			return err
		}
		if err := d.writeFiles(ctx, gatewayFiles(c, all)); err != nil {
			return err
		}
		if err := refreshPage(ctx, d, c, all); err != nil {
			return err
		}
		if _, err := d.output(ctx, cmdCaddyReload); err != nil {
			return fmt.Errorf("Caddy didn't take the new routes, so it keeps the old ones: %w", err)
		}
		checkRoutes(ctx, d, c, add)
		return nil
	}

	gone := slices.DeleteFunc(slices.Clone(old), func(o route) bool {
		return slices.ContainsFunc(add, func(r route) bool { return r.Name == o.Name })
	})
	adm, err := pickAdmin(ctx, d, c, yes, false)
	if err != nil {
		return err
	}
	if len(gone) > 0 {
		if err := teardownGateway(ctx, d, c, gone, adm); err != nil {
			return err
		}
	}
	self, err := d.tailscaleSelf(ctx)
	if err != nil {
		return err
	}
	w := gatewayWant(c, add, self.ID)
	if _, err := adm.prepare(ctx, w); err != nil {
		return err
	}
	if err := d.writeFiles(ctx, gatewayFiles(c, all)); err != nil {
		return err
	}
	if err := serveGateway(ctx, d, c, add); err != nil {
		return err
	}
	if err := adm.finish(ctx, w); err != nil {
		return err
	}
	checkRoutes(ctx, d, c, add)
	return nil
}

// askRoute asks for a project's name and port.
func askRoute(ctx context.Context) (project, port string, err error) {
	project, err = ui.Ask.Input(ctx, "What's the project called?",
		"Its name becomes its address, like myapp in https://myapp.…", "myapp", checkProject)
	if errors.Is(err, ui.ErrNoTerminal) {
		return "", "", errors.New("name the project and its port: outpost gateway add SERVER PROJECT PORT")
	}
	if err != nil {
		return "", "", err
	}
	port, err = ui.Ask.Input(ctx, "Which port does it listen on, on the server?",
		"The port its web server uses on 127.0.0.1, like 3000. outpost passes HTTPS to it.", "3000",
		func(s string) error { _, err := parsePort(s); return err })
	return project, port, err
}

// GatewayRemove takes a project's names off a server's gateway. With no
// project it asks which.
func GatewayRemove(ctx context.Context, h remote.Host, project string, yes bool) error {
	d, err := openActive(ctx, h)
	if err != nil {
		return err
	}
	c, rs := d.gateway(ctx)
	ps := projects(rs)
	if len(ps) == 0 {
		return errors.New("no projects have names on " + h.String() + " yet. Add one with: outpost gateway add " + h.String() + " PROJECT PORT")
	}
	if project == "" {
		var options []ui.Option
		for _, p := range ps {
			options = append(options, ui.Option{Label: p, Value: p})
		}
		project, err = ui.Ask.Choose(ctx, "Which project's names should outpost remove?", options, ps[0])
		if errors.Is(err, ui.ErrNoTerminal) {
			return errors.New("name the project: outpost gateway remove " + h.String() + " PROJECT")
		}
		if err != nil {
			return err
		}
	}
	if !slices.Contains(ps, project) {
		return fmt.Errorf("%s has no names on %s. Its projects: %s", project, h, strings.Join(ps, ", "))
	}
	gone := slices.DeleteFunc(slices.Clone(rs), func(r route) bool { return r.Project != project })
	rest := withoutProject(rs, project)
	if c.Mode == GatewayDomain {
		if _, err := d.output(ctx, cmdClearSites); err != nil {
			return err
		}
		if err := d.writeFiles(ctx, gatewayFiles(c, rest)); err != nil {
			return err
		}
		if err := refreshPage(ctx, d, c, rest); err != nil {
			return err
		}
		if _, err := d.output(ctx, cmdCaddyReload); err != nil {
			return fmt.Errorf("Caddy didn't reload: %w", err)
		}
	} else {
		var adm tailnetAdmin = manualAdmin{d}
		if c.active() {
			if adm, err = pickAdmin(ctx, d, c, yes, true); err != nil {
				return err
			}
			if _, ok := adm.(noAdmin); ok {
				adm = manualAdmin{d} // without a key, say what to tidy up
			}
		}
		if err := d.writeFiles(ctx, gatewayFiles(c, rest)); err != nil {
			return err
		}
		if c.active() {
			if err := teardownGateway(ctx, d, c, gone, adm); err != nil {
				return err
			}
		}
	}
	ui.Check(project, "removed")
	return nil
}

// runLocal runs a command on this computer with the terminal attached; tests replace it.
var runLocal = func(ctx context.Context, name string, args ...string) error {
	c := exec.CommandContext(ctx, name, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// GatewayTrust makes this computer trust a server's gateway certificate, and
// says how other devices can. It's for a gateway with its own domain:
// Tailscale names have certificates every device trusts already.
func GatewayTrust(ctx context.Context, h remote.Host) error {
	d, err := openActive(ctx, h)
	if err != nil {
		return err
	}
	c, _ := d.gateway(ctx)
	if c.Mode != GatewayDomain {
		return errors.New("there's nothing to trust: " + h.String() + "'s projects " + map[bool]string{
			true:  "have Tailscale names, whose certificates every device trusts already",
			false: "have no gateway names. Turn them on with: outpost gateway " + h.String()}[c.Mode == GatewayTailscale])
	}
	if err := trustHere(ctx, d, c); err != nil {
		return err
	}
	printTrustPage(c)
	return nil
}

func trustHere(ctx context.Context, d dev, c gatewayConfig) error {
	root, err := d.rootCert(ctx, c)
	if err != nil {
		return err
	}
	file := filepath.Join(os.TempDir(), "outpost-"+c.Domain+".crt")
	if err := os.WriteFile(file, root, 0o644); err != nil {
		return err
	}
	defer os.Remove(file)
	lipgloss.Println("  " + ui.Muted.Render("Your password lets outpost add the certificate for "+c.Domain+" to this computer:"))
	switch runtime.GOOS {
	case "darwin":
		err = runLocal(ctx, "sudo", "security", "add-trusted-cert", "-d", "-r", "trustRoot", "-k", "/Library/Keychains/System.keychain", file)
	case "linux":
		err = runLocal(ctx, "sudo", "sh", "-c", "cp "+quote(file)+" /usr/local/share/ca-certificates/outpost-"+c.Domain+".crt && update-ca-certificates")
	default:
		return errors.New("outpost can trust the certificate on macOS and Linux. Here, open http://" + c.Domain + " and follow the steps")
	}
	if err != nil {
		return fmt.Errorf("trust the certificate: %w", err)
	}
	ui.Check("This computer", "trusts names under "+c.Domain)
	if runtime.GOOS != "darwin" {
		return nil // Firefox on Linux can't use the system's certificates; the trust page shows how
	}
	profiles := firefoxProfiles()
	for _, p := range profiles {
		if err := useSystemCertificates(p); err != nil {
			return fmt.Errorf("let %s use this Mac's certificates: %w", p.browser, err)
		}
	}
	if len(profiles) > 0 {
		ui.Check(strings.Join(browserNames(profiles), ", "), "use this Mac's certificates now; restart them if they're open")
	}
	return nil
}

// A firefoxProfile is a profile of Firefox or a browser built on it (Zen,
// LibreWolf and the like), which keep their own certificates.
type firefoxProfile struct{ browser, dir string }

// firefoxBrowsers are where Firefox-based browsers keep their profiles on a Mac.
var firefoxBrowsers = map[string]string{"Firefox": "Firefox", "Zen": "zen", "LibreWolf": "librewolf", "Floorp": "Floorp", "Waterfox": "Waterfox"}

func firefoxProfiles() []firefoxProfile {
	if runtime.GOOS != "darwin" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var ps []firefoxProfile
	for _, name := range sortedKeys(firefoxBrowsers) {
		dirs, _ := filepath.Glob(filepath.Join(home, "Library", "Application Support", firefoxBrowsers[name], "Profiles", "*"))
		for _, dir := range dirs {
			if _, err := os.Stat(filepath.Join(dir, "prefs.js")); err == nil {
				ps = append(ps, firefoxProfile{name, dir})
			}
		}
	}
	return ps
}

func browserNames(ps []firefoxProfile) []string {
	var names []string
	for _, p := range ps {
		if !slices.Contains(names, p.browser) {
			names = append(names, p.browser)
		}
	}
	return names
}

// enterpriseRoots is the setting that has a Firefox-based browser trust the
// certificates the Mac trusts. user.js is read at each start and overrides prefs.js.
const enterpriseRoots = `user_pref("security.enterprise_roots.enabled", true);`

// useSystemCertificates turns enterpriseRoots on in a browser profile, once.
func useSystemCertificates(p firefoxProfile) error {
	file := filepath.Join(p.dir, "user.js")
	old, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	prefs, _ := os.ReadFile(filepath.Join(p.dir, "prefs.js")) // where about:config keeps a choice
	if strings.Contains(string(old)+string(prefs), "security.enterprise_roots.enabled") {
		return nil // already decided, by outpost or by you
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if len(old) > 0 && !strings.HasSuffix(string(old), "\n") {
		_, _ = f.WriteString("\n")
	}
	_, err = f.WriteString("// Added by outpost gateway trust: use the certificates this Mac trusts.\n" + enterpriseRoots + "\n")
	return errors.Join(err, f.Close())
}

// describeGateway sums up a gateway in a few words.
func describeGateway(c gatewayConfig, rs []route) string {
	switch {
	case !c.active():
		return "no HTTPS names"
	case c.Mode == GatewayDomain:
		return "names under " + c.Domain + ", " + plural(fmt.Sprint(len(projects(rs))), "project")
	}
	return "Tailscale names, " + plural(fmt.Sprint(len(projects(rs))), "project")
}

// printGateway shows each project's address.
func printGateway(h remote.Host, c gatewayConfig, rs []route) {
	lipgloss.Println("")
	if len(rs) == 0 {
		ui.Check("Gateway", "ready: "+c.Mode.String()+". Give a project an address: outpost gateway add "+h.String()+" PROJECT PORT")
	} else {
		ui.Check("Gateway", "ready: "+c.Mode.String())
		for _, r := range rs {
			lipgloss.Println("    " + padRight(r.Name, 18) + ui.Accent.Render(c.url(r)))
		}
	}
	if c.Mode == GatewayDomain {
		printTrustPage(c)
	}
}

// printTrustPage points other devices at the page they trust the gateway
// from, with a QR code for a phone's camera.
func printTrustPage(c gatewayConfig) {
	url := "http://" + c.Domain
	lipgloss.Println("\n  " + ui.Bold.Render("Your other devices") + "  " + ui.Muted.Render("open ") + ui.Accent.Render(url) +
		ui.Muted.Render(" on each, or scan this with a phone:"))
	fmt.Print(ui.QR(url))
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s + " "
	}
	return s + strings.Repeat(" ", n-len(s))
}
