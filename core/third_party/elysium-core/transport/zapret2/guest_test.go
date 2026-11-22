package zapret2

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestExampleProfilesInitialize(t *testing.T) {
	data, err := os.ReadFile("../../docs/zapret2.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Proxies []struct {
			Options []string `yaml:"options"`
		} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Proxies) != 1 {
		t.Fatalf("expected one example proxy, got %d", len(config.Proxies))
	}
	args, err := strategyArgs(config.Proxies[0].Options)
	if err != nil {
		t.Fatal(err)
	}
	g, err := newGuest(args, "")
	if err != nil {
		t.Fatal(err)
	}
	g.close()
}

func TestDocumentedTLSProfile(t *testing.T) {
	args, err := strategyArgs([]string{
		"--filter-tcp=443", "--filter-l7=tls", "--payload=tls_client_hello",
		"--lua-desync=fake:blob=fake_default_tls:repeats=6",
	})
	if err != nil {
		t.Fatal(err)
	}
	g, err := newGuest(args, "")
	if err != nil {
		t.Fatal(err)
	}
	defer g.close()

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	if err := server.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	go func() { _ = tls.Client(client, &tls.Config{ServerName: "youtube.com"}).Handshake() }()
	var header [5]byte
	if _, err := io.ReadFull(server, header[:]); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, binary.BigEndian.Uint16(header[3:5]))
	if _, err := io.ReadFull(server, body); err != nil {
		t.Fatal(err)
	}
	packet := []byte{
		0x45, 0, 0, 0, 0, 0, 0, 0, 64, 6, 0, 0, 192, 0, 2, 10, 198, 51, 100, 10,
		0xc3, 0x50, 0x01, 0xbb, 0, 0, 0, 100, 0, 0, 0, 200, 0x50, 0x18, 0xff, 0xff, 0, 0, 0, 0,
	}
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)+len(header)+len(body)))
	packet = append(append(packet, header[:]...), body...)
	result, err := g.process(packet, true, func(p []byte) bool { return !bytes.Equal(p, packet) })
	if err != nil || !result.keep || len(result.packets) != 6 {
		t.Fatalf("TLS profile: got keep=%v, emitted=%d, err=%v", result.keep, len(result.packets), err)
	}
}

// The Discord media profile from kinlay0's general (ALT14).bat uses these
// upstream options. A built-in TLS blob stands in for its external tls_sferum.
func TestALT14DiscordMediaProfile(t *testing.T) {
	args, err := strategyArgs([]string{
		"--lua-init=tls_sferum = fake_default_tls",
		"--name=discord media",
		"--filter-tcp=2053,2083,2087,2096,8443",
		"--hostlist-domains=discord.media",
		"--payload=tls_client_hello",
		"--lua-desync=fake:blob=tls_sferum:repeats=8:tcp_ts=-600000",
		"--lua-desync=multisplit:pos=2:seqovl=661:seqovl_pattern=tls_sferum",
		"--new",
		"--name=discord voice",
		"--filter-udp=19294-19344,50000-50100",
		"--filter-l7=discord,stun",
		"--payload=discord_ip_discovery,stun",
		"--lua-desync=fake:blob=fake_default_quic:repeats=6",
	})
	if err != nil {
		t.Fatal(err)
	}
	g, err := newGuest(args, "")
	if err != nil {
		t.Fatal(err)
	}
	defer g.close()
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	if err := server.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	go func() { _ = tls.Client(client, &tls.Config{ServerName: "discord.media"}).Handshake() }()
	var header [5]byte
	if _, err := io.ReadFull(server, header[:]); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, binary.BigEndian.Uint16(header[3:5]))
	if _, err := io.ReadFull(server, body); err != nil {
		t.Fatal(err)
	}
	packet := []byte{
		0x45, 0, 0, 0, 0, 0, 0, 0, 64, 6, 0, 0, 192, 0, 2, 10, 198, 51, 100, 10,
		0xc3, 0x50, 0x20, 0xfb, 0, 0, 0, 100, 0, 0, 0, 200, 0x50, 0x18, 0xff, 0xff, 0, 0, 0, 0,
	}
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)+len(header)+len(body)))
	packet = append(append(packet, header[:]...), body...)
	result, err := g.process(packet, true, func([]byte) bool { return true })
	if err != nil || len(result.packets) < 2 {
		t.Fatalf("ALT14 Discord media: emitted=%d, err=%v", len(result.packets), err)
	}
}

func TestGuestLoadsOriginalLuaAndProcessesUDP(t *testing.T) {
	cfg, err := strategyArgs([]string{
		"--blob=sample:0x01020304",
		"--filter-udp=443",
		"--payload=all",
		"--lua-desync=fake:blob=sample:payload=all",
	})
	if err != nil {
		t.Fatal(err)
	}
	g, err := newGuest(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	defer g.close()

	flow := socketFlow{local: netip.MustParseAddrPort("192.0.2.10:50000"),
		remote: netip.MustParseAddrPort("198.51.100.10:443")}
	packet := []byte{
		0x45, 0, 0, 32, 0, 0, 0, 0, 64, 17, 0, 0, 192, 0, 2, 10, 198, 51, 100, 10,
		0xc3, 0x50, 0x01, 0xbb, 0, 12, 0, 0, 9, 9, 9, 9,
	}
	result, err := g.process(packet, true, func(output []byte) bool { return flow.matchesPacket(output, true) })
	if err != nil {
		t.Fatal(err)
	}
	if !result.keep || len(result.packets) != 1 || !bytes.HasSuffix(result.packets[0], []byte{1, 2, 3, 4}) {
		t.Fatalf("unexpected result from upstream Lua: %+v", result)
	}
	binary.BigEndian.PutUint16(packet[22:24], 444)
	result, err = g.process(packet, true, func([]byte) bool { return true })
	if err != nil || !result.keep || len(result.packets) != 0 {
		t.Fatalf("non-matching profile: %+v, %v", result, err)
	}
}

func TestGuestReportsLuaConfigurationError(t *testing.T) {
	cfg, err := strategyArgs([]string{
		"--lua-desync=unknown_test_function",
	})
	if err != nil {
		t.Fatal(err)
	}
	g, err := newGuest(cfg, "")
	if err == nil {
		g.close()
		t.Fatal("invalid Lua strategy was accepted")
	}
	if !strings.Contains(err.Error(), "unknown_test_function") {
		t.Fatal(err)
	}
}

func TestGuestRunsUpstreamTCPSplit(t *testing.T) {
	cfg, err := strategyArgs([]string{
		"--filter-tcp=443", "--payload=all", "--lua-desync=multisplit:pos=2:payload=all",
	})
	if err != nil {
		t.Fatal(err)
	}
	g, err := newGuest(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	defer g.close()
	packet := []byte{
		0x45, 0, 0, 45, 0, 0, 0, 0, 64, 6, 0, 0, 192, 0, 2, 10, 198, 51, 100, 10,
		0xc3, 0x50, 0x01, 0xbb, 0, 0, 0, 100, 0, 0, 0, 200, 0x50, 0x18, 0xff, 0xff, 0, 0, 0, 0,
		'a', 'b', 'c', 'd', 'e',
	}
	flow := socketFlow{local: netip.MustParseAddrPort("192.0.2.10:50000"),
		remote: netip.MustParseAddrPort("198.51.100.10:443"), tcp: true}
	result, err := g.process(packet, true, func(output []byte) bool { return flow.matchesPacket(output, true) })
	if err != nil {
		t.Fatal(err)
	}
	if result.keep || len(result.packets) != 2 ||
		!bytes.HasSuffix(result.packets[0], []byte("ab")) || !bytes.HasSuffix(result.packets[1], []byte("cde")) {
		t.Fatalf("unexpected TCP split: %+v", result)
	}
}
