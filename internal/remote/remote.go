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
	"slices"
	"strconv"
	"strings"
	"time"
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

// Copy pipes the output of srcCmd on src straight into dstCmd on dst, both as
// user, calling progress with the running byte count.
func Copy(ctx context.Context, src, dst Server, user, srcCmd, dstCmd string, progress func(int64)) error {
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
	fromErr, toErr := from.Wait(), to.Wait()
	switch {
	case fromErr != nil:
		return fmt.Errorf("read from %s: %w: %s", src.Host, fromErr, strings.TrimSpace(srcErr.String()))
	case toErr != nil:
		return fmt.Errorf("write to %s: %w: %s", dst.Host, toErr, strings.TrimSpace(dstErr.String()))
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
