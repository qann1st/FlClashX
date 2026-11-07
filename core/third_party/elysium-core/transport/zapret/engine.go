package zapret

import (
	"errors"
	"net"
	"net/netip"
	"sync"

	"github.com/metacubex/mihomo/log"
)

// ErrUnsupported is returned on platforms without the packet backend. A config
// with zapret still loads there; the original fragment/finalmask path is used.
var ErrUnsupported = errors.New("zapret packet desync is supported on Windows only")

// Engine owns the parsed strategies and the platform backend.
type Engine struct {
	cfg     *Config
	backend backend
}

type backend interface {
	openTCP(local, remote netip.AddrPort, f *flow) (flowHandle, error)
	openUDP(local, remote netip.AddrPort, f *flow) (flowHandle, error)
}

type flowHandle interface {
	close() error
}

type outPacket struct {
	data     []byte
	addr     any
	crafted  bool
	fallback bool
}

type flow struct {
	proto    byte // 6 TCP, 17 UDP
	strategy *Strategy
	local    netip.AddrPort
	remote   netip.AddrPort

	dataPackets int
	handle      flowHandle
	closeOnce   sync.Once
}

// New builds the engine. On Windows it loads WinDivert; on other platforms it
// returns ErrUnsupported.
func New(cfg *Config) (*Engine, error) {
	b, err := newBackend()
	if err != nil {
		return nil, err
	}
	return &Engine{cfg: cfg, backend: b}, nil
}

// RegisterTCP matches the connection's destination port and starts packet
// interception for it. Without a matching strategy the connection is returned
// unchanged.
func (e *Engine) RegisterTCP(conn net.Conn) (net.Conn, error) {
	if e == nil || e.cfg == nil {
		return conn, nil
	}
	local, ok := addrPort(conn.LocalAddr())
	if !ok {
		return conn, nil
	}
	remote, ok := addrPort(conn.RemoteAddr())
	if !ok {
		return conn, nil
	}
	strategy := e.cfg.TCPFor(remote.Port())
	if strategy == nil {
		return conn, nil
	}
	f := &flow{proto: 6, strategy: strategy, local: local, remote: remote}
	h, err := e.backend.openTCP(local, remote, f)
	if err != nil {
		return conn, err
	}
	f.handle = h
	return &zapretConn{Conn: conn, flow: f}, nil
}

// RegisterUDP matches the destination port of the UDP flow and starts packet
// interception for it.
func (e *Engine) RegisterUDP(pc net.PacketConn, remote netip.AddrPort) (net.PacketConn, error) {
	if e == nil || e.cfg == nil || !remote.IsValid() {
		return pc, nil
	}
	strategy := e.cfg.UDPFor(remote.Port())
	if strategy == nil {
		return pc, nil
	}
	local, ok := addrPort(pc.LocalAddr())
	if !ok {
		return pc, nil
	}
	f := &flow{proto: 17, strategy: strategy, local: local, remote: remote}
	h, err := e.backend.openUDP(local, remote, f)
	if err != nil {
		return pc, err
	}
	f.handle = h
	return &zapretPacketConn{PacketConn: pc, flow: f}, nil
}

func (f *flow) close() {
	f.closeOnce.Do(func() {
		if f.handle != nil {
			if err := f.handle.close(); err != nil {
				log.Debugln("[zapret] close flow: %v", err)
			}
		}
	})
}

func (f *flow) matches(p *packetInfo) bool {
	if f.proto == 6 && !p.isTCP() {
		return false
	}
	if f.proto == 17 && p.udp == nil {
		return false
	}
	if p.srcPort != f.local.Port() || p.dstPort != f.remote.Port() {
		return false
	}
	if !f.local.Addr().IsUnspecified() && p.srcIP.Unmap() != f.local.Addr().Unmap() {
		return false
	}
	return p.dstIP.Unmap() == f.remote.Addr().Unmap()
}

// process is called by the backend for every intercepted outbound packet. It
// returns the packets to inject, including the original when it is kept.
func (f *flow) process(data []byte, addr any) []outPacket {
	original := outPacket{data: data, addr: addr}
	p, err := parsePacket(data)
	if err != nil || !f.matches(p) || len(p.payload) == 0 {
		return []outPacket{original}
	}
	first := f.strategy.cutoff == 0 || f.dataPackets < f.strategy.cutoff
	built, drop, err := f.strategy.apply(p, first)
	if err != nil {
		log.Debugln("[zapret] strategy error: %v", err)
		return []outPacket{original}
	}
	f.dataPackets++
	out := make([]outPacket, 0, len(built)+1)
	for _, b := range built {
		out = append(out, outPacket{data: b, addr: addr, crafted: true})
	}
	if !drop {
		out = append(out, original)
	} else {
		// Keep the original as a recovery packet: if injecting a crafted
		// segment fails, the real bytes are still delivered.
		out = append(out, outPacket{data: data, addr: addr, fallback: true})
	}
	return out
}

type zapretConn struct {
	net.Conn
	flow *flow
}

func (c *zapretConn) Close() error {
	c.flow.close()
	return c.Conn.Close()
}

func (c *zapretConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

func (c *zapretConn) CloseRead() error {
	if cr, ok := c.Conn.(interface{ CloseRead() error }); ok {
		return cr.CloseRead()
	}
	return nil
}

func (c *zapretConn) Upstream() any { return c.Conn }

type zapretPacketConn struct {
	net.PacketConn
	flow *flow
}

func (c *zapretPacketConn) Close() error {
	c.flow.close()
	return c.PacketConn.Close()
}

func (c *zapretPacketConn) Upstream() any { return c.PacketConn }

func addrPort(addr net.Addr) (netip.AddrPort, bool) {
	switch a := addr.(type) {
	case *net.TCPAddr:
		return a.AddrPort(), true
	case *net.UDPAddr:
		return a.AddrPort(), true
	default:
		return netip.AddrPort{}, false
	}
}
