//go:build !windows || !amd64

package zapret2

import (
	"context"
	"net"
)

type Engine struct{}

func New(options []string, _ string) (*Engine, error) {
	if _, err := strategyArgs(options); err != nil {
		return nil, err
	}
	return nil, ErrUnsupported
}

func (*Engine) Attach(context.Context, net.Conn, bool) (func() error, error) {
	return nil, ErrUnsupported
}
func (*Engine) Close() error { return nil }
func Shutdown()              {}
