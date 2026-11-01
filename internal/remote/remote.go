// Package remote runs commands on a server through the system ssh binary, so
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

// Server is an SSH-reachable machine. Port 0 leaves the port to ssh, so
// ~/.ssh/config applies; otherwise it's 22.
type Server struct {
	Host string
	Port int
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
	return out, withStderr(err, &stderr)
}

// withStderr adds a command's error output to its error, when there is any.
func withStderr(err error, stderr *bytes.Buffer) error {
	if msg := strings.TrimSpace(stderr.String()); err != nil && msg != "" {
		return fmt.Errorf("%w: %s", err, msg)
	}
	return err
}

// Script is a shell script to run on the server as `Runner <script>`.
type Script struct {
	Body   []byte
	Runner string // e.g. "bash" or "sudo bash"
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
	return sc.Runner + " " + path + "; s=$?; rm -f " + path + "; exit $s", nil
}

// Copy pipes the output of srcCmd on src straight into dstCmd on dst, both as
// user, calling progress with the running byte count.
func Copy(ctx context.Context, src, dst Server, user, srcCmd, dstCmd string, progress func(int64)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var srcErr, dstErr bytes.Buffer
	from := src.command(ctx, user, nil, srcCmd)
	from.Stderr = &srcErr
	out, err := from.StdoutPipe()
	if err != nil {
		return err
	}
	to := dst.command(ctx, user, nil, dstCmd)
	to.Stderr = &dstErr
	in, err := to.StdinPipe()
	if err != nil {
		return err
	}
	if err := to.Start(); err != nil {
		return err
	}
	if err := from.Start(); err != nil {
		in.Close()
		_ = to.Wait()
		return err
	}
	_, copyErr := io.Copy(in, &countingReader{r: out, progress: progress})
	in.Close() // end of input for dstCmd
	if copyErr != nil {
		cancel() // the destination stopped reading; stop the source rather than let it block forever
	}
	toErr, fromErr := to.Wait(), from.Wait()
	switch {
	case fromErr != nil && copyErr == nil: // the source failed on its own
		return fmt.Errorf("read from %s: %w", src.Host, withStderr(fromErr, &srcErr))
	case toErr != nil:
		return fmt.Errorf("write to %s: %w", dst.Host, withStderr(toErr, &dstErr))
	case fromErr != nil:
		return fmt.Errorf("read from %s: %w", src.Host, withStderr(fromErr, &srcErr))
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

func attach(cmd *exec.Cmd) *exec.Cmd {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd
}
