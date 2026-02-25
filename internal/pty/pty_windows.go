//go:build windows

package pty

import "errors"

var errPTYNotAvailableOnWindows = errors.New("PTY not available on Windows")

type LocalTransport struct{}

func OpenLocal(_ string) (*LocalTransport, error) {
	return nil, errPTYNotAvailableOnWindows
}

func (t *LocalTransport) Write(data []byte) error {
	return errPTYNotAvailableOnWindows
}

func (t *LocalTransport) Resize(cols, rows int) error {
	return errPTYNotAvailableOnWindows
}

func (t *LocalTransport) Close() error {
	return errPTYNotAvailableOnWindows
}

func (t *LocalTransport) ReadLoop(onData func([]byte) error) error {
	return errPTYNotAvailableOnWindows
}
