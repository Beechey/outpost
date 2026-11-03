package setup

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The gateway gives each project on a server private HTTPS names that every
// device on the tailnet can open, so browser features that need https (the
// clipboard, crypto.randomUUID, passkeys) work there. Two kinds of names:
//
//   - Tailscale names: one Tailscale Service per name, such as
//     https://myapp-devbox.tail1234.ts.net. Tailscale serves them with real
//     certificates, so devices need nothing.
//   - Your own domain: https://myapp.devbox.test. Caddy terminates TLS with a
//     certificate authority of its own, which can only sign names under the
//     domain; CoreDNS answers for the domain through Tailscale split DNS; and a
//     Tailscale Service gives the gateway an address with port 443 free. Each
//     device trusts the certificate authority once.
//
// Everything lives in ~/.outpost/gateway, so a move carries it, certificate
// authority included, and devices stay trusted on the new server.

// GatewayMode is which names a server's projects get, if any.
type GatewayMode string

const (
	GatewayUnset     GatewayMode = ""
	GatewayOff       GatewayMode = "off"
	GatewayTailscale GatewayMode = "tailscale"
	GatewayDomain    GatewayMode = "domain"
)

// ParseGatewayMode reads a mode as typed: off, tailscale or domain.
func ParseGatewayMode(s string) (GatewayMode, error) {
	switch m := GatewayMode(strings.ToLower(strings.TrimSpace(s))); m {
	case GatewayUnset, GatewayOff, GatewayTailscale, GatewayDomain:
		return m, nil
	}
	return "", fmt.Errorf("%q isn't a kind of name; choose tailscale, domain or off", s)
}

func (m GatewayMode) String() string {
	switch m {
	case GatewayTailscale:
		return "Tailscale names"
	case GatewayDomain:
		return "your own domain"
	}
	return "off"
}

// Where the gateway lives, relative to the dev user's home.
const (
	gatewayDir    = ".outpost/gateway"
	gatewayEnv    = gatewayDir + "/gateway.env"
	gatewayRoutes = gatewayDir + "/routes"
)

// Loopback ports the domain gateway's Caddy listens on. The gateway's
// Tailscale Service maps 443 and 80 to them, so nothing listens on the tailnet
// or the internet but CoreDNS. Unusual numbers, to stay clear of projects.
const (
	gatewayHTTPS = 44343
	gatewayHTTP  = 44380
	caddyAdmin   = 44319
)

// gatewayConfig is ~/.outpost/gateway/gateway.env: the settings that vary,
// read by Caddy and CoreDNS as environment variables and by outpost.
type gatewayConfig struct {
	Mode    GatewayMode
	Domain  string // domain: the names' domain, e.g. devbox.test
	Suffix  string // tailscale: ends each Service name so servers don't clash, e.g. devbox
	Tailnet string // tailscale: the tailnet's DNS name, e.g. tail1234.ts.net
	Service string // domain: the Service devices reach the gateway at, e.g. svc:gateway-devbox
	Address string // domain: that Service's tailnet IPv4, which every name resolves to
	IP      string // domain: this server's tailnet IPv4, where CoreDNS answers
}

var envKeys = []string{"MODE", "GATEWAY_DOMAIN", "SUFFIX", "TAILNET", "SERVICE", "GATEWAY_ADDRESS", "TAILSCALE_IP"}

func (c *gatewayConfig) field(key string) *string {
	switch key {
	case "MODE":
		return (*string)(&c.Mode)
	case "GATEWAY_DOMAIN":
		return &c.Domain
	case "SUFFIX":
		return &c.Suffix
	case "TAILNET":
		return &c.Tailnet
	case "SERVICE":
		return &c.Service
	case "GATEWAY_ADDRESS":
		return &c.Address
	case "TAILSCALE_IP":
		return &c.IP
	}
	return nil
}

func (c gatewayConfig) env() string {
	var b strings.Builder
	b.WriteString("# outpost's gateway settings. Change them with: outpost gateway\n")
	for _, k := range envKeys {
		if v := *c.field(k); v != "" {
			b.WriteString(k + "=" + v + "\n")
		}
	}
	return b.String()
}

// safeValue is what a setting may hold: it's read by systemd and shells.
var safeValue = regexp.MustCompile(`^[A-Za-z0-9.:_-]*$`)

func parseGatewayEnv(s string) gatewayConfig {
	var c gatewayConfig
	for line := range strings.Lines(s) {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if f := c.field(k); ok && f != nil && safeValue.MatchString(v) {
			*f = v
		}
	}
	if _, err := ParseGatewayMode(string(c.Mode)); err != nil {
		c.Mode = GatewayUnset
	}
	return c
}

// A route sends one name to a project's loopback port, with paths that go to
// other ports. A project can have several names, one route each.
type route struct {
	Project string
	Name    string // under the domain, or the Service name: "myapp", "files.myapp"
	Port    int
	Paths   []pathRoute
}

type pathRoute struct {
	Path string
	Port int
}

var (
	projectRule = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)
	hostRule    = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)
	pathRule    = regexp.MustCompile(`^/[A-Za-z0-9._~-]+(/[A-Za-z0-9._~-]+)*$`)
)

func checkProject(p string) error {
	if !projectRule.MatchString(p) {
		return fmt.Errorf("%q can't be a project name: use lowercase letters, numbers and hyphens, up to 40 characters", p)
	}
	return nil
}

func parsePort(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("%q isn't a port: use a number from 1 to 65535", s)
	}
	return n, nil
}

// NewRoutes builds a project's routes as typed: its port, paths like
// "/ws=3001" that go elsewhere on its name, and extra names like
// "files.myapp=9000".
func NewRoutes(project, port string, paths, names []string) ([]route, error) {
	if err := checkProject(project); err != nil {
		return nil, err
	}
	main := route{Project: project, Name: project}
	var err error
	if main.Port, err = parsePort(port); err != nil {
		return nil, err
	}
	for _, p := range paths {
		path, port, ok := strings.Cut(p, "=")
		if !ok || !pathRule.MatchString(path) {
			return nil, fmt.Errorf("%q isn't a path route: write it as /path=PORT, like /ws=3001", p)
		}
		n, err := parsePort(port)
		if err != nil {
			return nil, err
		}
		main.Paths = append(main.Paths, pathRoute{path, n})
	}
	routes := []route{main}
	for _, extra := range names {
		name, port, ok := strings.Cut(extra, "=")
		if !ok || !hostRule.MatchString(name) {
			return nil, fmt.Errorf("%q isn't an extra name: write it as NAME=PORT, like files.%s=8333", extra, project)
		}
		n, err := parsePort(port)
		if err != nil {
			return nil, err
		}
		routes = append(routes, route{Project: project, Name: name, Port: n})
	}
	return routes, nil
}

// The routes file holds one route per line: "project name port [/path=port...]".

func formatRoutes(rs []route) string {
	var b strings.Builder
	b.WriteString("# project name port [/path=port ...]. Change them with: outpost gateway add / remove\n")
	for _, r := range rs {
		b.WriteString(r.Project + " " + r.Name + " " + strconv.Itoa(r.Port))
		for _, p := range r.Paths {
			b.WriteString(" " + p.Path + "=" + strconv.Itoa(p.Port))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// parseRoutes reads the routes file, skipping lines that don't check out:
// they'd otherwise reach Caddy, Tailscale and shell commands.
func parseRoutes(s string) []route {
	var rs []route
	for line := range strings.Lines(s) {
		f := strings.Fields(line)
		if len(f) < 3 || strings.HasPrefix(f[0], "#") {
			continue
		}
		got, err := NewRoutes(f[0], f[2], f[3:], nil)
		if err != nil || !hostRule.MatchString(f[1]) {
			continue
		}
		got[0].Name = f[1]
		rs = append(rs, got[0])
	}
	return rs
}

// withRoutes replaces a project's routes with add.
func withRoutes(rs []route, project string, add []route) []route {
	return append(withoutProject(rs, project), add...)
}

func withoutProject(rs []route, project string) []route {
	return slices.DeleteFunc(slices.Clone(rs), func(r route) bool { return r.Project == project })
}

func projects(rs []route) []string {
	var ps []string
	for _, r := range rs {
		if !slices.Contains(ps, r.Project) {
			ps = append(ps, r.Project)
		}
	}
	return ps
}

// serviceName is a route's Tailscale Service: its name with dots as hyphens,
// then the server's suffix, so "files.myapp" on devbox is svc:files-myapp-devbox.
func serviceName(name, suffix string) string {
	return "svc:" + strings.ReplaceAll(name, ".", "-") + "-" + suffix
}

// host is the name devices open for a route.
func (c gatewayConfig) host(r route) string {
	if c.Mode == GatewayDomain {
		return r.Name + "." + c.Domain
	}
	return strings.TrimPrefix(serviceName(r.Name, c.Suffix), "svc:") + "." + c.Tailnet
}

func (c gatewayConfig) url(r route) string { return "https://" + c.host(r) }

// services are the Tailscale Services the gateway needs, with their ports.
func (c gatewayConfig) services(rs []route) map[string][]string {
	m := map[string][]string{}
	switch c.Mode {
	case GatewayTailscale:
		for _, r := range rs {
			m[serviceName(r.Name, c.Suffix)] = []string{"tcp:443"}
		}
	case GatewayDomain:
		m[c.Service] = []string{"tcp:443", "tcp:80"}
	}
	return m
}

// checkServiceNames makes sure each route's Service name fits Tailscale's
// limit of 63 characters for a name.
func checkServiceNames(rs []route, suffix string) error {
	for _, r := range rs {
		if n := len(strings.TrimPrefix(serviceName(r.Name, suffix), "svc:")); n > 63 {
			return fmt.Errorf("%q makes a Tailscale name %d characters long, over the limit of 63. Use a shorter name", r.Name, n)
		}
	}
	return nil
}

// reservedTLDs are top-level domains that never exist on the internet, so
// names under them can't hide real sites.
var reservedTLDs = []string{"test", "internal", "home.arpa", "lan", "localhost", "example", "invalid"}

// checkDomain checks a domain for the gateway's names. .local is refused: it's
// for mDNS, which doesn't cross Tailscale. public reports a domain that could
// exist on the internet, whose real sites the gateway would hide.
func checkDomain(d string) (public bool, err error) {
	d = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d)), ".")
	if !hostRule.MatchString(d) || !strings.Contains(d, ".") {
		return false, fmt.Errorf("%q isn't a domain: use two or more parts, like devbox.test", d)
	}
	if d == "local" || strings.HasSuffix(d, ".local") {
		return false, fmt.Errorf("%s ends in .local, which devices only look up on their own network (mDNS), so Tailscale can't serve it. Try .test or .internal", d)
	}
	for _, tld := range reservedTLDs {
		if strings.HasSuffix(d, "."+tld) {
			return false, nil
		}
	}
	return true, nil
}

// caddySite is a route's site in the domain gateway's Caddyfile.
func caddySite(r route) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s.{$GATEWAY_DOMAIN} {\n", r.Name)
	for i, p := range r.Paths {
		fmt.Fprintf(&b, "\t@path%d path %s %s/*\n\thandle @path%[1]d {\n\t\treverse_proxy 127.0.0.1:%[4]d\n\t}\n", i, p.Path, p.Path, p.Port)
	}
	if len(r.Paths) > 0 {
		fmt.Fprintf(&b, "\thandle {\n\t\treverse_proxy 127.0.0.1:%d\n\t}\n", r.Port)
	} else {
		fmt.Fprintf(&b, "\treverse_proxy 127.0.0.1:%d\n", r.Port)
	}
	b.WriteString("}\n")
	return b.String()
}

// caddySites is every route's site, by project, for sites/<project>.caddy.
func caddySites(rs []route) map[string]string {
	m := map[string]string{}
	for _, r := range rs {
		m[r.Project] += "# " + r.Project + " (made by outpost gateway add)\n" + caddySite(r)
	}
	return m
}

// serveCmd publishes a route through its Tailscale Service. Tailscale strips a
// path's prefix before passing it on, so the target names the path again.
func serveCmd(c gatewayConfig, r route) string {
	svc := serviceName(r.Name, c.Suffix)
	cmd := "sudo tailscale serve clear " + svc + " >/dev/null 2>&1; " +
		fmt.Sprintf("sudo tailscale serve --service=%s --https=443 --yes http://127.0.0.1:%d >/dev/null", svc, r.Port)
	for _, p := range r.Paths {
		cmd += fmt.Sprintf(" && sudo tailscale serve --service=%s --https=443 --set-path=%s --yes http://127.0.0.1:%d%s >/dev/null", svc, p.Path, p.Port, p.Path)
	}
	return cmd
}

// unserveCmd stops publishing Services; ones already gone are fine.
func unserveCmd(services []string) string {
	if len(services) == 0 {
		return "true"
	}
	var parts []string
	for _, s := range services {
		parts = append(parts, "sudo tailscale serve clear "+s+" >/dev/null 2>&1")
	}
	return strings.Join(parts, "; ") + "; true"
}

// domainServeCmd points the domain gateway's Service at Caddy.
func domainServeCmd(c gatewayConfig) string {
	return fmt.Sprintf("sudo tailscale serve clear %s >/dev/null 2>&1; "+
		"sudo tailscale serve --service=%[1]s --tcp=443 --yes tcp://127.0.0.1:%[2]d >/dev/null && "+
		"sudo tailscale serve --service=%[1]s --tcp=80 --yes tcp://127.0.0.1:%[3]d >/dev/null", c.Service, gatewayHTTPS, gatewayHTTP)
}
