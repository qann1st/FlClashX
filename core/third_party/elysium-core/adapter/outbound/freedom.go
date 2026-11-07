package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/metacubex/mihomo/component/dialer"
	"github.com/metacubex/mihomo/component/loopback"
	"github.com/metacubex/mihomo/component/resolver"
	"github.com/metacubex/mihomo/component/sockopt"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/log"
	"github.com/metacubex/mihomo/transport/finalmask"
	"github.com/metacubex/mihomo/transport/fragment"
	"github.com/metacubex/mihomo/transport/zapret"
)

// Freedom is DIRECT with the fragment and sockopt settings of Xray-core's
// freedom outbound, plus an optional finalmask chain. The TCP chain wraps the
// stream (the fragment block is the short spelling of one fragment mask), and
// the UDP chain wraps the packet socket, where masks such as fake send decoys
// to get past DPI that follows QUIC, Discord voice or game traffic.
type Freedom struct {
	*Direct
	fragment        *fragment.Config
	sockopt         *sockopt.Options
	finalMask       *finalmask.FinalMask
	finalMaskDialer *finalmask.Dialer
	zapret          *zapret.Engine
}

type FreedomOption struct {
	BasicOption
	Name      string                 `proxy:"name"`
	Fragment  *FreedomFragmentOption `proxy:"fragment,omitempty"`
	Sockopt   *FreedomSockoptOption  `proxy:"sockopt,omitempty"`
	FinalMask *FinalMaskOption       `proxy:"finalmask,omitempty"`
	Zapret    *zapret.Option         `proxy:"zapret,omitempty"`
}

type FreedomFragmentOption struct {
	Packets       string `proxy:"packets,omitempty"`
	Length        string `proxy:"length,omitempty"`
	Interval      string `proxy:"interval,omitempty"`
	MaxSplit      string `proxy:"max-split,omitempty"`
	Split         string `proxy:"split,omitempty"`
	Wait          string `proxy:"wait,omitempty"`
	FallbackDelay int    `proxy:"fallback-delay,omitempty"`
}

type FreedomSockoptOption struct {
	TCPCongestion        string                 `proxy:"tcp-congestion,omitempty"`
	TCPUserTimeout       int                    `proxy:"tcp-user-timeout,omitempty"`
	TCPKeepAliveIdle     int                    `proxy:"tcp-keep-alive-idle,omitempty"`
	TCPKeepAliveInterval int                    `proxy:"tcp-keep-alive-interval,omitempty"`
	CustomSockopt        []FreedomCustomSockopt `proxy:"custom-sockopt,omitempty"`
}

type FreedomCustomSockopt struct {
	System  string `proxy:"system,omitempty"`
	Network string `proxy:"network,omitempty"`
	Level   string `proxy:"level,omitempty"`
	Opt     string `proxy:"opt"`
	Type    string `proxy:"type,omitempty"`
	Value   string `proxy:"value"`
}

// DialContext implements C.ProxyAdapter
func (f *Freedom) DialContext(ctx context.Context, metadata *C.Metadata) (C.Conn, error) {
	if err := f.loopBack.CheckConn(metadata); err != nil {
		return nil, err
	}
	opts := f.DialOptions()
	opts = append(opts, dialer.WithResolver(resolver.DirectHostResolver))
	if f.sockopt != nil {
		opts = append(opts, dialer.WithNetDialer(&net.Dialer{ControlContext: f.sockopt.Control}))
	}
	c, err := dialer.DialContext(ctx, "tcp", metadata.RemoteAddress(), opts...)
	if err != nil {
		return nil, err
	}
	if f.sockopt != nil {
		f.sockopt.AfterDial(c)
	}
	if f.zapret != nil {
		var zerr error
		c, zerr = f.zapret.RegisterTCP(c)
		if zerr != nil {
			log.Warnln("[zapret] %s: %v", f.Name(), zerr)
		}
	}
	if f.zapret == nil && f.fragment != nil {
		c = fragment.NewConn(c, f.fragment)
	}
	if f.zapret == nil && f.finalMask != nil && f.finalMask.HasTCP() {
		dest, err := finalmask.ParseDestination(metadata.RemoteAddress())
		if err != nil {
			_ = c.Close()
			return nil, err
		}
		c, err = f.finalMask.WrapConnClient(ctx, c, &dest, f.finalMaskDialer)
		if err != nil {
			return nil, err
		}
	}
	return f.loopBack.NewConn(NewConn(c, f)), nil
}

// ListenPacketContext implements C.ProxyAdapter
func (f *Freedom) ListenPacketContext(ctx context.Context, metadata *C.Metadata) (C.PacketConn, error) {
	if err := f.loopBack.CheckPacketConn(metadata); err != nil {
		return nil, err
	}
	if err := f.ResolveUDP(ctx, metadata); err != nil {
		return nil, err
	}
	if f.zapret != nil {
		pc, err := dialer.NewDialer(f.DialOptions()...).ListenPacket(ctx, "udp", "", metadata.AddrPort())
		if err != nil {
			return nil, err
		}
		pc, zerr := f.zapret.RegisterUDP(pc, metadata.AddrPort())
		if zerr != nil {
			log.Warnln("[zapret] %s: %v", f.Name(), zerr)
		}
		return f.loopBack.NewPacketConn(NewPacketConn(pc, f)), nil
	}
	if f.finalMask != nil && f.finalMask.HasUDP() {
		// ResolveUDP above guarantees an IP, so the masks see the address the
		// socket dials, not a domain the OS resolver would look up again.
		dest := finalmask.Destination{Host: metadata.DstIP.String(), Port: metadata.DstPort}
		pc, _, err := f.finalMask.DialUDP(ctx, dest, f.finalMaskDialer)
		if err != nil {
			return nil, err
		}
		return f.loopBack.NewPacketConn(NewPacketConn(pc, f)), nil
	}
	pc, err := dialer.NewDialer(f.DialOptions()...).ListenPacket(ctx, "udp", "", metadata.AddrPort())
	if err != nil {
		return nil, err
	}
	return f.loopBack.NewPacketConn(NewPacketConn(pc, f)), nil
}

// newFinalMaskDialer opens the sockets a mask asks for through this outbound's
// own dialer, so interface binding, routing marks and dialer-proxy apply.
func (f *Freedom) newFinalMaskDialer() *finalmask.Dialer {
	return &finalmask.Dialer{
		DialTCP: func(ctx context.Context, addr string) (net.Conn, error) {
			opts := append(f.DialOptions(), dialer.WithResolver(resolver.DirectHostResolver))
			return dialer.DialContext(ctx, "tcp", addr, opts...)
		},
		DialUDP: func(ctx context.Context, addr string) (net.PacketConn, net.Addr, error) {
			raddr, err := net.ResolveUDPAddr("udp", addr)
			if err != nil {
				return nil, nil, err
			}
			pc, err := dialer.NewDialer(f.DialOptions()...).ListenPacket(ctx, "udp", "", raddr.AddrPort())
			if err != nil {
				return nil, nil, err
			}
			return pc, raddr, nil
		},
	}
}

func NewFreedom(option FreedomOption) (*Freedom, error) {
	f := &Freedom{
		Direct: &Direct{
			Base: NewBase(BaseOption{
				Name:         option.Name,
				Type:         C.Freedom,
				ProviderName: option.ProviderName,
				UDP:          true,
				TFO:          option.TFO,
				MPTCP:        option.MPTCP,
				Interface:    option.Interface,
				RoutingMark:  option.RoutingMark,
				Prefer:       option.IPVersion,
			}),
			loopBack: loopback.NewDetector(),
		},
	}
	if o := option.Fragment; o != nil {
		cfg, err := fragment.NewConfig(fragment.Option{
			Packets:       o.Packets,
			Length:        o.Length,
			Interval:      o.Interval,
			MaxSplit:      o.MaxSplit,
			Split:         o.Split,
			Wait:          o.Wait,
			FallbackDelay: o.FallbackDelay,
		})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", option.Name, err)
		}
		f.fragment = cfg
	}
	if o := option.Sockopt; o != nil {
		so := &sockopt.Options{
			Name:              option.Name,
			TCPCongestion:     o.TCPCongestion,
			TCPUserTimeout:    o.TCPUserTimeout,
			KeepAliveIdle:     o.TCPKeepAliveIdle,
			KeepAliveInterval: o.TCPKeepAliveInterval,
		}
		for _, c := range o.CustomSockopt {
			custom, err := sockopt.ParseCustom(c.System, c.Network, c.Level, c.Opt, c.Type, c.Value)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", option.Name, err)
			}
			so.Custom = append(so.Custom, custom)
		}
		f.sockopt = so
	}
	if o := option.FinalMask; o != nil {
		fm, err := o.Parse()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", option.Name, err)
		}
		f.finalMask = fm
		f.finalMaskDialer = f.newFinalMaskDialer()
	}
	if o := option.Zapret; o != nil {
		cfg, err := zapret.Parse(o)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", option.Name, err)
		}
		eng, err := zapret.New(cfg)
		if err != nil {
			if errors.Is(err, zapret.ErrUnsupported) {
				// zapret is Windows-only. Everywhere else freedom keeps
				// its original fragment/finalmask behaviour.
				log.Warnln("[zapret] %s: %v; using the original fragment/finalmask path", option.Name, err)
			} else {
				return nil, fmt.Errorf("%s: %w", option.Name, err)
			}
		} else {
			if option.Fragment != nil || option.FinalMask != nil {
				log.Warnln("[zapret] %s: zapret overrides fragment/finalmask on Windows", option.Name)
			}
			f.zapret = eng
		}
	}
	return f, nil
}
