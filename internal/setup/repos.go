package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"charm.land/huh/v2"
	"github.com/charmbracelet/x/term"

	"github.com/Beechey/outpost/internal/remote"
)

type ghRepo struct {
	FullName    string `json:"full_name"`
	Private     bool   `json:"private"`
	Description string `json:"description"`
}

// pickRepos lists the GitHub repos the server's gh login can see, newest
// pushes first, and lets the user tick the ones to set up.
func pickRepos(ctx context.Context, srv remote.Server) ([]string, error) {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return nil, nil
	}
	// Personal, collaborator and organization repos, most recently pushed first, one JSON object per line.
	out, err := srv.Output(ctx, DevUser, `gh api --paginate `+
		`'user/repos?affiliation=owner,collaborator,organization_member&sort=pushed&per_page=100' `+
		`--jq '.[] | {full_name, private, description}'`)
	if err != nil {
		return nil, fmt.Errorf("list your GitHub repos: %w", err)
	}
	var list []ghRepo
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var r ghRepo
		if err := dec.Decode(&r); err != nil {
			return nil, fmt.Errorf("read GitHub's repo list: %w", err)
		}
		list = append(list, r)
	}
	if len(list) == 0 {
		return nil, nil
	}

	options := make([]huh.Option[string], 0, len(list))
	for _, r := range list {
		options = append(options, huh.NewOption(repoLabel(r), r.FullName))
	}
	var chosen []string
	form := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Which repos should your agents work on?").
			Description("Pick as many as you like, or none. Missing an org's repos?\n" +
				"An org owner may need to approve the GitHub CLI app.").
			Options(options...).
			Filterable(true).
			Height(min(len(options)+5, 20)). // + title and description lines
			Value(&chosen),
	))
	if err := form.RunWithContext(ctx); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil, nil
		}
		return nil, err
	}
	return chosen, validateRepos(chosen)
}

func repoLabel(r ghRepo) string {
	label := r.FullName
	if r.Private {
		label += " (private)"
	}
	if d := []rune(r.Description); len(d) > 0 {
		if len(d) > 50 {
			d = append(d[:49], '…')
		}
		label += " · " + string(d)
	}
	return label
}
