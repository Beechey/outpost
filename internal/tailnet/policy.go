// Package tailnet does the Tailscale admin steps a gateway needs, through the
// Tailscale API: the policy additions, tagging the server, defining and
// approving Services, and split DNS. Callers describe the state they want
// (Want); the order of the steps and their error cases stay in here.
package tailnet

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tailscale/hujson"
)

// Tag is the tag outpost gives gateway servers. Tailscale only lets tagged
// machines host Services.
const Tag = "tag:devbox"

// self is the policy's word for "a user's own devices"; tagging a server takes
// it out of that group, so rules that reached it through self need a copy.
const self = "autogroup:self"

// PatchPolicy adds what a gateway server needs to a policy file: an owner for
// tag, and for each kind of rule (ssh, acls, grants) that reaches devices
// through autogroup:self and has no rule for tag yet, a copy that reaches tag
// instead, so tagging the server doesn't cut off SSH or access to it. Comments
// and layout are kept. added describes each addition, and is empty when the
// policy already has everything.
func PatchPolicy(policy []byte, tag string) (patched []byte, added []string, err error) {
	v, err := hujson.Parse(policy)
	if err != nil {
		return nil, nil, fmt.Errorf("read your policy file: %w", err)
	}
	std := v.Clone()
	std.Standardize()
	var p map[string]json.RawMessage
	if err := json.Unmarshal(std.Pack(), &p); err != nil {
		return nil, nil, fmt.Errorf("read your policy file: %w", err)
	}

	type op struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value any    `json:"value"`
	}
	var ops []op
	owners := map[string]json.RawMessage{}
	if raw, ok := p["tagOwners"]; ok {
		if err := json.Unmarshal(raw, &owners); err != nil {
			return nil, nil, fmt.Errorf("read your policy's tagOwners: %w", err)
		}
	}
	if _, ok := owners[tag]; !ok {
		owner := []string{"autogroup:admin"}
		if _, ok := p["tagOwners"]; ok {
			ops = append(ops, op{"add", "/tagOwners/" + pointer(tag), owner})
		} else {
			ops = append(ops, op{"add", "/tagOwners", map[string]any{tag: owner}})
		}
		added = append(added, fmt.Sprintf(`tagOwners: %q may be given by admins`, tag))
	}

	for _, section := range []string{"ssh", "acls", "grants"} {
		raw, ok := p[section]
		if !ok {
			continue
		}
		var rules []map[string]any
		if err := json.Unmarshal(raw, &rules); err != nil {
			return nil, nil, fmt.Errorf("read your policy's %s: %w", section, err)
		}
		if reaches(rules, tag) {
			continue // the policy already says who reaches tagged servers; leave it be
		}
		for _, r := range rules {
			c, ok := retarget(r, tag)
			if !ok {
				continue
			}
			ops = append(ops, op{"add", "/" + section + "/-", c})
			js, _ := json.Marshal(c)
			added = append(added, section+": "+string(js))
		}
	}
	if len(ops) == 0 {
		return policy, nil, nil
	}
	patch, err := json.Marshal(ops)
	if err != nil {
		return nil, nil, err
	}
	if err := v.Patch(patch); err != nil {
		return nil, nil, fmt.Errorf("update your policy file: %w", err)
	}
	v.Format()
	return v.Pack(), added, nil
}

// reaches reports whether any rule already has tag as a destination.
func reaches(rules []map[string]any, tag string) bool {
	for _, r := range rules {
		for _, d := range dsts(r) {
			if d == tag || strings.HasPrefix(d, tag+":") {
				return true
			}
		}
	}
	return false
}

// retarget copies a rule that reaches autogroup:self so it reaches tag
// instead, keeping any port suffix (acls write "autogroup:self:*").
func retarget(r map[string]any, tag string) (map[string]any, bool) {
	var to []any
	for _, d := range dsts(r) {
		if d == self || strings.HasPrefix(d, self+":") {
			to = append(to, tag+strings.TrimPrefix(d, self))
		}
	}
	if len(to) == 0 {
		return nil, false
	}
	c := make(map[string]any, len(r))
	for k, v := range r {
		c[k] = v
	}
	c["dst"] = to
	return c, true
}

func dsts(r map[string]any) []string {
	list, _ := r["dst"].([]any)
	var out []string
	for _, d := range list {
		if s, ok := d.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// pointer escapes a key for a JSON Pointer (RFC 6901).
func pointer(key string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(key)
}
