// Package remote runs commands on servers. Host is the interface the setup and
// move flows depend on; Server is its adapter over the system ssh binary, so
// the user's keys, agent and ~/.ssh/config work as usual.
package remote

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Host is a server that commands run on, as a given user.
type Host interface {
	// String names the host for messages and records: "host", or "host:port".
	String() string
	// CanLogin reports whether user can log in with a key, without prompting.
	CanLogin(ctx context.Context, user string) bool
	// CopyID installs the local public key for user, prompting for their password.
	CopyID(ctx context.Context, user string) error
	// Output runs a shell command and returns its stdout, even when it fails.
	Output(ctx context.Context, user, cmd string) ([]byte, error)
	// Interactive runs a shell command with the terminal attached, so it can prompt.
	Interactive(ctx context.Context, user, cmd string) error
	// RunStreaming runs a script, calling onLine for each line of its combined output.
	RunStreaming(ctx context.Context, user string, sc Script, onLine func(string)) error
	// CopyTo pipes srcCmd's output on this host into dstCmd on dst, both as user,
	// calling progress with the running byte count.
	CopyTo(ctx context.Context, dst Host, user, srcCmd, dstCmd string, progress func(int64)) error
}

// Script is a shell script to upload and run.
type Script struct {
	Body   []byte
	AsRoot bool // run with sudo
}

// Server is a Host reached with ssh. Port 0 leaves the port to ssh, so
// ~/.ssh/config applies; otherwise it's 22.
type Server struct {
	Addr string
	Port int
}

var _ Host = Server{}

// No leading "-", so an address can't be read as an ssh option.
var addrRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:-]*$`)

// Validate checks the server is safe to pass to ssh and to name in remote shell commands.
func (s Server) Validate() error {
	if !addrRe.MatchString(s.Addr) {
		return fmt.Errorf("%q doesn't look like a server address", s.Addr)
	}
	if s.Port < 0 || s.Port > 65535 {
		return fmt.Errorf("port %d is out of range", s.Port)
	}
	return nil
}

func (s Server) String() string {
	if s.Port == 0 {
		return s.Addr
	}
	return s.Addr + ":" + strconv.Itoa(s.Port)
}

var baseOpts = []string{"-o", "StrictHostKeyChecking=accept-new", "-o", "ConnectTimeout=10"}

func (s Server) sshArgs(extra ...string) []string {
	args := slices.Clone(baseOpts)
	if s.Port != 0 {
		args = append(args, "-p", strconv.Itoa(s.Port))
	}
	return append(args, extra...)
}

func (s Server) command(ctx context.Context, user string, opts []string, remoteCmd ...string) *exec.Cmd {
	args := append(s.sshArgs(opts...), user+"@"+s.Addr)
	return exec.CommandContext(ctx, "ssh", append(args, remoteCmd...)...)
}

func (s Server) CanLogin(ctx context.Context, user string) bool {
	return s.command(ctx, user, []string{"-o", "BatchMode=yes"}, "true").Run() == nil
}

func (s Server) CopyID(ctx context.Context, user string) error {
	return attach(exec.CommandContext(ctx, "ssh-copy-id", append(s.sshArgs(), user+"@"+s.Addr)...)).Run()
}

func (s Server) Output(ctx context.Context, user, cmd string) ([]byte, error) {
	var stderr bytes.Buffer
	c := s.command(ctx, user, nil, cmd)
	c.Stderr = &stderr
	out, err := c.Output()
	return out, withStderr(err, &stderr)
}

func (s Server) Interactive(ctx context.Context, user, cmd string) error {
	return attach(s.command(ctx, user, []string{"-t"}, cmd)).Run()
}

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
	if err := scan.Err(); errors.Is(err, bufio.ErrTooLong) {
		_, _ = io.Copy(io.Discard, pr) // keep reading so ssh can finish and exit
		return err
	}
	return scan.Err() // the exit error from Wait, via CloseWithError
}

// tmpPath matches the files mktemp creates for uploaded scripts.
var tmpPath = regexp.MustCompile(`^/tmp/outpost\.[A-Za-z0-9]+$`)

// upload copies the script to a private temporary file on the server and
// returns the command that runs it and then deletes it, keeping its exit status.
func (s Server) upload(ctx context.Context, user string, sc Script) (string, error) {
	var stderr bytes.Buffer
	up := s.command(ctx, user, nil, `f=$(mktemp /tmp/outpost.XXXXXXXX) && cat > "$f" && echo "$f"`)
	up.Stdin, up.Stderr = bytes.NewReader(sc.Body), &stderr
	out, err := up.Output()
	if err != nil {
		return "", fmt.Errorf("upload script: %w", withStderr(err, &stderr))
	}
	path := strings.TrimSpace(string(out))
	if !tmpPath.MatchString(path) { // it goes into a shell command below
		return "", fmt.Errorf("upload script: unexpected temporary file %q", path)
	}
	runner := "bash"
	if sc.AsRoot {
		runner = "sudo bash"
	}
	return runner + " " + path + "; s=$?; rm -f " + path + "; exit $s", nil
}

// CopyTo streams between two ssh servers through this machine.
func (s Server) CopyTo(ctx context.Context, dst Host, user, srcCmd, dstCmd string, progress func(int64)) error {
	to, ok := dst.(Server)
	if !ok {
		return fmt.Errorf("can only copy between ssh servers, not to %T", dst)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var srcErr, dstErr bytes.Buffer
	reader := s.command(ctx, user, nil, srcCmd)
	reader.Stderr = &srcErr
	out, err := reader.StdoutPipe()
	if err != nil {
		return err
	}
	writer := to.command(ctx, user, nil, dstCmd)
	writer.Stderr = &dstErr
	in, err := writer.StdinPipe()
	if err != nil {
		return err
	}
	if err := writer.Start(); err != nil {
		return err
	}
	if err := reader.Start(); err != nil {
		in.Close()
		_ = writer.Wait()
		return err
	}
	_, copyErr := io.Copy(in, &countingReader{r: out, progress: progress})
	in.Close() // end of input for dstCmd
	if copyErr != nil {
		cancel() // the destination stopped reading; stop the source rather than let it block forever
	}
	writeErr, readErr := writer.Wait(), reader.Wait()
	switch {
	case readErr != nil && copyErr == nil: // the source failed on its own
		return fmt.Errorf("read from %s: %w", s, withStderr(readErr, &srcErr))
	case writeErr != nil:
		return fmt.Errorf("write to %s: %w", to, withStderr(writeErr, &dstErr))
	case readErr != nil:
		return fmt.Errorf("read from %s: %w", s, withStderr(readErr, &srcErr))
	}
	return copyErr
}

type countingReader struct {
	r        io.Reader
	n        int64
	progress func(int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	c.progress(c.n)
	return n, err
}

// withStderr adds a command's error output to its error, when there is any.
func withStderr(err error, stderr *bytes.Buffer) error {
	if msg := strings.TrimSpace(stderr.String()); err != nil && msg != "" {
		return fmt.Errorf("%w: %s", err, msg)
	}
	return err
}

func attach(cmd *exec.Cmd) *exec.Cmd {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd
}
