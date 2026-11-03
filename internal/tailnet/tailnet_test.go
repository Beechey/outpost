package tailnet

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/tailscale/hujson"
)

// defaultPolicy is the policy a new tailnet starts with, comments and all.
const defaultPolicy = `// Example/default ACLs for unrestricted connections.
{
	// Declare static groups of users.
	"groups": {},

	"acls": [
		// Allow all connections.
		{"action": "accept", "src": ["*"], "dst": ["*:*"]},
	],

	// Test access rules every time they're saved.
	"ssh": [
		{
			"action": "check",
			"src":    ["autogroup:member"],
			"dst":    ["autogroup:self"],
			"users":  ["autogroup:nonroot", "root"],
		},
	],
}
`

func standard(t *testing.T, b []byte) map[string]any {
	t.Helper()
	v, err := hujson.Parse(b)
	if err != nil {
		t.Fatalf("not HuJSON: %v\n%s", err, b)
	}
	v.Standardize()
	var m map[string]any
	if err := json.Unmarshal(v.Pack(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPatchPolicyDefault(t *testing.T) {
	out, added, err := PatchPolicy([]byte(defaultPolicy), Tag)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 2 {
		t.Fatalf("want a tag owner and an SSH rule, got %q", added)
	}
	if !strings.Contains(string(out), "// Allow all connections.") {
		t.Error("comments must be kept")
	}
	p := standard(t, out)
	if owners := p["tagOwners"].(map[string]any)[Tag]; owners == nil {
		t.Error("tag owner missing")
	}
	ssh := p["ssh"].([]any)
	if len(ssh) != 2 {
		t.Fatalf("want the original SSH rule plus a copy, got %v", ssh)
	}
	c := ssh[1].(map[string]any)
	if c["dst"].([]any)[0] != Tag || c["action"] != "check" {
		t.Errorf("copy should reach %s the same way: %v", Tag, c)
	}
	if len(p["acls"].([]any)) != 1 {
		t.Error("an allow-all ACL reaches tagged servers already; leave it alone")
	}

	again, more, err := PatchPolicy(out, Tag)
	if err != nil || len(more) != 0 || string(again) != string(out) {
		t.Errorf("a patched policy needs nothing more, got %q", more)
	}
}

func TestPatchPolicySelfACLsAndExistingOwners(t *testing.T) {
	in := `{
	"tagOwners": {"tag:ci": ["autogroup:admin"]},
	"acls": [{"action": "accept", "src": ["autogroup:member"], "dst": ["autogroup:self:*"]}],
	"grants": [{"src": ["autogroup:member"], "dst": ["autogroup:self"], "ip": ["*"]}],
}`
	out, added, err := PatchPolicy([]byte(in), Tag)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 3 {
		t.Fatalf("want an owner, an ACL and a grant, got %q", added)
	}
	p := standard(t, []byte(out))
	if p["tagOwners"].(map[string]any)["tag:ci"] == nil {
		t.Error("existing tag owners must stay")
	}
	if dst := p["acls"].([]any)[1].(map[string]any)["dst"].([]any)[0]; dst != Tag+":*" {
		t.Errorf("ACL copy should keep the port: %v", dst)
	}
	if dst := p["grants"].([]any)[1].(map[string]any)["dst"].([]any)[0]; dst != Tag {
		t.Errorf("grant copy: %v", dst)
	}
}

func TestPatchPolicyLeavesRulesForTagAlone(t *testing.T) {
	in := `{"tagOwners": {"tag:devbox": ["autogroup:admin"]},
	"ssh": [{"action": "accept", "src": ["autogroup:admin"], "dst": ["tag:devbox"], "users": ["dev"]},
	        {"action": "check", "src": ["autogroup:member"], "dst": ["autogroup:self"], "users": ["root"]}]}`
	_, added, err := PatchPolicy([]byte(in), Tag)
	if err != nil || len(added) != 0 {
		t.Fatalf("the user already decided who reaches tagged servers; got %q, %v", added, err)
	}
}

func TestPatchPolicyRejectsBrokenPolicy(t *testing.T) {
	if _, _, err := PatchPolicy([]byte(`{"ssh": [`), Tag); err == nil {
		t.Fatal("want an error")
	}
}

// fakeAPI is a tiny Tailscale API: a policy, one device, Services and split DNS.
type fakeAPI struct {
	mu       sync.Mutex
	policy   string
	etag     string
	tags     []string
	services map[string][]string
	approved map[string]bool
	split    map[string]any
	features map[string]bool
	calls    []string
	deny     string // a path prefix that answers 403
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{policy: defaultPolicy, etag: `"1"`, services: map[string][]string{}, approved: map[string]bool{}, split: map[string]any{}, features: map[string]bool{}}
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/api/v2")
	f.calls = append(f.calls, r.Method+" "+path)
	if path == "/oauth/token" {
		r.ParseForm()
		if r.PostForm.Get("client_secret") != "tskey-client-ok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		io.WriteString(w, `{"access_token":"good"}`)
		return
	}
	if r.Header.Get("Authorization") != "Bearer good" {
		http.Error(w, `{"message":"bad key"}`, http.StatusUnauthorized)
		return
	}
	if f.deny != "" && strings.HasPrefix(path, f.deny) {
		http.Error(w, `{"message":"forbidden"}`, http.StatusForbidden)
		return
	}
	body, _ := io.ReadAll(r.Body)
	switch {
	case path == "/tailnet/-/acl" && r.Method == http.MethodGet:
		w.Header().Set("ETag", f.etag)
		io.WriteString(w, f.policy)
	case path == "/tailnet/-/acl" && r.Method == http.MethodPost:
		if r.Header.Get("If-Match") != f.etag {
			http.Error(w, `{"message":"precondition failed"}`, http.StatusPreconditionFailed)
			return
		}
		f.policy, f.etag = string(body), `"2"`
	case path == "/device/node1" && r.Method == http.MethodGet:
		json.NewEncoder(w).Encode(map[string]any{"tags": f.tags})
	case path == "/device/node1/tags":
		var b struct{ Tags []string }
		json.Unmarshal(body, &b)
		f.tags = b.Tags
	case strings.HasSuffix(path, "/approved"):
		f.approved[strings.Split(path, "/")[4]] = true
	case strings.HasPrefix(path, "/tailnet/-/services/"):
		name := strings.TrimPrefix(path, "/tailnet/-/services/")
		switch r.Method {
		case http.MethodGet:
			ports, ok := f.services[name]
			if !ok {
				http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"name": name, "ports": ports, "addrs": []string{"100.64.0.9", "fd7a::9"}})
		case http.MethodPut:
			var b struct{ Ports []string }
			json.Unmarshal(body, &b)
			f.services[name] = b.Ports
			json.NewEncoder(w).Encode(map[string]any{"name": name, "ports": b.Ports, "addrs": []string{"100.64.0.9", "fd7a::9"}})
		case http.MethodDelete:
			if _, ok := f.services[name]; !ok {
				http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
				return
			}
			delete(f.services, name)
		}
	case path == "/tailnet/-/dns/preferences" || path == "/tailnet/-/settings":
		var b map[string]bool
		json.Unmarshal(body, &b)
		for k, v := range b {
			f.features[k] = v
		}
	case path == "/tailnet/-/services":
		io.WriteString(w, `{"vipServices":[]}`)
	case path == "/tailnet/-/dns/split-dns":
		var b map[string]any
		json.Unmarshal(body, &b)
		for k, v := range b {
			if v == nil {
				delete(f.split, k)
			} else {
				f.split[k] = v
			}
		}
	default:
		http.NotFound(w, r)
	}
}

func client(t *testing.T, f *fakeAPI, key string) *Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := New(key)
	c.Base = srv.URL + "/api/v2"
	return c
}

var want = Want{
	Device:   "node1",
	Services: map[string][]string{"svc:myapp-devbox": {"tcp:443"}},
	SplitDNS: map[string]string{"devbox.test": "100.100.1.1"},
}

func TestPrepareAndFinish(t *testing.T) {
	ctx := context.Background()
	f := newFakeAPI()
	f.tags = []string{"tag:other"}
	c := client(t, f, "good")

	var shown []string
	addrs, err := c.Prepare(ctx, want, func(added []string) error { shown = added; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(shown) == 0 || !strings.Contains(f.policy, Tag) {
		t.Error("policy additions should be shown, then saved")
	}
	if strings.Join(f.tags, ",") != "tag:other,"+Tag {
		t.Errorf("tags should be kept and added to: %v", f.tags)
	}
	if addrs["svc:myapp-devbox"] != "100.64.0.9" {
		t.Errorf("want the Service's IPv4, got %v", addrs)
	}
	if err := c.Finish(ctx, want); err != nil {
		t.Fatal(err)
	}
	if !f.approved["svc:myapp-devbox"] || f.split["devbox.test"] == nil {
		t.Errorf("approval and split DNS: %v %v", f.approved, f.split)
	}

	// A second run changes nothing it doesn't need to.
	f.calls = nil
	if _, err := c.Prepare(ctx, want, func([]string) error { t.Error("nothing new to confirm"); return nil }); err != nil {
		t.Fatal(err)
	}
	for _, call := range f.calls {
		if !strings.HasPrefix(call, "GET") {
			t.Errorf("re-run should only read, but sent %s", call)
		}
	}
}

func TestPrepareStopsWhenPolicyRefused(t *testing.T) {
	f := newFakeAPI()
	c := client(t, f, "good")
	no := errors.New("cancelled")
	if _, err := c.Prepare(context.Background(), want, func([]string) error { return no }); !errors.Is(err, no) {
		t.Fatalf("want the refusal, got %v", err)
	}
	if strings.Contains(f.policy, Tag) || len(f.tags) > 0 || len(f.services) > 0 {
		t.Error("nothing should change after a refusal")
	}
}

func TestPolicyChangedMeanwhile(t *testing.T) {
	f := newFakeAPI()
	c := client(t, f, "good")
	_, err := c.Prepare(context.Background(), want, func([]string) error {
		f.mu.Lock()
		f.etag = `"someone else"`
		f.mu.Unlock()
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "changed while") {
		t.Fatalf("want a changed-meanwhile error, got %v", err)
	}
}

func TestForget(t *testing.T) {
	ctx := context.Background()
	f := newFakeAPI()
	f.services["svc:myapp-devbox"] = []string{"tcp:443"}
	f.split["devbox.test"] = []any{"100.100.1.1"}
	c := client(t, f, "good")
	if err := c.Forget(ctx, []string{"svc:myapp-devbox", "svc:gone"}, []string{"devbox.test"}); err != nil {
		t.Fatal(err)
	}
	if len(f.services) != 0 || len(f.split) != 0 {
		t.Errorf("should be gone: %v %v", f.services, f.split)
	}
}

func TestKeyErrors(t *testing.T) {
	ctx := context.Background()
	if err := client(t, newFakeAPI(), "wrong").Verify(ctx); !errors.Is(err, ErrBadKey) {
		t.Errorf("want ErrBadKey, got %v", err)
	}
	f := newFakeAPI()
	f.deny = "/tailnet/-/acl"
	_, err := client(t, f, "good").Prepare(ctx, want, nil)
	if err == nil || !strings.Contains(err.Error(), "policy_file") {
		t.Errorf("a 403 should name the scopes, got %v", err)
	}
}

func TestTrustCredential(t *testing.T) {
	if err := client(t, newFakeAPI(), "tskey-client-ok").Verify(context.Background()); err != nil {
		t.Fatalf("a trust credential should be exchanged for a token: %v", err)
	}
	if err := client(t, newFakeAPI(), "tskey-client-bad").Verify(context.Background()); !errors.Is(err, ErrBadKey) {
		t.Errorf("want ErrBadKey, got %v", err)
	}
}

func TestPrepareTurnsOnFeatures(t *testing.T) {
	f := newFakeAPI()
	w := want
	w.MagicDNS, w.HTTPS = true, true
	if _, err := client(t, f, "good").Prepare(context.Background(), w, nil); err != nil {
		t.Fatal(err)
	}
	if !f.features["magicDNS"] || !f.features["httpsEnabled"] {
		t.Errorf("MagicDNS and HTTPS certificates should be turned on: %v", f.features)
	}
}
