// Package remote runs commands on a server through the system ssh binary, so
// the user's keys, agent and ~/.ssh/config work as usual.
package remote

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
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

// Output runs remoteCmd and returns its stdout, even when it fails.
func (s Server) Output(ctx context.Context, user, remoteCmd string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := s.command(ctx, user, nil, remoteCmd)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, err
}

// Script is a shell script to run on the server as `Runner <script> Args...`.
// The remote shell sees Args unquoted, so callers must validate them.
type Script struct {
	Body   []byte
	Runner string // e.g. "bash" or "sudo bash"
	Args   []string
}

// Interactive runs remoteCmd with a terminal attached, so it can prompt.
func (s Server) Interactive(ctx context.Context, user, remoteCmd string) error {
	return attach(s.command(ctx, user, []string{"-t"}, remoteCmd)).Run()
}

// RunStreaming runs script without a terminal, calling onLine for each line of
// combined stdout and stderr.
func (s Server) RunStreaming(ctx context.Context, user string, sc Script, onLine func(string)) error {
	remoteCmd, err := s.upload(ctx, user, sc)
	if err != nil {
		return err
	}
	cmd := s.command(ctx, user, nil, remoteCmd)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { pw.CloseWithError(cmd.Wait()) }()
	scan := bufio.NewScanner(pr)
	scan.Buffer(make([]byte, 64*1024), 1024*1024)
	for scan.Scan() {
		onLine(scan.Text())
	}
	return scan.Err() // the exit error from Wait, via CloseWithError
}

// upload copies the script to the server and returns the command that runs it
// and then deletes it, keeping its exit status.
func (s Server) upload(ctx context.Context, user string, sc Script) (string, error) {
	path := fmt.Sprintf("/tmp/outpost-%d.sh", time.Now().UnixNano())
	up := s.command(ctx, user, nil, "cat > "+path)
	up.Stdin = bytes.NewReader(sc.Body)
	if out, err := up.CombinedOutput(); err != nil {
		return "", fmt.Errorf("upload script: %w: %s", err, strings.TrimSpace(string(out)))
	}
	run := strings.Join(append([]string{sc.Runner, path}, sc.Args...), " ")
	return run + "; s=$?; rm -f " + path + "; exit $s", nil
}

func attach(cmd *exec.Cmd) *exec.Cmd {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd
}
