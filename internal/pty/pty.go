package pty

type Transport interface {
	Write([]byte) error
	Resize(cols, rows int) error
	Close() error
	ReadLoop(func([]byte) error) error
}
