package zapret

import (
	"bytes"
	"net"
	"testing"

	"github.com/metacubex/gopacket"
	"github.com/metacubex/gopacket/layers"
)

func makeTCPPacket(payload []byte, srcPort, dstPort uint16, seq uint32) []byte {
	ip := &layers.IPv4{
		Version:  4,
		IHL:      5,
		TTL:      64,
		Protocol: layers.IPProtocolTCP,
		SrcIP:    net.IPv4(10, 0, 0, 1),
		DstIP:    net.IPv4(10, 0, 0, 2),
	}
	tcp := &layers.TCP{
		SrcPort: layers.TCPPort(srcPort),
		DstPort: layers.TCPPort(dstPort),
		Seq:     seq,
		Ack:     2000,
		PSH:     true,
		ACK:     true,
		Window:  65535,
	}
	_ = tcp.SetNetworkLayerForChecksum(ip)
	buf := gopacket.NewSerializeBuffer()
	_ = gopacket.SerializeLayers(buf, gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}, ip, tcp, gopacket.Payload(payload))
	return append([]byte(nil), buf.Bytes()...)
}

func makeUDPPacket(payload []byte, srcPort, dstPort uint16) []byte {
	ip := &layers.IPv4{
		Version:  4,
		IHL:      5,
		TTL:      64,
		Protocol: layers.IPProtocolUDP,
		SrcIP:    net.IPv4(10, 0, 0, 1),
		DstIP:    net.IPv4(10, 0, 0, 2),
	}
	udp := &layers.UDP{SrcPort: layers.UDPPort(srcPort), DstPort: layers.UDPPort(dstPort)}
	_ = udp.SetNetworkLayerForChecksum(ip)
	buf := gopacket.NewSerializeBuffer()
	_ = gopacket.SerializeLayers(buf, gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}, ip, udp, gopacket.Payload(payload))
	return append([]byte(nil), buf.Bytes()...)
}

func TestSeqOvlSplit(t *testing.T) {
	real := bytes.Repeat([]byte("R"), 40)
	raw := makeTCPPacket(real, 12345, 443, 1000)
	p, err := parsePacket(raw)
	if err != nil {
		t.Fatal(err)
	}
	pattern := bytes.Repeat([]byte("P"), 10)
	s := &Strategy{fake: false, split: true, splitAt: []int{1}, seqovl: 10, repeats: 1, cutoff: 1, fakeRaw: pattern}
	segs, drop, err := s.apply(p, true)
	if err != nil {
		t.Fatal(err)
	}
	if !drop {
		t.Fatal("original not dropped")
	}
	if len(segs) != 2 {
		t.Fatalf("got %d segments, want 2", len(segs))
	}

	first, err := parsePacket(segs[0])
	if err != nil {
		t.Fatal(err)
	}
	if first.seq != 1000-10 {
		t.Fatalf("first seq %d, want %d", first.seq, 990)
	}
	if !bytes.Equal(first.payload[:10], pattern) {
		t.Fatal("seqovl pattern missing")
	}
	if !bytes.Equal(first.payload[10:], real[:1]) {
		t.Fatal("first real byte is not after the pattern")
	}

	second, err := parsePacket(segs[1])
	if err != nil {
		t.Fatal(err)
	}
	if second.seq != 1001 {
		t.Fatalf("second seq %d, want 1001", second.seq)
	}
	if !bytes.Equal(second.payload, real[1:]) {
		t.Fatal("second segment payload mismatch")
	}
}

func TestFakeTCPSegment(t *testing.T) {
	raw := makeTCPPacket([]byte("real"), 12345, 443, 500)
	p, err := parsePacket(raw)
	if err != nil {
		t.Fatal(err)
	}
	fake := []byte("fake-tls")
	s := &Strategy{fake: true, repeats: 3, cutoff: 1, ttl: 3, fakeRaw: fake}
	segs, drop, err := s.apply(p, true)
	if err != nil {
		t.Fatal(err)
	}
	if drop {
		t.Fatal("fake-only dropped the original")
	}
	if len(segs) != 3 {
		t.Fatalf("got %d segments, want 3", len(segs))
	}
	for i, seg := range segs {
		q, err := parsePacket(seg)
		if err != nil {
			t.Fatal(err)
		}
		if q.seq != 500 || !bytes.Equal(q.payload, fake) {
			t.Fatalf("segment %d seq=%d payload=%q", i, q.seq, q.payload)
		}
		if q.ip4.TTL != 3 {
			t.Fatalf("segment %d TTL=%d, want 3", i, q.ip4.TTL)
		}
	}
}

func TestFakeUDP(t *testing.T) {
	raw := makeUDPPacket([]byte("real"), 40000, 50001)
	p, err := parsePacket(raw)
	if err != nil {
		t.Fatal(err)
	}
	fake := []byte("quic")
	s := &Strategy{fake: true, repeats: 2, cutoff: 1, ttl: 3, fakeRaw: fake}
	packets, drop, err := s.apply(p, true)
	if err != nil {
		t.Fatal(err)
	}
	if drop {
		t.Fatal("UDP fake dropped the original")
	}
	if len(packets) != 2 {
		t.Fatalf("got %d datagrams, want 2", len(packets))
	}
	for i, packet := range packets {
		q, err := parsePacket(packet)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(q.payload, fake) || q.ip4.TTL != 3 {
			t.Fatalf("datagram %d payload=%q ttl=%d", i, q.payload, q.ip4.TTL)
		}
	}
}

func TestCutoffPassesLaterPackets(t *testing.T) {
	raw := makeTCPPacket([]byte("data"), 12345, 443, 1)
	p, err := parsePacket(raw)
	if err != nil {
		t.Fatal(err)
	}
	s := &Strategy{fake: true, repeats: 1, cutoff: 1, ttl: 3, fakeRaw: []byte("fake")}
	segs, drop, err := s.apply(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 0 || drop {
		t.Fatalf("later packet was desynced: %d segments, drop=%v", len(segs), drop)
	}
}
