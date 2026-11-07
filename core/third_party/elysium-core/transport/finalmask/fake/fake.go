package fake

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/metacubex/mihomo/transport/finalmask"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// Config is the built mask. It is safe to share between connections.
type Config struct {
	repeats  int
	cutoff   int
	ttl      int
	delayMin int
	delayMax int
	payloads []*Payload
}

// New reads the option into a mask.
func New(o Option) (*Config, error) {
	repeats := 1
	if o.Repeats != nil {
		repeats = *o.Repeats
	}
	if repeats < 1 {
		return nil, errors.New("fake repeats must be at least 1")
	}
	cutoff := 1
	if o.Cutoff != nil {
		cutoff = *o.Cutoff
	}
	if cutoff < 0 {
		return nil, errors.New("fake cutoff can't be negative")
	}
	if o.TTL < 0 || o.TTL > 255 {
		return nil, errors.New("fake ttl must be in 0-255")
	}
	delayMin, delayMax := o.Delay.From, o.Delay.To
	if delayMin > delayMax {
		delayMin, delayMax = delayMax, delayMin
	}
	if delayMin < 0 {
		return nil, errors.New("fake delay can't be negative")
	}
	payloads := make([]*Payload, 0, len(o.Payloads))
	for i, item := range o.Payloads {
		payload, err := item.build()
		if err != nil {
			return nil, fmt.Errorf("fake payload %d: %w", i, err)
		}
		payloads = append(payloads, payload)
	}
	return &Config{
		repeats:  repeats,
		cutoff:   cutoff,
		ttl:      o.TTL,
		delayMin: delayMin,
		delayMax: delayMax,
		payloads: payloads,
	}, nil
}

// payloadFor picks the payload for addr. A nil return means the real datagram
// is repeated as the fake.
func (c *Config) payloadFor(addr net.Addr) *Payload {
	port := 0
	if udp, ok := addr.(*net.UDPAddr); ok {
		port = udp.Port
	}
	for _, payload := range c.payloads {
		if payload.matches(port) {
			return payload
		}
	}
	return nil
}

func (c *Config) WrapPacketConnClient(_ context.Context, conn net.PacketConn, _ *finalmask.Destination, _ *finalmask.Dialer) (net.PacketConn, error) {
	return NewConn(c, conn), nil
}

// WrapPacketConnServer leaves the socket alone: the decoys are for the DPI on
// the client's path, a server has nothing to send them to.
func (c *Config) WrapPacketConnServer(conn net.PacketConn) (net.PacketConn, error) {
	return conn, nil
}

// Conn sends the decoy datagrams before the real ones.
type Conn struct {
	net.PacketConn
	cfg    *Config
	mu     sync.Mutex
	counts map[string]int

	// TTL is a socket option, so it is changed around a fake and restored
	// before the real datagram. The probe runs once per address family.
	probe4, probe6     bool
	haveTTL4, haveTTL6 bool
	v4                 *ipv4.Conn
	v6                 *ipv6.Conn
	ttl4, ttl6         int
}

func NewConn(c *Config, raw net.PacketConn) net.PacketConn {
	return &Conn{
		PacketConn: raw,
		cfg:        c,
		counts:     make(map[string]int),
	}
}

func (c *Conn) Upstream() any { return c.PacketConn }

func (c *Conn) WriteTo(p []byte, addr net.Addr) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := ""
	if addr != nil {
		key = addr.String()
	}
	if c.cfg.cutoff != 0 && c.counts[key] >= c.cfg.cutoff {
		return c.PacketConn.WriteTo(p, addr)
	}

	payload := c.cfg.payloadFor(addr)
	restore := c.fakeTTL(addr)
	for i := 0; i < c.cfg.repeats; i++ {
		b := p
		if payload != nil {
			b = payload.bytes()
		}
		if _, err := c.PacketConn.WriteTo(b, addr); err != nil {
			// A decoy that can't be sent must not stop the real datagram.
			break
		}
		c.pause()
	}
	restore()

	n, err := c.PacketConn.WriteTo(p, addr)
	if err == nil {
		c.counts[key]++
	}
	return n, err
}

func (c *Conn) pause() {
	if c.cfg.delayMax <= 0 {
		return
	}
	d := finalmask.RandBetween(int64(c.cfg.delayMin), int64(c.cfg.delayMax)+1)
	if d > 0 {
		time.Sleep(time.Duration(d) * time.Millisecond)
	}
}

// fakeTTL lowers the socket TTL for the decoys and returns a function that
// restores it. When the family's TTL can't be read or set, the decoys go out
// with the normal TTL.
func (c *Conn) fakeTTL(addr net.Addr) func() {
	noop := func() {}
	if c.cfg.ttl == 0 {
		return noop
	}
	udp, ok := addr.(*net.UDPAddr)
	if !ok || udp.IP == nil {
		return noop
	}
	raw := underlyingConn(c.PacketConn)
	if raw == nil {
		return noop
	}
	if udp.IP.To4() != nil {
		if !c.probe4 {
			c.probe4 = true
			c.v4 = ipv4.NewConn(raw)
			if ttl, err := c.v4.TTL(); err == nil {
				c.ttl4, c.haveTTL4 = ttl, true
			}
		}
		if !c.haveTTL4 || c.v4.SetTTL(c.cfg.ttl) != nil {
			return noop
		}
		return func() { _ = c.v4.SetTTL(c.ttl4) }
	}
	if !c.probe6 {
		c.probe6 = true
		c.v6 = ipv6.NewConn(raw)
		if ttl, err := c.v6.HopLimit(); err == nil {
			c.ttl6, c.haveTTL6 = ttl, true
		}
	}
	if !c.haveTTL6 || c.v6.SetHopLimit(c.cfg.ttl) != nil {
		return noop
	}
	return func() { _ = c.v6.SetHopLimit(c.ttl6) }
}

// underlyingConn walks the wrappers of the chain to the socket whose options
// can be set. A mask whose conn is not a net.Conn hides the socket and the
// decoys then go out with the normal TTL.
func underlyingConn(pc net.PacketConn) net.Conn {
	for i := 0; i < 8 && pc != nil; i++ {
		if conn, ok := pc.(net.Conn); ok {
			return conn
		}
		upstream, ok := pc.(interface{ Upstream() any })
		if !ok {
			return nil
		}
		next, ok := upstream.Upstream().(net.PacketConn)
		if !ok {
			return nil
		}
		pc = next
	}
	return nil
}
