// Package fake is the "fake" UDP finalmask: before a real datagram it sends
// decoy datagrams from the same socket, as zapret's --dpi-desync=fake does.
//
// The decoys confuse a DPI that follows a UDP flow (QUIC, Discord voice,
// STUN, game traffic): it sees the fake payload first and the flow is
// classified from it, while the peer ignores the decoy and answers the real
// datagram. A low TTL keeps the decoys from reaching the peer at all.
package fake

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/transport/finalmask"
	"github.com/metacubex/mihomo/transport/finalmask/custom"
)

// Option is the mask's settings as written in the config.
//
// The mask sends Repeats fake datagrams before the real one, for the first
// Cutoff datagrams of every destination. TTL, when above zero, is the IP TTL
// of the fakes, so they expire before the peer; the socket's own TTL is
// restored before the real datagram. Payloads are tried by destination port:
// the first entry whose Ports match is used, an entry without Ports is the
// fallback, and without any entry the real datagram is repeated as the fake.
type Option struct {
	Repeats  *int            `proxy:"repeats,omitempty"`
	Cutoff   *int            `proxy:"cutoff,omitempty"`
	TTL      int             `proxy:"ttl,omitempty"`
	Delay    finalmask.Range `proxy:"delay,omitempty"`
	Payloads []PayloadOption `proxy:"payloads,omitempty"`
}

// PayloadOption is one fake payload. Exactly one of Builtin, File, Hex,
// Base64, Text, Packet or Random gives its bytes.
type PayloadOption struct {
	// Builtin picks a payload compiled into the core: discord-udp (also
	// discord/quic), stun or dtls. No external file is needed.
	Builtin string `proxy:"builtin,omitempty"`
	File    string `proxy:"file,omitempty"`
	Hex     string `proxy:"hex,omitempty"`
	Base64  string `proxy:"base64,omitempty"`
	Text    string `proxy:"text,omitempty"`
	// Packet with Type reads the bytes as custom.ParseBytes does: Type str
	// (default), hex or base64, or a list of byte values.
	Packet any    `proxy:"packet,omitempty"`
	Type   string `proxy:"type,omitempty"`
	// Random sends RandomMin..RandomMax fresh random bytes per datagram.
	Random *finalmask.Range `proxy:"random,omitempty"`
	// Ports limits the payload to destination ports, "443" or
	// "50000-50100,3478"; empty matches every port.
	Ports string `proxy:"ports,omitempty"`
}

type portRange struct {
	from, to int
}

// Payload is one built fake payload.
type Payload struct {
	data      []byte
	randomMin int
	randomMax int
	ports     []portRange
}

// matches reports whether the payload applies to a destination port.
func (p *Payload) matches(port int) bool {
	if len(p.ports) == 0 {
		return true
	}
	for _, r := range p.ports {
		if port >= r.from && port <= r.to {
			return true
		}
	}
	return false
}

// bytes returns the payload for one fake datagram.
func (p *Payload) bytes() []byte {
	if p.randomMax < p.randomMin || p.randomMax <= 0 {
		return p.data
	}
	n := int(finalmask.RandBetween(int64(p.randomMin), int64(p.randomMax)+1))
	if n <= 0 {
		return nil
	}
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func (o PayloadOption) build() (*Payload, error) {
	ports, err := parsePorts(o.Ports)
	if err != nil {
		return nil, err
	}
	payload := &Payload{ports: ports}
	set := 0
	for _, present := range []bool{o.Builtin != "", o.File != "", o.Hex != "", o.Base64 != "", o.Text != "", o.Packet != nil, o.Random != nil} {
		if present {
			set++
		}
	}
	if set == 0 {
		return nil, errors.New("payload needs one of builtin, file, hex, base64, text, packet or random")
	}
	if set > 1 {
		return nil, errors.New("payload takes only one of builtin, file, hex, base64, text, packet or random")
	}
	switch {
	case o.Builtin != "":
		b64, ok := builtinPayloads[strings.ToLower(strings.TrimSpace(o.Builtin))]
		if !ok {
			return nil, fmt.Errorf("unknown builtin payload %q, want discord-udp, discord, quic, stun or dtls", o.Builtin)
		}
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("builtin payload %s: %w", o.Builtin, err)
		}
		payload.data = data
	case o.File != "":
		// A relative path is resolved against the config directory, as the
		// path of a rule provider is, not against the process working dir.
		data, err := os.ReadFile(C.Path.Resolve(o.File))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", o.File, err)
		}
		payload.data = data
	case o.Hex != "":
		data, err := custom.ParseBytes(o.Hex, "hex")
		if err != nil {
			return nil, fmt.Errorf("hex payload: %w", err)
		}
		payload.data = data
	case o.Base64 != "":
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(o.Base64))
		if err != nil {
			return nil, fmt.Errorf("base64 payload: %w", err)
		}
		payload.data = data
	case o.Text != "":
		payload.data = []byte(o.Text)
	case o.Packet != nil:
		data, err := custom.ParseBytes(o.Packet, o.Type)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			return nil, errors.New("empty packet payload")
		}
		payload.data = data
	default:
		lo, hi := o.Random.From, o.Random.To
		if lo > hi {
			lo, hi = hi, lo
		}
		if lo <= 0 {
			return nil, errors.New("random payload size must be positive")
		}
		payload.randomMin, payload.randomMax = lo, hi
	}
	return payload, nil
}

func parsePorts(s string) ([]portRange, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []portRange
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		from, to, err := parseRange(part)
		if err != nil {
			return nil, fmt.Errorf("ports %q: %w", part, err)
		}
		if from < 0 || to > 65535 {
			return nil, fmt.Errorf("ports %q: port must be in 0-65535", part)
		}
		out = append(out, portRange{from: from, to: to})
	}
	return out, nil
}

func parseRange(s string) (int, int, error) {
	a, b, isRange := strings.Cut(s, "-")
	from, err := strconv.Atoi(strings.TrimSpace(a))
	if err != nil {
		return 0, 0, errors.New("invalid port")
	}
	to := from
	if isRange {
		if to, err = strconv.Atoi(strings.TrimSpace(b)); err != nil {
			return 0, 0, errors.New("invalid port")
		}
	}
	if from > to {
		from, to = to, from
	}
	return from, to, nil
}
