package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"path/filepath"

	"github.com/metacubex/mihomo/component/dialer"
	"github.com/metacubex/mihomo/component/resolver"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/transport/zapret2"
)

type Zapret2Option struct {
	BasicOption
	Name      string   `proxy:"name"`
	Options   []string `proxy:"options"`
	AssetsDir string   `proxy:"assets-dir,omitempty"`
}

type Zapret2 struct {
	*Direct
	engine *zapret2.Engine
}

func NewZapret2(option Zapret2Option) (*Zapret2, error) {
	switch {
	case option.Name == "":
		return nil, errors.New("zapret2 requires a name")
	case option.ProviderName != "":
		return nil, errors.New("zapret2 must be declared in the main configuration")
	case option.TFO || option.MPTCP || option.DialerProxy != "":
		return nil, errors.New("zapret2 requires direct sockets without TFO or MPTCP")
	}
	assetsDir := option.AssetsDir
	if assetsDir != "" {
		if !C.Path.IsSafePath(assetsDir) {
			return nil, C.Path.ErrNotSafePath(assetsDir)
		}
		resolved, err := filepath.EvalSymlinks(C.Path.Resolve(assetsDir))
		if err != nil {
			return nil, fmt.Errorf("zapret2 assets-dir: %w", err)
		}
		if !C.Path.IsSafePath(resolved) {
			return nil, C.Path.ErrNotSafePath(resolved)
		}
		assetsDir = resolved
	}
	engine, err := zapret2.New(option.Options, assetsDir)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", option.Name, err)
	}
	direct := NewDirectWithOption(DirectOption{BasicOption: option.BasicOption, Name: option.Name})
	direct.tp = C.Zapret2
	return &Zapret2{Direct: direct, engine: engine}, nil
}

func (z *Zapret2) DialContext(ctx context.Context, metadata *C.Metadata) (C.Conn, error) {
	if err := z.loopBack.CheckConn(metadata); err != nil {
		return nil, err
	}
	options := append(z.DialOptions(), dialer.WithResolver(resolver.DirectHostResolver))
	conn, err := dialer.DialContext(ctx, "tcp", metadata.RemoteAddress(), options...)
	if err != nil {
		return nil, err
	}
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("zapret2 requires a native TCP socket")
	}
	closeFlow, err := z.engine.Attach(ctx, tcp, true)
	if err != nil {
		_ = tcp.Close()
		return nil, err
	}
	return z.loopBack.NewConn(NewConn(&zapret2TCPConn{TCPConn: tcp, closeFlow: closeFlow}, z)), nil
}

func (z *Zapret2) ListenPacketContext(ctx context.Context, metadata *C.Metadata) (C.PacketConn, error) {
	if err := z.loopBack.CheckPacketConn(metadata); err != nil {
		return nil, err
	}
	if err := z.ResolveUDP(ctx, metadata); err != nil {
		return nil, err
	}
	remote := metadata.AddrPort()
	if !remote.IsValid() || remote.Port() == 0 {
		return nil, errors.New("zapret2 requires a UDP destination")
	}
	conn, err := dialer.DialContext(ctx, "udp", remote.String(), z.DialOptions()...)
	if err != nil {
		return nil, err
	}
	udp, ok := conn.(*net.UDPConn)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("zapret2 requires a native UDP socket")
	}
	closeFlow, err := z.engine.Attach(ctx, udp, false)
	if err != nil {
		_ = udp.Close()
		return nil, err
	}
	return z.loopBack.NewPacketConn(NewPacketConn(&zapret2UDPConn{
		UDPConn: udp, remote: remote, closeFlow: closeFlow,
	}, z)), nil
}

func (z *Zapret2) Close() error { return z.engine.Close() }

type zapret2TCPConn struct {
	*net.TCPConn
	closeFlow func() error
}

func (c *zapret2TCPConn) Close() error { return c.closeFlow() }

type zapret2UDPConn struct {
	*net.UDPConn
	remote    netip.AddrPort
	closeFlow func() error
}

func (c *zapret2UDPConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	remote, ok := addr.(*net.UDPAddr)
	if !ok || remote.AddrPort() != c.remote {
		return 0, errors.New("zapret2 UDP destination differs from the registered flow")
	}
	return c.UDPConn.Write(p)
}

func (c *zapret2UDPConn) Close() error { return c.closeFlow() }
