//go:build windows

package zapret

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"syscall"

	"github.com/metacubex/mihomo/log"
)

type winBackend struct {
	wd    *winDivert
	mu    sync.Mutex
	flows map[*flow]*windivertFlow
}

func newBackend() (backend, error) {
	wd, err := loadWinDivert()
	if err != nil {
		return nil, err
	}
	// Install/verify the driver once, so a missing driver or missing
	// Administrator rights fail at config load instead of per connection.
	probe, err := wd.open("false")
	if err != nil {
		return nil, fmt.Errorf("WinDivertOpen: %w (run as Administrator and put WinDivert.dll + WinDivert64.sys next to the exe)", err)
	}
	_ = wd.close(probe)
	return &winBackend{wd: wd, flows: make(map[*flow]*windivertFlow)}, nil
}

func (b *winBackend) openTCP(local, remote netip.AddrPort, f *flow) (flowHandle, error) {
	return b.openFlow("tcp", local, remote, f)
}

func (b *winBackend) openUDP(local, remote netip.AddrPort, f *flow) (flowHandle, error) {
	return b.openFlow("udp", local, remote, f)
}

func (b *winBackend) openFlow(proto string, local, remote netip.AddrPort, f *flow) (flowHandle, error) {
	filter, err := flowFilter(proto, local, remote)
	if err != nil {
		return nil, err
	}
	handle, err := b.wd.open(filter)
	if err != nil {
		// A simpler filter avoids address-syntax differences between
		// WinDivert builds; the 5-tuple check still happens in userspace.
		simple := fmt.Sprintf("outbound and %s and %s.SrcPort == %d and %s.DstPort == %d",
			proto, proto, local.Port(), proto, remote.Port())
		handle, err = b.wd.open(simple)
		if err != nil {
			return nil, fmt.Errorf("WinDivertOpen(%q): %w", filter, err)
		}
	}
	h := &windivertFlow{backend: b, handle: handle, flow: f}
	b.mu.Lock()
	b.flows[f] = h
	b.mu.Unlock()
	go h.loop()
	return h, nil
}

type windivertFlow struct {
	backend *winBackend
	handle  syscall.Handle
	flow    *flow
	once    sync.Once
}

func (h *windivertFlow) loop() {
	defer h.close()
	buf := make([]byte, 65535)
	addr := &windivertAddress{}
	for {
		n, err := h.backend.wd.recv(h.handle, buf, addr)
		if err != nil {
			log.Debugln("[zapret] recv: %v", err)
			return
		}
		pkts := h.flow.process(buf[:n], addr)
		failed := false
		for _, p := range pkts {
			if p.fallback {
				continue
			}
			a, _ := p.addr.(*windivertAddress)
			if p.crafted && a != nil {
				copyAddr := *a
				copyAddr.setChecksums(isIPv6(p.data), isTCP(p.data))
				a = &copyAddr
			}
			if err := h.backend.wd.send(h.handle, p.data, a); err != nil {
				failed = true
				log.Debugln("[zapret] send: %v", err)
			}
		}
		if failed {
			for _, p := range pkts {
				if !p.fallback {
					continue
				}
				a, _ := p.addr.(*windivertAddress)
				if err := h.backend.wd.send(h.handle, p.data, a); err != nil {
					log.Debugln("[zapret] send fallback: %v", err)
				}
			}
		}
	}
}

func (h *windivertFlow) close() error {
	var err error
	h.once.Do(func() {
		h.backend.mu.Lock()
		delete(h.backend.flows, h.flow)
		h.backend.mu.Unlock()
		err = h.backend.wd.close(h.handle)
	})
	return err
}

// flowFilter builds the per-flow WinDivert filter. Intercepting only the
// registered 5-tuple keeps other applications out of the engine.
func flowFilter(proto string, local, remote netip.AddrPort) (string, error) {
	if !remote.IsValid() {
		return "", errors.New("zapret: invalid remote address")
	}
	local = netip.AddrPortFrom(local.Addr().Unmap(), local.Port())
	remote = netip.AddrPortFrom(remote.Addr().Unmap(), remote.Port())

	var parts []string
	parts = append(parts, "outbound", proto)
	if remote.Addr().Is4() {
		parts = append(parts, "ip")
		if !local.Addr().IsUnspecified() {
			parts = append(parts, fmt.Sprintf("ip.SrcAddr == %s", local.Addr()))
		}
		parts = append(parts, fmt.Sprintf("ip.DstAddr == %s", remote.Addr()))
	} else {
		parts = append(parts, "ipv6")
		if !local.Addr().IsUnspecified() {
			parts = append(parts, fmt.Sprintf("ipv6.SrcAddr == %s", local.Addr()))
		}
		parts = append(parts, fmt.Sprintf("ipv6.DstAddr == %s", remote.Addr()))
	}
	parts = append(parts,
		fmt.Sprintf("%s.SrcPort == %d", proto, local.Port()),
		fmt.Sprintf("%s.DstPort == %d", proto, remote.Port()),
	)
	return strings.Join(parts, " and "), nil
}

func isIPv6(pkt []byte) bool { return len(pkt) > 0 && pkt[0]>>4 == 6 }

func isTCP(pkt []byte) bool {
	if len(pkt) < 20 {
		return false
	}
	ihl := int(pkt[0]&0x0f) * 4
	if pkt[0]>>4 == 6 {
		return len(pkt) > 6 && pkt[6] == 6
	}
	return len(pkt) > ihl && pkt[9] == 6
}
