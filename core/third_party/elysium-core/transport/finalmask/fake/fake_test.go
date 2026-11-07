package fake

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/transport/finalmask"
)

func intPtr(v int) *int { return &v }

func newPair(t *testing.T) (net.PacketConn, net.PacketConn) {
	t.Helper()
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	client, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return server, client
}

func readPackets(t *testing.T, conn net.PacketConn, n int) [][]byte {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var out [][]byte
	buf := make([]byte, 4096)
	for i := 0; i < n; i++ {
		read, _, err := conn.ReadFrom(buf)
		if err != nil {
			t.Fatalf("read %d of %d: %v", i+1, n, err)
		}
		out = append(out, append([]byte(nil), buf[:read]...))
	}
	return out
}

func equalAll(t *testing.T, got [][]byte, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d datagrams, want %d", len(got), len(want))
	}
	for i := range want {
		if string(got[i]) != want[i] {
			t.Fatalf("datagram %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// The first datagram of a destination gets repeats decoys, later ones pass.
func TestFakeSendsDecoysBeforeFirstDatagram(t *testing.T) {
	server, client := newPair(t)
	cfg, err := New(Option{
		Repeats:  intPtr(3),
		Cutoff:   intPtr(1),
		Payloads: []PayloadOption{{Hex: "c0ffee"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn := NewConn(cfg, client)
	if _, err := conn.WriteTo([]byte("real"), server.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.WriteTo([]byte("later"), server.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	equalAll(t, readPackets(t, server, 5), "\xc0\xff\xee", "\xc0\xff\xee", "\xc0\xff\xee", "real", "later")
}

// Without a payload the real datagram itself is the decoy, a plain repeat.
func TestFakeRepeatsTheRealDatagramWithoutPayload(t *testing.T) {
	server, client := newPair(t)
	cfg, err := New(Option{Repeats: intPtr(2), Cutoff: intPtr(1)})
	if err != nil {
		t.Fatal(err)
	}
	conn := NewConn(cfg, client)
	if _, err := conn.WriteTo([]byte("real"), server.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	equalAll(t, readPackets(t, server, 3), "real", "real", "real")
}

// The first payload whose ports match the destination is used.
func TestFakePicksPayloadByPort(t *testing.T) {
	server, client := newPair(t)
	port := strconv.Itoa(server.LocalAddr().(*net.UDPAddr).Port)
	cfg, err := New(Option{
		Repeats: intPtr(1),
		Cutoff:  intPtr(1),
		Payloads: []PayloadOption{
			{Hex: "aa", Ports: port},
			{Hex: "bb", Ports: "1-2"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn := NewConn(cfg, client)
	if _, err := conn.WriteTo([]byte("real"), server.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	equalAll(t, readPackets(t, server, 2), "\xaa", "real")
}

// cutoff 0 keeps sending decoys for every datagram.
func TestFakeCutoffZeroNeverStops(t *testing.T) {
	server, client := newPair(t)
	cfg, err := New(Option{
		Repeats:  intPtr(1),
		Cutoff:   intPtr(0),
		Payloads: []PayloadOption{{Text: "fake"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn := NewConn(cfg, client)
	for i := 0; i < 2; i++ {
		if _, err := conn.WriteTo([]byte("real"), server.LocalAddr()); err != nil {
			t.Fatal(err)
		}
	}
	equalAll(t, readPackets(t, server, 4), "fake", "real", "fake", "real")
}

// A random payload gets fresh bytes for every decoy.
func TestFakeRandomPayload(t *testing.T) {
	server, client := newPair(t)
	cfg, err := New(Option{
		Repeats:  intPtr(2),
		Cutoff:   intPtr(1),
		Payloads: []PayloadOption{{Random: &finalmask.Range{From: 4, To: 8}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn := NewConn(cfg, client)
	if _, err := conn.WriteTo([]byte("real"), server.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	for i, p := range readPackets(t, server, 2) {
		if len(p) < 4 || len(p) > 8 {
			t.Fatalf("decoy %d has %d bytes, want 4-8", i, len(p))
		}
	}
}

// A low decoy TTL is restored before the real datagram, so the real one is
// not lost.
func TestFakeTTLRestoresTheRealDatagram(t *testing.T) {
	server, client := newPair(t)
	cfg, err := New(Option{
		Repeats:  intPtr(1),
		Cutoff:   intPtr(1),
		TTL:      3,
		Payloads: []PayloadOption{{Text: "fake"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn := NewConn(cfg, client)
	if _, err := conn.WriteTo([]byte("real"), server.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	got := readPackets(t, server, 2)
	if string(got[1]) != "real" {
		t.Fatalf("real datagram = %q, want it sent after the decoy TTL is restored", got[1])
	}
}

// A wrapper that exposes the socket through Upstream, as another mask does,
// still lets the TTL probe reach it and sends the real datagram.
type upstreamConn struct{ net.PacketConn }

func (c *upstreamConn) Upstream() any { return c.PacketConn }

func TestFakeTTLThroughWrapper(t *testing.T) {
	server, client := newPair(t)
	cfg, err := New(Option{
		Repeats:  intPtr(1),
		Cutoff:   intPtr(1),
		TTL:      6,
		Payloads: []PayloadOption{{Text: "fake"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn := NewConn(cfg, &upstreamConn{PacketConn: client})
	if _, err := conn.WriteTo([]byte("real"), server.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	got := readPackets(t, server, 2)
	if string(got[1]) != "real" {
		t.Fatalf("real datagram = %q", got[1])
	}
}

// A relative payload file is resolved against the config directory, as a
// rule provider path is.
func TestPayloadFileResolvesAgainstHomeDir(t *testing.T) {
	dir := t.TempDir()
	old := C.Path.HomeDir()
	C.SetHomeDir(dir)
	t.Cleanup(func() { C.SetHomeDir(old) })
	if err := os.WriteFile(filepath.Join(dir, "fake.bin"), []byte("from-home"), 0o600); err != nil {
		t.Fatal(err)
	}
	server, client := newPair(t)
	cfg, err := New(Option{
		Repeats:  intPtr(1),
		Cutoff:   intPtr(1),
		Payloads: []PayloadOption{{File: "fake.bin"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn := NewConn(cfg, client)
	if _, err := conn.WriteTo([]byte("real"), server.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	equalAll(t, readPackets(t, server, 2), "from-home", "real")
}

// Built-in payloads need no file and carry the expected bytes.
func TestBuiltinPayloads(t *testing.T) {
	for name, wantLen := range map[string]int{
		"discord-udp": 1200,
		"discord":     1200,
		"quic":        1200,
		"stun":        20,
		"dtls":        111,
	} {
		cfg, err := New(Option{Payloads: []PayloadOption{{Builtin: name}}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := len(cfg.payloads[0].bytes()); got != wantLen {
			t.Fatalf("%s: %d bytes, want %d", name, got, wantLen)
		}
	}
}

func TestNewRejectsBadOptions(t *testing.T) {
	for name, o := range map[string]Option{
		"no repeats":      {Repeats: intPtr(0)},
		"negative":        {Cutoff: intPtr(-1)},
		"bad ttl":         {TTL: 300},
		"empty payload":   {Payloads: []PayloadOption{{}}},
		"two sources":     {Payloads: []PayloadOption{{Hex: "aa", Text: "b"}}},
		"bad hex":         {Payloads: []PayloadOption{{Hex: "zz"}}},
		"bad base64":      {Payloads: []PayloadOption{{Base64: "!"}}},
		"bad ports":       {Payloads: []PayloadOption{{Hex: "aa", Ports: "x"}}},
		"port too large":  {Payloads: []PayloadOption{{Hex: "aa", Ports: "70000"}}},
		"zero random":     {Payloads: []PayloadOption{{Random: &finalmask.Range{}}}},
		"missing file":    {Payloads: []PayloadOption{{File: "/nonexistent/fake.bin"}}},
		"unknown builtin": {Payloads: []PayloadOption{{Builtin: "nope"}}},
		"builtin+hex":     {Payloads: []PayloadOption{{Builtin: "stun", Hex: "aa"}}},
	} {
		if _, err := New(o); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}
