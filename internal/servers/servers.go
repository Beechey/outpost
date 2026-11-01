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

	"charm.land/huh/v2"
	"github.com/charmbracelet/x/term"

	"github.com/Beechey/outpost/internal/remote"
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
	if !term.IsTerminal(os.Stdin.Fd()) {
		return remote.Server{}, errors.New("give the server's address, for example 203.0.113.7 or 203.0.113.7:2222")
	}
	known := List()
	for _, e := range exclude {
		known = without(known, e)
	}

	const another = ""
	choice := another
	if len(known) > 0 {
		options := make([]huh.Option[string], 0, len(known)+1)
		for _, srv := range known {
			options = append(options, huh.NewOption(srv.String(), srv.String()))
		}
		options = append(options, huh.NewOption("Another server…", another))
		err := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().Title(title).Options(options...).Value(&choice),
		)).RunWithContext(ctx)
		if err != nil {
			return remote.Server{}, err
		}
	}
	if choice == another {
		err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title(title).
				Description("Its IP address or hostname, with :port if SSH isn't on 22.").
				Placeholder("203.0.113.7").
				Validate(func(s string) error { _, err := Parse(s); return err }).
				Value(&choice),
		)).RunWithContext(ctx)
		if err != nil {
			return remote.Server{}, err
		}
	}
	return Parse(choice)
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
