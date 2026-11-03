package setup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"html/template"
	"slices"
	"strings"

	"github.com/Beechey/outpost/internal/remote"
)

// What the gateway does on a server, as the dev user. Every command is built
// here, so tests answer them by name.

var (
	//go:embed gateway/Caddyfile
	caddyfile string
	//go:embed gateway/Corefile
	corefile string
	//go:embed gateway/install.sh
	gatewayInstallScript string
	//go:embed gateway/page.html
	pageHTML string
	page     = template.Must(template.New("page").Parse(pageHTML))
)

// cmdReadGateway prints gateway.env, a marker line, then the routes file.
const cmdReadGateway = "cat ~/" + gatewayEnv + " 2>/dev/null; echo ::routes; cat ~/" + gatewayRoutes + " 2>/dev/null; true"

// gateway reads the server's gateway settings and routes. A server without
// one has Mode GatewayUnset.
func (d dev) gateway(ctx context.Context) (gatewayConfig, []route) {
	out, _ := d.output(ctx, cmdReadGateway)
	env, routes, _ := strings.Cut(string(out), "::routes")
	return parseGatewayEnv(env), parseRoutes(routes)
}

// tailscaleSelf is what the server's Tailscale knows about itself.
type tailscaleSelf struct {
	ID       string   `json:"id"`       // stable node ID, which the API takes
	DNSName  string   `json:"dns"`      // e.g. devbox.tail1234.ts.net
	IP       string   `json:"ip"`       // tailnet IPv4
	Tailnet  string   `json:"tailnet"`  // MagicDNS suffix, e.g. tail1234.ts.net
	Tags     []string `json:"tags"`     // the server's tags
	HTTPS    bool     `json:"https"`    // HTTPS certificates are on in the tailnet
	MagicDNS bool     `json:"magicdns"` // MagicDNS is on
}

const cmdTailscaleSelf = `tailscale status --json | jq -c '{id: .Self.ID, dns: (.Self.DNSName // "" | rtrimstr(".")), ` +
	`ip: ((.Self.TailscaleIPs // [])[0] // ""), tailnet: (.MagicDNSSuffix // ""), tags: (.Self.Tags // []), ` +
	`https: ((.CertDomains // []) | length > 0), magicdns: (.CurrentTailnet.MagicDNSEnabled // false)}'`

func (d dev) tailscaleSelf(ctx context.Context) (tailscaleSelf, error) {
	out, err := d.output(ctx, cmdTailscaleSelf)
	var s tailscaleSelf
	if err != nil || json.Unmarshal(out, &s) != nil || s.ID == "" {
		return s, fmt.Errorf("the server isn't signed in to Tailscale. Sign it in with: outpost login %s tailscale", d.h)
	}
	return s, nil
}

// writeFilesCmd writes files under the dev user's home, each in one step
// (a temporary file, then a rename), so a reader never sees half of one.
func writeFilesCmd(files map[string]string) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	var b strings.Builder
	b.WriteString("set -e; cd ~; umask 022")
	for _, p := range paths {
		dir := p[:max(strings.LastIndex(p, "/"), 0)]
		if dir != "" {
			b.WriteString("; mkdir -p " + quote(dir))
		}
		b.WriteString("; printf %s " + quote(files[p]) + " > " + quote(p+".tmp") + " && mv " + quote(p+".tmp") + " " + quote(p))
	}
	return b.String()
}

func (d dev) writeFiles(ctx context.Context, files map[string]string) error {
	if _, err := d.output(ctx, writeFilesCmd(files)); err != nil {
		return fmt.Errorf("write the gateway's settings: %w", err)
	}
	return nil
}

// cmdClearSites removes the domain gateway's sites before they're rewritten,
// so a removed project's site goes too.
const cmdClearSites = "rm -f ~/" + gatewayDir + "/sites/*.caddy"

// The domain gateway's system services.
const (
	cmdCaddyBin       = "/usr/local/lib/outpost/bin/caddy"
	cmdGatewayStop    = "sudo systemctl disable --now outpost-caddy outpost-dns >/dev/null 2>&1; true"
	cmdGatewayUnits   = `for u in outpost-caddy outpost-dns; do echo "$u $(systemctl is-active $u)"; done`
	cmdGatewayServed  = `sudo tailscale serve status --json 2>/dev/null | jq -r '.Services // {} | keys[]'`
	cmdGatewayRootPEM = "cat ~/" + gatewayDir + "/pki/\"$GATEWAY_DOMAIN\"/root.crt"
)

// cmdCaddyReload checks the Caddyfile and, if it's good, has Caddy load it, so
// a bad change never reaches the running gateway.
const cmdCaddyReload = "set -a; . ~/" + gatewayEnv + "; set +a; cd ~/" + gatewayDir + " && " +
	cmdCaddyBin + " validate --config Caddyfile --adapter caddyfile >/dev/null 2>&1 && sudo systemctl reload outpost-caddy"

// gatewayFiles are the files a gateway's settings and routes make.
func gatewayFiles(c gatewayConfig, rs []route) map[string]string {
	files := map[string]string{gatewayEnv: c.env(), gatewayRoutes: formatRoutes(rs)}
	if c.Mode == GatewayDomain {
		files[gatewayDir+"/Caddyfile"] = caddyfile
		files[gatewayDir+"/Corefile"] = corefile
		for project, site := range caddySites(rs) {
			files[gatewayDir+"/sites/"+project+".caddy"] = site
		}
	}
	return files
}

// installDomainGateway installs Caddy and CoreDNS and makes the certificate authority.
func installDomainGateway(ctx context.Context, d dev, c gatewayConfig) error {
	body := []byte("GATEWAY_DOMAIN=" + quote(c.Domain) + "\n" + gatewayInstallScript)
	if err := runTasks(ctx, d.h, DevUser, remote.Script{Body: body, AsRoot: true}); err != nil {
		return fmt.Errorf("installing the gateway failed (see above). Run the same command again to retry: %w", err)
	}
	return nil
}

// rootCert reads the domain gateway's root certificate, as PEM.
func (d dev) rootCert(ctx context.Context, c gatewayConfig) ([]byte, error) {
	out, err := d.output(ctx, "GATEWAY_DOMAIN="+quote(c.Domain)+"; "+cmdGatewayRootPEM)
	if err != nil || !bytes.Contains(out, []byte("BEGIN CERTIFICATE")) {
		return nil, fmt.Errorf("read the gateway's certificate: %w", errors.Join(err, errors.New("not found")))
	}
	return out, nil
}

// pageFiles are the files of the page devices trust the gateway from.
func pageFiles(server, address string, c gatewayConfig, rs []route, root []byte) (map[string]string, error) {
	block, _ := pem.Decode(root)
	if block == nil {
		return nil, errors.New("the gateway's certificate isn't PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("read the gateway's certificate: %w", err)
	}
	sum := sha256.Sum256(block.Bytes)
	pairs := make([]string, len(sum))
	for i, b := range sum {
		pairs[i] = fmt.Sprintf("%02X", b)
	}
	// Each project once, with the extra names it uses (say, its file storage) beside it.
	type project struct {
		Name, URL string
		Also      []string
	}
	var ps []project
	for _, p := range projects(rs) {
		var pr project
		for _, r := range rs {
			switch {
			case r.Project != p:
			case r.Name == p:
				pr.Name, pr.URL = p, c.url(r)
			default:
				pr.Also = append(pr.Also, c.host(r))
			}
		}
		if pr.Name == "" { // a project with only extra names: show its first one
			pr.Name, pr.URL, pr.Also = p, "https://"+pr.Also[0], pr.Also[1:]
		}
		ps = append(ps, pr)
	}
	var html bytes.Buffer
	if err := page.Execute(&html, map[string]any{
		"Domain": c.Domain, "Server": server, "Address": address, "Projects": ps,
		"Expires": cert.NotAfter.Format("2 January 2006"), "Fingerprint": strings.Join(pairs, " "),
	}); err != nil {
		return nil, err
	}
	profile, err := mobileconfig(c.Domain, root)
	if err != nil {
		return nil, err
	}
	www := gatewayDir + "/www/"
	return map[string]string{www + "index.html": html.String(), www + "root.crt": string(root), www + "outpost.mobileconfig": profile}, nil
}

// mobileconfig is an Apple configuration profile that installs the root, so
// iPhones, iPads and Macs can trust it with a tap.
func mobileconfig(domain string, rootPEM []byte) (string, error) {
	block, _ := pem.Decode(rootPEM)
	if block == nil {
		return "", errors.New("the gateway's certificate isn't PEM")
	}
	sum := sha256.Sum256(block.Bytes)
	uuid := func(salt byte) string {
		b := sum
		b[0] ^= salt
		return fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
	}
	esc := func(s string) string { var b bytes.Buffer; template.HTMLEscape(&b, []byte(s)); return b.String() }
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadCertificateFileName</key><string>outpost-` + esc(domain) + `.crt</string>
			<key>PayloadContent</key><data>` + base64.StdEncoding.EncodeToString(block.Bytes) + `</data>
			<key>PayloadDisplayName</key><string>outpost ` + esc(domain) + `</string>
			<key>PayloadIdentifier</key><string>dev.outpost.gateway.` + esc(domain) + `.root</string>
			<key>PayloadType</key><string>com.apple.security.root</string>
			<key>PayloadUUID</key><string>` + uuid(1) + `</string>
			<key>PayloadVersion</key><integer>1</integer>
		</dict>
	</array>
	<key>PayloadDescription</key><string>Trusts outpost's certificate for names under ` + esc(domain) + `. It can't vouch for any other name.</string>
	<key>PayloadDisplayName</key><string>outpost: HTTPS for ` + esc(domain) + `</string>
	<key>PayloadIdentifier</key><string>dev.outpost.gateway.` + esc(domain) + `</string>
	<key>PayloadType</key><string>Configuration</string>
	<key>PayloadUUID</key><string>` + uuid(2) + `</string>
	<key>PayloadVersion</key><integer>1</integer>
</dict>
</plist>
`, nil
}

// probeCmd asks a route's name for its front page from the server itself,
// through the same front door devices use, and prints the HTTP status. The
// first request to a new Tailscale name waits while its certificate is made.
func probeCmd(c gatewayConfig, r route) string {
	host := c.host(r)
	if c.Mode == GatewayDomain {
		return fmt.Sprintf(`curl -s -o /dev/null -w '%%{http_code}' --max-time 20 --cacert ~/%s/pki/%s/root.crt --resolve %s:%d:127.0.0.1 https://%[3]s:%[4]d/`,
			gatewayDir, c.Domain, host, gatewayHTTPS)
	}
	return fmt.Sprintf(`curl -s -o /dev/null -w '%%{http_code}' --max-time 45 --retry 3 --retry-all-errors --retry-delay 3 https://%s/`, host)
}

// probe reports how a route answers: "" when its project answers, otherwise what's wrong.
func (d dev) probe(ctx context.Context, c gatewayConfig, r route) string {
	out, _ := d.output(ctx, probeCmd(c, r))
	switch code := strings.TrimSpace(string(out)); {
	case code == "502" || code == "503" || code == "504":
		return fmt.Sprintf("nothing answers on port %d yet; start the project and it will", r.Port)
	case code == "" || code == "000":
		return "doesn't answer yet; Tailscale can take a minute to publish a new name"
	}
	return ""
}

// gatewayDown lists the parts of an active gateway that aren't running.
func (d dev) gatewayDown(ctx context.Context, c gatewayConfig, rs []route) []string {
	var down []string
	if c.Mode == GatewayDomain {
		out, _ := d.output(ctx, cmdGatewayUnits)
		for line := range strings.Lines(string(out)) {
			if name, state, _ := strings.Cut(strings.TrimSpace(line), " "); state != "active" {
				down = append(down, name+" is "+state)
			}
		}
	}
	served, _ := d.output(ctx, cmdGatewayServed)
	have := strings.Fields(string(served))
	for svc := range c.services(rs) {
		if !slices.Contains(have, svc) {
			down = append(down, svc+" isn't served")
		}
	}
	slices.Sort(down)
	return down
}
