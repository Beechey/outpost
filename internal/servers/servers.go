// Package servers remembers the servers outpost has set up, so commands can
// offer them in a list instead of asking for an address.
package servers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Beechey/outpost/internal/remote"
	"github.com/Beechey/outpost/internal/ui"
)

// Parse reads a server written as "host" or "host:port". An IPv6 address
// can't carry a port this way; use ~/.ssh/config for that.
func Parse(s string) (remote.Server, error) {
	srv := remote.Server{Addr: strings.TrimSpace(s)}
	if host, port, ok := strings.Cut(srv.Addr, ":"); ok && !strings.Contains(port, ":") {
		p, err := strconv.Atoi(port)
		if err != nil {
			return srv, fmt.Errorf("%q isn't a valid port", port)
		}
		srv = remote.Server{Addr: host, Port: p}
	}
	return srv, srv.Validate()
}

// List returns the remembered servers, most recently used first.
func List() []remote.Server {
	path, err := file()
	if err != nil {
		return nil
	}
	data, _ := os.ReadFile(path)
	var list []remote.Server
	for line := range strings.Lines(string(data)) {
		if srv, err := Parse(line); err == nil {
			list = append(list, srv)
		}
	}
	return list
}

// Remember records srv as the most recently used server.
func Remember(srv remote.Server) error {
	return save(append([]remote.Server{srv}, without(List(), srv)...))
}

// Forget removes srv, for example after its work moved elsewhere.
func Forget(srv remote.Server) error {
	return save(without(List(), srv))
}

// Pick asks which server to use: one from the list (leaving out exclude), or a new address.
func Pick(ctx context.Context, title string, exclude ...remote.Server) (remote.Server, error) {
	known := List()
	for _, e := range exclude {
		known = without(known, e)
	}

	const another = ""
	choice := another
	if len(known) > 0 {
		options := make([]ui.Option, 0, len(known)+1)
		for _, srv := range known {
			options = append(options, ui.Option{Label: srv.String(), Value: srv.String()})
		}
		options = append(options, ui.Option{Label: "Another server…", Value: another})
		var err error
		if choice, err = ui.Ask.Choose(ctx, title, options, known[0].String()); err != nil {
			return remote.Server{}, pickErr(err)
		}
	}
	if choice == another {
		var err error
		choice, err = ui.Ask.Input(ctx, title, "Its IP address or hostname, with :port if SSH isn't on 22.", "203.0.113.7",
			func(s string) error { _, err := Parse(s); return err })
		if err != nil {
			return remote.Server{}, pickErr(err)
		}
	}
	return Parse(choice)
}

func pickErr(err error) error {
	if errors.Is(err, ui.ErrNoTerminal) {
		return errors.New("give the server's address, for example 203.0.113.7 or 203.0.113.7:2222")
	}
	return err
}

func without(list []remote.Server, srv remote.Server) []remote.Server {
	return slices.DeleteFunc(slices.Clone(list), func(s remote.Server) bool { return s.String() == srv.String() })
}

func save(list []remote.Server) error {
	path, err := file()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var b strings.Builder
	for _, srv := range list {
		b.WriteString(srv.String() + "\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func file() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "outpost", "servers"), nil
}
