package zapret2

import (
	"encoding/binary"
	"fmt"
	"net/netip"
)

type socketFlow struct {
	local, remote netip.AddrPort
	tcp           bool
}

func newSocketFlow(local, remote netip.AddrPort, tcp bool) (socketFlow, error) {
	localIP, remoteIP := local.Addr().Unmap().WithZone(""), remote.Addr().Unmap().WithZone("")
	if !localIP.IsValid() || !remoteIP.IsValid() || localIP.IsUnspecified() || remoteIP.IsUnspecified() ||
		localIP.IsMulticast() || remoteIP.IsMulticast() || localIP.BitLen() != remoteIP.BitLen() ||
		local.Addr().Zone() != "" || remote.Addr().Zone() != "" ||
		local.Port() == 0 || remote.Port() == 0 {
		return socketFlow{}, fmt.Errorf("zapret2 requires a concrete unicast socket tuple: %s to %s", local, remote)
	}
	return socketFlow{local: netip.AddrPortFrom(localIP, local.Port()), remote: netip.AddrPortFrom(remoteIP, remote.Port()), tcp: tcp}, nil
}

func (f socketFlow) filter() string {
	proto := "udp"
	if f.tcp {
		proto = "tcp"
	}
	ip := "ipv6"
	if f.local.Addr().Is4() {
		ip = "ip"
	}
	return fmt.Sprintf("%s and %s and !impostor and (("+
		"outbound and %s.SrcAddr == %s and %s.DstAddr == %s and %s.SrcPort == %d and %s.DstPort == %d) or ("+
		"(inbound or loopback) and %s.SrcAddr == %s and %s.DstAddr == %s and %s.SrcPort == %d and %s.DstPort == %d))",
		ip, proto,
		ip, f.local.Addr(), ip, f.remote.Addr(), proto, f.local.Port(), proto, f.remote.Port(),
		ip, f.remote.Addr(), ip, f.local.Addr(), proto, f.remote.Port(), proto, f.local.Port())
}

func (f socketFlow) matchesPacket(packet []byte, outbound bool) bool {
	src, dst, tcp, ok := packetEndpoints(packet)
	if !ok || tcp != f.tcp {
		return false
	}
	if !outbound {
		src, dst = dst, src
	}
	return dst == f.remote && src == f.local
}

func packetEndpoints(packet []byte) (src, dst netip.AddrPort, tcp, ok bool) {
	if len(packet) < 20 {
		return
	}
	var srcIP, dstIP netip.Addr
	var header int
	var proto byte
	switch packet[0] >> 4 {
	case 4:
		header, proto = int(packet[0]&15)*4, packet[9]
		// Drop all IP fragments from the output capability. They cannot be
		// attributed reliably to the current socket by their transport ports.
		if header != 20 || binary.BigEndian.Uint16(packet[6:8])&0x3fff != 0 {
			return
		}
		if int(binary.BigEndian.Uint16(packet[2:4])) != len(packet) {
			return
		}
		srcIP = netip.AddrFrom4([4]byte(packet[12:16]))
		dstIP = netip.AddrFrom4([4]byte(packet[16:20]))
	case 6:
		if len(packet) < 40 || int(binary.BigEndian.Uint16(packet[4:6])) != len(packet)-40 {
			return
		}
		header, proto = 40, packet[6]
		srcIP = netip.AddrFrom16([16]byte(packet[8:24]))
		dstIP = netip.AddrFrom16([16]byte(packet[24:40]))
		for proto == 0 || proto == 60 || proto == 51 {
			if len(packet)-header < 2 {
				return
			}
			length := (int(packet[header+1]) + 1) * 8
			if proto == 51 {
				length = (int(packet[header+1]) + 2) * 4
			}
			proto = packet[header]
			header += length
		}
	default:
		return
	}
	if proto == 6 {
		if len(packet)-header < 20 || packet[header+12]>>4 < 5 || int(packet[header+12]>>4)*4 > len(packet)-header {
			return
		}
	} else if proto != 17 || len(packet)-header < 8 || int(binary.BigEndian.Uint16(packet[header+4:])) != len(packet)-header {
		return
	}
	return netip.AddrPortFrom(srcIP.Unmap(), binary.BigEndian.Uint16(packet[header:])),
		netip.AddrPortFrom(dstIP.Unmap(), binary.BigEndian.Uint16(packet[header+2:])), proto == 6, true
}
