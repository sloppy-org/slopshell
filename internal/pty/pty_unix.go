//go:build !windows

package pty

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	creackpty "github.com/creack/pty"
)

type LocalTransport struct {
	cmd  *exec.Cmd
	file *os.File
	mu   sync.Mutex
}

func OpenLocal(cwd string) (*LocalTransport, error) {
	if cwd == "" {
		cwd = "."
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return nil, errors.New("invalid cwd")
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	cmd := exec.Command(shell, "-i")
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	f, err := creackpty.Start(cmd)
	if err != nil {
		return nil, err
	}
	return &LocalTransport{cmd: cmd, file: f}, nil
}

func (t *LocalTransport) Write(data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.file == nil {
		return io.ErrClosedPipe
	}
	_, err := t.file.Write(data)
	return err
}

func (t *LocalTransport) Resize(cols, rows int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.file == nil {
		return io.ErrClosedPipe
	}
	if cols < 1 {
		cols = 120
	}
	if rows < 1 {
		rows = 40
	}
	return creackpty.Setsize(t.file, &creackpty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (t *LocalTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	var err error
	if t.file != nil {
		err = t.file.Close()
		t.file = nil
	}
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Signal(syscall.SIGTERM)
		_, _ = t.cmd.Process.Wait()
	}
	return err
}

func (t *LocalTransport) ReadLoop(onData func([]byte) error) error {
	buf := make([]byte, 4096)
	for {
		t.mu.Lock()
		f := t.file
		t.mu.Unlock()
		if f == nil {
			return nil
		}
		n, err := f.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if cbErr := onData(chunk); cbErr != nil {
				return cbErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}
