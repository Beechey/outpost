// Package remote runs commands on a server through the system ssh binary, so
// the user's keys, agent and ~/.ssh/config work as usual.
package remote

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Server is an SSH-reachable machine.
type Server struct {
	Host string
	Port int
}

var baseOpts = []string{"-o", "StrictHostKeyChecking=accept-new", "-o", "ConnectTimeout=10"}

func (s Server) sshArgs(extra ...string) []string {
	args := append([]string{"-p", strconv.Itoa(s.Port)}, baseOpts...)
	return append(args, extra...)
}

func (s Server) command(ctx context.Context, user string, opts []string, remoteCmd ...string) *exec.Cmd {
	args := append(s.sshArgs(opts...), user+"@"+s.Host)
	return exec.CommandContext(ctx, "ssh", append(args, remoteCmd...)...)
}

// CanLogin reports whether user can log in with a key, without prompting.
func (s Server) CanLogin(ctx context.Context, user string) bool {
	return s.command(ctx, user, []string{"-o", "BatchMode=yes"}, "true").Run() == nil
}

// CopyID installs the local public key for user, prompting for their password.
func (s Server) CopyID(ctx context.Context, user string) error {
	cmd := exec.CommandContext(ctx, "ssh-copy-id", append(s.sshArgs(), user+"@"+s.Host)...)
	return attach(cmd).Run()
}

// Run runs remoteCmd and streams its output.
func (s Server) Run(ctx context.Context, user, remoteCmd string) error {
	cmd := s.command(ctx, user, nil, remoteCmd)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// RunScript uploads script and runs it as `runner <path> args...` with a
// terminal attached, so the script can prompt. The remote shell sees args
// unquoted, so callers must validate them.
func (s Server) RunScript(ctx context.Context, user string, script []byte, runner string, args ...string) error {
	path := fmt.Sprintf("/tmp/outpost-%d.sh", time.Now().UnixNano())
	upload := s.command(ctx, user, nil, "cat > "+path)
	upload.Stdin, upload.Stderr = bytes.NewReader(script), os.Stderr
	if err := upload.Run(); err != nil {
		return fmt.Errorf("upload script: %w", err)
	}
	run := strings.Join(append([]string{runner, path}, args...), " ")
	// Clean up the script but keep its exit status.
	return attach(s.command(ctx, user, []string{"-t"}, run+"; s=$?; rm -f "+path+"; exit $s")).Run()
}

func attach(cmd *exec.Cmd) *exec.Cmd {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd
}
