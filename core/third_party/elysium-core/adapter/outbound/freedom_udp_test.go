package outbound

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/metacubex/mihomo/common/structure"
	C "github.com/metacubex/mihomo/constant"
)

// udpServer starts a UDP listener on loopback for the freedom UDP tests.
func udpServer(t *testing.T) net.PacketConn {
	t.Helper()
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return server
}

func readUDPMessages(t *testing.T, conn net.PacketConn, n int) []string {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var out []string
	buf := make([]byte, 4096)
	for i := 0; i < n; i++ {
		read, _, err := conn.ReadFrom(buf)
		if err != nil {
			t.Fatalf("read %d of %d: %v", i+1, n, err)
		}
		out = append(out, string(buf[:read]))
	}
	return out
}

func newUDPFreedom(t *testing.T, finalMask *FinalMaskOption) *Freedom {
	t.Helper()
	proxy, err := NewFreedom(FreedomOption{
		Name:      "UDP-FAKE",
		FinalMask: finalMask,
	})
	if err != nil {
		t.Fatal(err)
	}
	return proxy
}

func listenFreedomUDP(t *testing.T, proxy *Freedom, server net.PacketConn) C.PacketConn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	pc, err := proxy.ListenPacketContext(ctx, &C.Metadata{
		NetWork: C.UDP,
		DstIP:   netip.MustParseAddr("127.0.0.1"),
		DstPort: uint16(server.LocalAddr().(*net.UDPAddr).Port),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	return pc
}

// The fake UDP mask sends its decoys before the real datagram through the
// freedom packet socket.
func TestFreedomUDPFakeMask(t *testing.T) {
	server := udpServer(t)
	proxy := newUDPFreedom(t, &FinalMaskOption{
		UDP: []map[string]any{{
			"type":     "fake",
			"repeats":  2,
			"cutoff":   1,
			"payloads": []any{map[string]any{"hex": "c0ffee"}},
		}},
	})
	pc := listenFreedomUDP(t, proxy, server)
	if _, err := pc.WriteTo([]byte("real"), server.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	got := readUDPMessages(t, server, 3)
	for i := 0; i < 2; i++ {
		if got[i] != "\xc0\xff\xee" {
			t.Fatalf("decoy %d = %q, want the configured payload", i, got[i])
		}
	}
	if got[2] != "real" {
		t.Fatalf("last datagram = %q, want the real one", got[2])
	}
}

// A UDP mask that cannot be built stops the proxy from being created.
func TestFreedomRejectsBadUDPFinalMask(t *testing.T) {
	if _, err := NewFreedom(FreedomOption{
		Name: "bad",
		FinalMask: &FinalMaskOption{
			UDP: []map[string]any{{"type": "fake", "repeats": 0}},
		},
	}); err == nil {
		t.Fatal("finalmask with repeats 0 accepted")
	}
}

// The freedom finalmask block decodes from the same YAML mapping ParseProxy
// hands it, payloads and their port filters included.
func TestFreedomUDPFinalMaskFromMapping(t *testing.T) {
	decoder := structure.NewDecoder(structure.Option{
		TagName:          "proxy",
		WeaklyTypedInput: true,
		KeyReplacer:      structure.DefaultKeyReplacer,
	})
	mapping := map[string]any{
		"name": "UDP-FAKE",
		"type": "freedom",
		"finalmask": map[string]any{
			"udp": []any{map[string]any{
				"type":    "fake",
				"repeats": 6,
				"cutoff":  1,
				"ttl":     3,
				"delay":   "0-1",
				"payloads": []any{
					map[string]any{"hex": "c0ffee", "ports": "50000-50100"},
					map[string]any{"random": "64-120"},
				},
			}},
		},
	}
	option := &FreedomOption{}
	if err := decoder.Decode(mapping, option); err != nil {
		t.Fatal(err)
	}
	proxy, err := NewFreedom(*option)
	if err != nil {
		t.Fatal(err)
	}
	if proxy.finalMask == nil || !proxy.finalMask.HasUDP() {
		t.Fatal("the fake UDP mask was not built from the mapping")
	}
}
