package zapret

import (
	"errors"
	"fmt"
	"net/netip"
	"sync"

	"github.com/metacubex/gopacket"
	"github.com/metacubex/gopacket/layers"
)

var layersOnce sync.Once

func ensureLayers() {
	layersOnce.Do(layers.Init)
}

type packetInfo struct {
	ip4     *layers.IPv4
	ip6     *layers.IPv6
	tcp     *layers.TCP
	udp     *layers.UDP
	raw     []byte
	payload []byte

	srcIP, dstIP     netip.Addr
	srcPort, dstPort uint16
	seq              uint32
}

func (p *packetInfo) isTCP() bool  { return p.tcp != nil }
func (p *packetInfo) isIPv6() bool { return p.ip6 != nil }

// parsePacket reads an IPv4/IPv6 packet with TCP or UDP. Fragmented packets and
// IPv6 packets with extension headers are refused: their L4 header is not at a
// fixed offset, so they are passed through unmodified.
func parsePacket(data []byte) (*packetInfo, error) {
	ensureLayers()
	if len(data) == 0 {
		return nil, errors.New("empty packet")
	}
	var first gopacket.LayerType
	switch data[0] >> 4 {
	case 4:
		first = layers.LayerTypeIPv4
	case 6:
		first = layers.LayerTypeIPv6
	default:
		return nil, fmt.Errorf("unknown IP version %d", data[0]>>4)
	}
	packet := gopacket.NewPacket(data, first, gopacket.DecodeOptions{Lazy: false, NoCopy: false})
	if el := packet.ErrorLayer(); el != nil {
		return nil, el.Error()
	}
	info := &packetInfo{raw: data}
	if ip, ok := packet.Layer(layers.LayerTypeIPv4).(*layers.IPv4); ok {
		if ip.FragOffset != 0 || ip.Flags&layers.IPv4MoreFragments != 0 {
			return nil, errors.New("fragmented IPv4 packet")
		}
		info.ip4 = ip
		info.srcIP, _ = netip.AddrFromSlice(ip.SrcIP)
		info.dstIP, _ = netip.AddrFromSlice(ip.DstIP)
	} else if ip, ok := packet.Layer(layers.LayerTypeIPv6).(*layers.IPv6); ok {
		if packet.Layer(layers.LayerTypeIPv6HopByHop) != nil ||
			packet.Layer(layers.LayerTypeIPv6Routing) != nil ||
			packet.Layer(layers.LayerTypeIPv6Fragment) != nil ||
			packet.Layer(layers.LayerTypeIPv6Destination) != nil {
			return nil, errors.New("IPv6 extension header")
		}
		info.ip6 = ip
		info.srcIP, _ = netip.AddrFromSlice(ip.SrcIP)
		info.dstIP, _ = netip.AddrFromSlice(ip.DstIP)
	} else {
		return nil, errors.New("no IP layer")
	}
	if tcp, ok := packet.Layer(layers.LayerTypeTCP).(*layers.TCP); ok {
		info.tcp = tcp
		info.srcPort = uint16(tcp.SrcPort)
		info.dstPort = uint16(tcp.DstPort)
		info.seq = tcp.Seq
		info.payload = tcp.Payload
		return info, nil
	}
	if udp, ok := packet.Layer(layers.LayerTypeUDP).(*layers.UDP); ok {
		info.udp = udp
		info.srcPort = uint16(udp.SrcPort)
		info.dstPort = uint16(udp.DstPort)
		info.payload = udp.Payload
		return info, nil
	}
	return nil, errors.New("no TCP/UDP layer")
}

type checksumLayer interface {
	SetNetworkLayerForChecksum(gopacket.NetworkLayer) error
}

func (p *packetInfo) serialize(ttl int, l4 gopacket.SerializableLayer, payload []byte) ([]byte, error) {
	ensureLayers()
	setter, ok := l4.(checksumLayer)
	if !ok {
		return nil, errors.New("layer has no checksum setter")
	}
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	if p.ip4 != nil {
		ip := *p.ip4
		ip.Length = 0
		ip.Checksum = 0
		if ttl > 0 {
			ip.TTL = uint8(ttl)
		}
		if err := setter.SetNetworkLayerForChecksum(&ip); err != nil {
			return nil, err
		}
		if err := gopacket.SerializeLayers(buf, opts, &ip, l4, gopacket.Payload(payload)); err != nil {
			return nil, err
		}
	} else {
		ip := *p.ip6
		ip.Length = 0
		if ttl > 0 {
			ip.HopLimit = uint8(ttl)
		}
		if err := setter.SetNetworkLayerForChecksum(&ip); err != nil {
			return nil, err
		}
		if err := gopacket.SerializeLayers(buf, opts, &ip, l4, gopacket.Payload(payload)); err != nil {
			return nil, err
		}
	}
	return append([]byte(nil), buf.Bytes()...), nil
}

// buildTCP makes a TCP segment with the given sequence number and payload,
// preserving the original flags, window and options (timestamps included).
func buildTCP(p *packetInfo, seq uint32, payload []byte, ttl int) ([]byte, error) {
	if p.tcp == nil {
		return nil, errors.New("not a TCP packet")
	}
	tcp := *p.tcp
	tcp.Seq = seq
	tcp.Checksum = 0
	tcp.Options = append([]layers.TCPOption(nil), p.tcp.Options...)
	tcp.Padding = append([]byte(nil), p.tcp.Padding...)
	tcp.BaseLayer = layers.BaseLayer{}
	return p.serialize(ttl, &tcp, payload)
}

// buildUDP makes a UDP datagram with the given payload.
func buildUDP(p *packetInfo, payload []byte, ttl int) ([]byte, error) {
	if p.udp == nil {
		return nil, errors.New("not a UDP packet")
	}
	udp := *p.udp
	udp.Length = 0
	udp.Checksum = 0
	udp.BaseLayer = layers.BaseLayer{}
	return p.serialize(ttl, &udp, payload)
}
