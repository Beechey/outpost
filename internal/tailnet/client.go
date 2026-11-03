package tailnet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Want is the tailnet state a gateway needs.
type Want struct {
	Device   string              // the server's stable node ID
	Services map[string][]string // Service name ("svc:myapp-devbox") → its ports ("tcp:443")
	SplitDNS map[string]string   // domain → the nameserver that answers for it
	MagicDNS bool                // turn MagicDNS on, if it's off
	HTTPS    bool                // turn HTTPS certificates on, if they're off
}

// Client changes one tailnet through the Tailscale API, with an API access
// token (tskey-api-…) or a trust credential's secret (tskey-client-…).
type Client struct {
	Base string // the API root; tests point it at a local server
	HTTP *http.Client

	key, token string
}

// New returns a client for the tailnet key belongs to.
func New(key string) *Client {
	return &Client{Base: "https://api.tailscale.com/api/v2", HTTP: &http.Client{Timeout: 30 * time.Second}, key: strings.TrimSpace(key)}
}

// ErrBadKey means Tailscale didn't accept the key: wrong, revoked or expired.
var ErrBadKey = errors.New("Tailscale didn't accept the API key: it may be mistyped, revoked or expired")

// Verify checks the key works, by listing the tailnet's Services.
func (c *Client) Verify(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/tailnet/-/services", nil, nil, nil)
}

// Prepare makes the tailnet ready for the server to serve w's Services: the
// policy additions (shown to confirm first; nil applies them without asking),
// the server's tag, and each Service. It returns each Service's tailnet IPv4.
func (c *Client) Prepare(ctx context.Context, w Want, confirm func(added []string) error) (map[string]string, error) {
	if w.MagicDNS {
		if err := c.do(ctx, http.MethodPost, "/tailnet/-/dns/preferences", map[string]bool{"magicDNS": true}, nil, nil); err != nil {
			return nil, fmt.Errorf("turn on MagicDNS: %w", err)
		}
	}
	if w.HTTPS {
		if err := c.do(ctx, http.MethodPatch, "/tailnet/-/settings", map[string]bool{"httpsEnabled": true}, nil, nil); err != nil {
			return nil, fmt.Errorf("turn on HTTPS certificates: %w", err)
		}
	}
	if err := c.policy(ctx, confirm); err != nil {
		return nil, err
	}
	if err := c.tag(ctx, w.Device); err != nil {
		return nil, err
	}
	addrs := map[string]string{}
	for _, name := range sortedKeys(w.Services) {
		addr, err := c.service(ctx, name, w.Services[name])
		if err != nil {
			return nil, err
		}
		addrs[name] = addr
	}
	return addrs, nil
}

// Finish approves the server as each Service's host, once it serves them,
// and points split DNS at the nameservers.
func (c *Client) Finish(ctx context.Context, w Want) error {
	for _, name := range sortedKeys(w.Services) {
		body := map[string]bool{"approved": true}
		if err := c.do(ctx, http.MethodPost, "/tailnet/-/services/"+name+"/device/"+url.PathEscape(w.Device)+"/approved", body, nil, nil); err != nil {
			return fmt.Errorf("approve %s as the host of %s: %w", w.Device, name, err)
		}
	}
	return c.splitDNS(ctx, w.SplitDNS)
}

// Forget deletes Services and clears split DNS for domains. Ones already gone are fine.
func (c *Client) Forget(ctx context.Context, services []string, domains []string) error {
	for _, name := range services {
		err := c.do(ctx, http.MethodDelete, "/tailnet/-/services/"+name, nil, nil, nil)
		if err != nil && !isStatus(err, http.StatusNotFound) {
			return fmt.Errorf("delete %s: %w", name, err)
		}
	}
	clear := map[string]string{}
	for _, d := range domains {
		clear[d] = ""
	}
	return c.splitDNS(ctx, clear)
}

func (c *Client) policy(ctx context.Context, confirm func([]string) error) error {
	var policy bytes.Buffer
	h, err := c.raw(ctx, http.MethodGet, "/tailnet/-/acl", nil, map[string]string{"Accept": "application/hujson"}, &policy)
	if err != nil {
		return fmt.Errorf("read your tailnet policy: %w", err)
	}
	patched, added, err := PatchPolicy(policy.Bytes(), Tag)
	if err != nil || len(added) == 0 {
		return err
	}
	if confirm != nil {
		if err := confirm(added); err != nil {
			return err
		}
	}
	// If-Match: if someone edits the policy meanwhile, fail rather than overwrite their change.
	hdr := map[string]string{"Content-Type": "application/hujson", "Accept": "application/hujson", "If-Match": h.Get("ETag")}
	if _, err := c.raw(ctx, http.MethodPost, "/tailnet/-/acl", bytes.NewReader(patched), hdr, io.Discard); err != nil {
		if isStatus(err, http.StatusPreconditionFailed) {
			return errors.New("your tailnet policy changed while outpost was updating it. Run the same command again")
		}
		return fmt.Errorf("update your tailnet policy: %w", err)
	}
	return nil
}

func (c *Client) tag(ctx context.Context, device string) error {
	var d struct {
		Tags []string `json:"tags"`
	}
	if err := c.do(ctx, http.MethodGet, "/device/"+url.PathEscape(device), nil, &d, nil); err != nil {
		return fmt.Errorf("look up the server in your tailnet: %w", err)
	}
	if slices.Contains(d.Tags, Tag) {
		return nil
	}
	body := map[string][]string{"tags": append(d.Tags, Tag)}
	if err := c.do(ctx, http.MethodPost, "/device/"+url.PathEscape(device)+"/tags", body, nil, nil); err != nil {
		return fmt.Errorf("tag the server %s: %w", Tag, err)
	}
	return nil
}

// service makes sure a Service exists with these ports, returning its IPv4.
func (c *Client) service(ctx context.Context, name string, ports []string) (string, error) {
	var s struct {
		Addrs []string `json:"addrs"`
		Ports []string `json:"ports"`
	}
	err := c.do(ctx, http.MethodGet, "/tailnet/-/services/"+name, nil, &s, nil)
	switch {
	case err == nil && sameSet(s.Ports, ports):
		return ipv4(s.Addrs), nil
	case err != nil && !isStatus(err, http.StatusNotFound):
		return "", fmt.Errorf("look up %s: %w", name, err)
	}
	body := map[string]any{"name": name, "ports": ports, "comment": "Made by outpost"}
	if len(s.Addrs) > 0 {
		body["addrs"] = s.Addrs // keep its address when only the ports change
	}
	if err := c.do(ctx, http.MethodPut, "/tailnet/-/services/"+name, body, &s, nil); err != nil {
		return "", fmt.Errorf("define %s: %w", name, err)
	}
	return ipv4(s.Addrs), nil
}

func (c *Client) splitDNS(ctx context.Context, m map[string]string) error {
	if len(m) == 0 {
		return nil
	}
	body := map[string]any{}
	for domain, ns := range m {
		if ns == "" {
			body[domain] = nil
		} else {
			body[domain] = []string{ns}
		}
	}
	if err := c.do(ctx, http.MethodPatch, "/tailnet/-/dns/split-dns", body, nil, nil); err != nil {
		return fmt.Errorf("set split DNS: %w", err)
	}
	return nil
}

// do sends body as JSON and reads a JSON reply into out, when they're set.
func (c *Client) do(ctx context.Context, method, path string, body, out any, hdr map[string]string) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	var buf bytes.Buffer
	if _, err := c.raw(ctx, method, path, r, merge(map[string]string{"Content-Type": "application/json"}, hdr), &buf); err != nil {
		return err
	}
	if out != nil && buf.Len() > 0 {
		return json.Unmarshal(buf.Bytes(), out)
	}
	return nil
}

func (c *Client) raw(ctx context.Context, method, path string, body io.Reader, hdr map[string]string, w io.Writer) (http.Header, error) {
	token, err := c.auth(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range hdr {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, apiError(resp.StatusCode, msg)
	}
	_, err = io.Copy(w, resp.Body)
	return resp.Header, err
}

// auth returns the bearer token: the key itself, or for a trust credential,
// an access token it's exchanged for once.
func (c *Client) auth(ctx context.Context) (string, error) {
	if !strings.HasPrefix(c.key, "tskey-client-") {
		return c.key, nil
	}
	if c.token != "" {
		return c.token, nil
	}
	form := url.Values{"client_secret": {c.key}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", ErrBadKey
	}
	var t struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil || t.AccessToken == "" {
		return "", ErrBadKey
	}
	c.token = t.AccessToken
	return c.token, nil
}

// StatusError is an error reply from the API.
type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string {
	if e.Status == http.StatusForbidden {
		return "the API key isn't allowed to do this. Use a key from an admin, or give the trust credential " +
			"the policy_file, devices:core, services, dns and networking_settings scopes (" + e.Message + ")"
	}
	return fmt.Sprintf("Tailscale said %d: %s", e.Status, e.Message)
}

func apiError(status int, body []byte) error {
	if status == http.StatusUnauthorized {
		return ErrBadKey
	}
	var m struct {
		Message string `json:"message"`
	}
	msg := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &m) == nil && m.Message != "" {
		msg = m.Message
	}
	return &StatusError{Status: status, Message: msg}
}

func isStatus(err error, status int) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Status == status
}

func ipv4(addrs []string) string {
	for _, a := range addrs {
		if !strings.Contains(a, ":") {
			return a
		}
	}
	return ""
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func merge(a, b map[string]string) map[string]string {
	for k, v := range b {
		a[k] = v
	}
	return a
}
