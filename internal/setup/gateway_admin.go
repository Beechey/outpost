package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Beechey/outpost/internal/tailnet"
	"github.com/Beechey/outpost/internal/ui"
)

// tailnetAdmin does the gateway's Tailscale admin steps. Three adapters: the
// API, with a key; the user, following steps outpost shows; and none, for a
// repair that only needs the server put right.
type tailnetAdmin interface {
	// prepare readies the tailnet before the server serves: policy, tag and
	// Services. It returns each Service's tailnet IPv4.
	prepare(ctx context.Context, w tailnet.Want) (map[string]string, error)
	// finish approves the server as the Services' host and sets split DNS.
	finish(ctx context.Context, w tailnet.Want) error
	// forget deletes Services and clears split DNS for domains.
	forget(ctx context.Context, services, domains []string) error
}

// apiAdmin does the steps through the Tailscale API.
type apiAdmin struct {
	c   *tailnet.Client
	yes bool // apply policy additions without asking
}

func (a apiAdmin) prepare(ctx context.Context, w tailnet.Want) (map[string]string, error) {
	confirmPolicy := func(added []string) error {
		lipgloss.Println("\n  " + ui.Bold.Render("Your tailnet policy needs these additions:"))
		for _, line := range added {
			lipgloss.Println("  " + ui.Accent.Render("+") + " " + ui.Muted.Render(line))
		}
		if a.yes {
			return nil
		}
		return confirm(ctx, "Add them to your tailnet policy?",
			"The server is tagged "+tailnet.Tag+" so it can host Tailscale Services, and the copies keep\n"+
				"your SSH and access rules reaching it once it's tagged. Nothing else changes.", "Add them")
	}
	return a.c.Prepare(ctx, w, confirmPolicy)
}

func (a apiAdmin) finish(ctx context.Context, w tailnet.Want) error { return a.c.Finish(ctx, w) }

func (a apiAdmin) forget(ctx context.Context, services, domains []string) error {
	return a.c.Forget(ctx, services, domains)
}

// manualAdmin shows the user each step to do in the Tailscale admin console,
// then checks what it can from the server.
type manualAdmin struct{ d dev }

func (m manualAdmin) prepare(ctx context.Context, w tailnet.Want) (map[string]string, error) {
	self, err := m.d.tailscaleSelf(ctx)
	if err != nil {
		return nil, err
	}
	var steps []string
	if w.MagicDNS || w.HTTPS {
		steps = append(steps, "In https://login.tailscale.com/admin/dns, turn on MagicDNS and HTTPS Certificates")
	}
	if !slices.Contains(self.Tags, tailnet.Tag) {
		steps = append(steps,
			"In https://login.tailscale.com/admin/acls, add to \"tagOwners\":  \""+tailnet.Tag+"\": [\"autogroup:admin\"]\n"+
				"     If an \"ssh\" rule reaches \"autogroup:self\", add a copy with \"dst\": [\""+tailnet.Tag+"\"], so SSH keeps working.",
			"In https://login.tailscale.com/admin/machines, open "+self.DNSName+" → Edit ACL tags → add "+tailnet.Tag)
	}
	for _, svc := range sortedKeys(w.Services) {
		steps = append(steps, "In https://login.tailscale.com/admin/services, Advertise → Define a Service: name "+
			strings.TrimPrefix(svc, "svc:")+", endpoints "+strings.Join(w.Services[svc], ", "))
	}
	if err := m.show(ctx, "Do these in the Tailscale admin console:", steps); err != nil {
		return nil, err
	}
	if self, err = m.d.tailscaleSelf(ctx); err == nil && !slices.Contains(self.Tags, tailnet.Tag) {
		return nil, fmt.Errorf("the server isn't tagged %s yet. Tag it, then run the same command again", tailnet.Tag)
	}
	addrs := map[string]string{}
	for svc := range w.Services {
		out, _ := m.d.output(ctx, "getent ahostsv4 "+quote(strings.TrimPrefix(svc, "svc:")+"."+self.Tailnet)+" | awk 'NR==1 {print $1}'")
		addrs[svc] = strings.TrimSpace(string(out))
	}
	return addrs, nil
}

func (m manualAdmin) finish(ctx context.Context, w tailnet.Want) error {
	var steps []string
	for _, svc := range sortedKeys(w.Services) {
		steps = append(steps, "In https://login.tailscale.com/admin/services, open "+strings.TrimPrefix(svc, "svc:")+" and approve this server as its host")
	}
	for _, domain := range sortedKeys(w.SplitDNS) {
		steps = append(steps, "In https://login.tailscale.com/admin/dns, Add nameserver → Custom: "+w.SplitDNS[domain]+
			", turn on Restrict to domain (split DNS), and enter "+domain)
	}
	return m.show(ctx, "Then these:", steps)
}

func (m manualAdmin) forget(_ context.Context, services, domains []string) error {
	var steps []string
	for _, svc := range services {
		steps = append(steps, "In https://login.tailscale.com/admin/services, delete "+strings.TrimPrefix(svc, "svc:"))
	}
	for _, d := range domains {
		steps = append(steps, "In https://login.tailscale.com/admin/dns, remove the nameserver for "+d)
	}
	if len(steps) > 0 {
		printSteps("To tidy up your tailnet, when you like:", steps)
	}
	return nil
}

func (m manualAdmin) show(ctx context.Context, title string, steps []string) error {
	if len(steps) == 0 {
		return nil
	}
	printSteps(title, steps)
	ok, err := ui.Ask.Confirm(ctx, "Done?", "outpost checks each one from the server once you are.", "Done")
	switch {
	case errors.Is(err, ui.ErrNoTerminal):
		return errors.New("run in a terminal to follow the steps, or give outpost a Tailscale API key in TS_API_KEY")
	case err != nil:
		return err
	case !ok:
		return ui.ErrCancelled
	}
	return nil
}

func printSteps(title string, steps []string) {
	lipgloss.Println("\n  " + ui.Bold.Render(title))
	for i, s := range steps {
		lipgloss.Println("  " + ui.Accent.Render(fmt.Sprintf("%d.", i+1)) + " " + s)
	}
	fmt.Println()
}

// noAdmin leaves the tailnet as it is: for repairs without a key, where the
// tailnet side was done before. It knows the domain gateway's address from its settings.
type noAdmin struct{ c gatewayConfig }

func (n noAdmin) prepare(context.Context, tailnet.Want) (map[string]string, error) {
	return map[string]string{n.c.Service: n.c.Address}, nil
}
func (noAdmin) finish(context.Context, tailnet.Want) error       { return nil }
func (noAdmin) forget(context.Context, []string, []string) error { return nil }

// keyFile is where outpost keeps a Tailscale API key on this computer, so
// adding a project or moving needs nothing more.
func keyFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "outpost", "tailscale-key"), nil
}

// savedKey is the key in TS_API_KEY, else the one outpost kept, else "".
func savedKey() string {
	if k := strings.TrimSpace(os.Getenv("TS_API_KEY")); k != "" {
		return k
	}
	path, err := keyFile()
	if err != nil {
		return ""
	}
	b, _ := os.ReadFile(path)
	return strings.TrimSpace(string(b))
}

func saveKey(key string) error {
	path, err := keyFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(key+"\n"), 0o600)
}

// keysPage is where a person makes a Tailscale API key.
const keysPage = "https://login.tailscale.com/admin/settings/keys"

// newAPI makes an API client; tests replace it to talk to a fake.
var newAPI = func(key string) *tailnet.Client { return tailnet.New(key) }

// pickAdmin chooses how the Tailscale admin steps get done: with the saved
// key if there is one, otherwise as the user says. A repair with no key
// leaves the tailnet alone.
// Tests replace it with a fake admin.
var pickAdmin = func(ctx context.Context, d dev, c gatewayConfig, yes, repair bool) (tailnetAdmin, error) {
	if key := savedKey(); key != "" {
		api := newAPI(key)
		if err := api.Verify(ctx); err == nil {
			return apiAdmin{api, yes}, nil
		} else if !errors.Is(err, tailnet.ErrBadKey) {
			return nil, err
		}
		lipgloss.Println("  " + ui.Muted.Render("Tailscale no longer accepts the saved API key."))
	}
	if repair {
		return noAdmin{c}, nil
	}
	how, err := ui.Ask.Choose(ctx, "Should outpost do the Tailscale admin steps for you?", []ui.Option{
		{Label: "Yes: I'll paste a Tailscale API key", Value: "key"},
		{Label: "No: show me each step to do in the admin console", Value: "manual"},
	}, "key")
	if errors.Is(err, ui.ErrNoTerminal) {
		return nil, errors.New("outpost needs a Tailscale API key for this. Put one in TS_API_KEY, or run in a terminal")
	}
	if err != nil {
		return nil, err
	}
	if how == "manual" {
		return manualAdmin{d}, nil
	}
	// Only a person can make the key; open the page where they do.
	_ = openURL(ctx, keysPage)
	for {
		key, err := ui.Ask.Secret(ctx, "Paste a Tailscale API key",
			"outpost opened "+keysPage+" for you. Generate an access token\n"+
				"(it expires within 90 days), or a trust credential with the policy_file, devices:core,\n"+
				"services, dns and networking_settings scopes, which doesn't expire.\n"+
				"outpost keeps it on this computer only, for adding projects and moving servers.")
		if err != nil {
			return nil, err
		}
		api := newAPI(key)
		if err := api.Verify(ctx); err != nil {
			lipgloss.Println("  " + ui.Muted.Render(err.Error()+". Try again, or press Ctrl-C."))
			continue
		}
		if err := saveKey(strings.TrimSpace(key)); err != nil {
			return nil, fmt.Errorf("keep the key: %w", err)
		}
		ui.Check("Tailscale API key", "kept on this computer, for adding projects and moves")
		return apiAdmin{api, yes}, nil
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
