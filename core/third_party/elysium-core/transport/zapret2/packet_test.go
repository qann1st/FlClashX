package zapret2

import (
	"net/netip"
	"strings"
	"testing"
)

func TestCaptureIsBoundToBothEndpoints(t *testing.T) {
	local := netip.MustParseAddrPort("192.0.2.10:50000")
	remote := netip.MustParseAddrPort("198.51.100.10:443")
	flow, err := newSocketFlow(local, remote, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"ip.SrcAddr == 192.0.2.10", "ip.DstAddr == 192.0.2.10",
		"ip.SrcAddr == 198.51.100.10", "ip.DstAddr == 198.51.100.10",
		"udp.SrcPort == 50000", "udp.DstPort == 50000"} {
		if !strings.Contains(flow.filter(), part) {
			t.Errorf("filter missing %q: %s", part, flow.filter())
		}
	}
	packet := []byte{
		0x45, 0, 0, 32, 0, 0, 0, 0, 64, 17, 0, 0, 192, 0, 2, 10, 198, 51, 100, 10,
		0xc3, 0x50, 0x01, 0xbb, 0, 12, 0, 0, 9, 9, 9, 9,
	}
	if !flow.matchesPacket(packet, true) || flow.matchesPacket(packet, false) {
		t.Fatal("wrong direction for registered socket")
	}
	packet[21]++ // Same destination, different local socket.
	if flow.matchesPacket(packet, true) {
		t.Fatal("captured another socket to the same destination")
	}
	if _, err := newSocketFlow(netip.MustParseAddrPort("0.0.0.0:50000"), remote, false); err == nil {
		t.Fatal("accepted wildcard local address")
	}
}
