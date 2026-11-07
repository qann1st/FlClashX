// Package zapret implements the packet-level TCP/UDP desync of zapret for the
// freedom outbound on Windows. The strategies are the socket-independent part;
// engine_windows.go intercepts the flow's packets with WinDivert and reinjects
// crafted segments, as zapret's nfq/desync.c does on Linux.
package zapret

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/metacubex/mihomo/transport/finalmask/fake"
)

// Option is the zapret block of a freedom proxy.
type Option struct {
	TCP []StrategyOption `proxy:"tcp,omitempty"`
	UDP []StrategyOption `proxy:"udp,omitempty"`
}

// StrategyOption is one strategy, written with zapret's option names.
type StrategyOption struct {
	// Ports limits the strategy to destination ports, "443,50000-50100";
	// empty or "*" matches every port.
	Ports string `proxy:"ports,omitempty"`
	// Desync is the mode list: "fake", "split", "multisplit" or
	// "fake,multisplit" (ALT11). Only these are supported.
	Desync string `proxy:"desync,omitempty"`
	// SplitPos is where the payload is cut, "1" or "1,2,3".
	SplitPos string `proxy:"split-pos,omitempty"`
	// SeqOvl is the number of pattern bytes prepended before the first
	// segment, as zapret's --dpi-desync-split-seqovl.
	SeqOvl int `proxy:"seqovl,omitempty"`
	// Repeats is --dpi-desync-repeats.
	Repeats int `proxy:"repeats,omitempty"`
	// Cutoff is --dpi-desync-cutoff=nN: desync the first N data packets;
	// 0 means every packet.
	Cutoff int `proxy:"cutoff,omitempty"`
	// TTL is the TTL of the fake packets (--dpi-desync-ttl); 0 uses the
	// default 3, fooling: none keeps the original TTL.
	TTL int `proxy:"ttl,omitempty"`
	// Fooling accepts "ttl" and "ts" (both use a low TTL here) or "none".
	Fooling string `proxy:"fooling,omitempty"`
	// FakeTLS is the TCP fake payload: builtin:tls-google (default),
	// builtin:tls-microsoft, hex:... or base64:...
	FakeTLS string `proxy:"fake-tls,omitempty"`
	// Fake is the UDP fake payload: builtin:discord-udp (default),
	// builtin:quic, builtin:stun, hex:... or base64:...
	Fake string `proxy:"fake,omitempty"`
}

type portRange struct {
	from, to uint16
}

func (r portRange) contains(port uint16) bool { return port >= r.from && port <= r.to }

// Strategy is one parsed strategy.
type Strategy struct {
	ports   []portRange
	fake    bool
	split   bool
	splitAt []int
	seqovl  int
	repeats int
	cutoff  int
	ttl     int
	fakeRaw []byte
}

func (s *Strategy) matches(port uint16) bool {
	if len(s.ports) == 0 {
		return true
	}
	for _, r := range s.ports {
		if r.contains(port) {
			return true
		}
	}
	return false
}

// Config holds the parsed strategy lists.
type Config struct {
	tcp []*Strategy
	udp []*Strategy
}

func (c *Config) TCPFor(port uint16) *Strategy {
	for _, s := range c.tcp {
		if s.matches(port) {
			return s
		}
	}
	return nil
}

func (c *Config) UDPFor(port uint16) *Strategy {
	for _, s := range c.udp {
		if s.matches(port) {
			return s
		}
	}
	return nil
}

// Parse reads the option into strategies.
func Parse(o *Option) (*Config, error) {
	if o == nil {
		return nil, nil
	}
	cfg := &Config{}
	for i, item := range o.TCP {
		s, err := parseTCP(item)
		if err != nil {
			return nil, fmt.Errorf("zapret.tcp[%d]: %w", i, err)
		}
		cfg.tcp = append(cfg.tcp, s)
	}
	for i, item := range o.UDP {
		s, err := parseUDP(item)
		if err != nil {
			return nil, fmt.Errorf("zapret.udp[%d]: %w", i, err)
		}
		cfg.udp = append(cfg.udp, s)
	}
	return cfg, nil
}

func parseTCP(o StrategyOption) (*Strategy, error) {
	s := &Strategy{repeats: 1, cutoff: 1, ttl: 3, seqovl: o.SeqOvl}
	ports, err := parsePorts(o.Ports)
	if err != nil {
		return nil, err
	}
	s.ports = ports
	if o.Repeats > 0 {
		s.repeats = o.Repeats
	}
	if o.Cutoff > 0 {
		s.cutoff = o.Cutoff
	}
	if o.TTL > 0 {
		s.ttl = o.TTL
	}
	switch strings.ToLower(strings.TrimSpace(o.Fooling)) {
	case "", "ttl", "ts":
	case "none":
		s.ttl = 0
	default:
		return nil, fmt.Errorf("fooling %q is not supported, use ttl, ts or none", o.Fooling)
	}
	if s.seqovl < 0 {
		return nil, errors.New("seqovl can't be negative")
	}
	if s.seqovl > 65000 {
		return nil, errors.New("seqovl is too large")
	}

	modes := splitModes(o.Desync)
	if len(modes) == 0 {
		modes = []string{"fake", "multisplit"}
	}
	for _, m := range modes {
		switch m {
		case "fake":
			s.fake = true
		case "split", "split2", "multisplit":
			s.split = true
		default:
			return nil, fmt.Errorf("tcp desync %q is not supported (supported: fake, split, multisplit)", m)
		}
	}
	if s.fake {
		s.fakeRaw, err = parsePayload(o.FakeTLS, "builtin:tls-google")
		if err != nil {
			return nil, fmt.Errorf("fake-tls: %w", err)
		}
	} else if s.seqovl > 0 {
		// seqovl needs a pattern even when the fake stage is off.
		s.fakeRaw, err = parsePayload(o.FakeTLS, "builtin:tls-google")
		if err != nil {
			return nil, fmt.Errorf("fake-tls: %w", err)
		}
	}
	if s.split {
		s.splitAt, err = parseSplitPos(o.SplitPos)
		if err != nil {
			return nil, err
		}
	}
	return s, nil
}

func parseUDP(o StrategyOption) (*Strategy, error) {
	s := &Strategy{repeats: 1, cutoff: 1, ttl: 3}
	ports, err := parsePorts(o.Ports)
	if err != nil {
		return nil, err
	}
	s.ports = ports
	if o.Repeats > 0 {
		s.repeats = o.Repeats
	}
	if o.Cutoff > 0 {
		s.cutoff = o.Cutoff
	}
	if o.TTL > 0 {
		s.ttl = o.TTL
	}
	switch strings.ToLower(strings.TrimSpace(o.Fooling)) {
	case "", "ttl", "ts":
	case "none":
		s.ttl = 0
	default:
		return nil, fmt.Errorf("fooling %q is not supported, use ttl, ts or none", o.Fooling)
	}
	modes := splitModes(o.Desync)
	if len(modes) == 0 {
		modes = []string{"fake"}
	}
	for _, m := range modes {
		switch m {
		case "fake":
			s.fake = true
		default:
			return nil, fmt.Errorf("udp desync %q is not supported on Windows (supported: fake)", m)
		}
	}
	s.fakeRaw, err = parsePayload(o.Fake, "builtin:discord-udp")
	if err != nil {
		return nil, fmt.Errorf("fake: %w", err)
	}
	return s, nil
}

func splitModes(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseSplitPos(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return []int{1}, nil
	}
	seen := map[int]bool{}
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n <= 0 || n > 65000 {
			return nil, fmt.Errorf("split-pos %q is invalid", part)
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("split-pos is empty")
	}
	sort.Ints(out)
	return out, nil
}

func parsePorts(s string) ([]portRange, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "*" {
		return nil, nil
	}
	var out []portRange
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		a, b, isRange := strings.Cut(part, "-")
		from, err := strconv.ParseUint(strings.TrimSpace(a), 10, 16)
		if err != nil {
			return nil, fmt.Errorf("ports %q is invalid", part)
		}
		to := from
		if isRange {
			if to, err = strconv.ParseUint(strings.TrimSpace(b), 10, 16); err != nil {
				return nil, fmt.Errorf("ports %q is invalid", part)
			}
		}
		if from > to {
			from, to = to, from
		}
		out = append(out, portRange{from: uint16(from), to: uint16(to)})
	}
	return out, nil
}

func parsePayload(spec, def string) ([]byte, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		spec = def
	}
	if name, ok := strings.CutPrefix(spec, "builtin:"); ok {
		data, found := fake.Builtin(name)
		if !found {
			return nil, fmt.Errorf("unknown builtin payload %q", name)
		}
		return data, nil
	}
	if s, ok := strings.CutPrefix(spec, "hex:"); ok {
		data, err := hex.DecodeString(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
		if err != nil {
			return nil, fmt.Errorf("hex payload: %w", err)
		}
		return data, nil
	}
	if s, ok := strings.CutPrefix(spec, "base64:"); ok {
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
		if err != nil {
			return nil, fmt.Errorf("base64 payload: %w", err)
		}
		return data, nil
	}
	if data, found := fake.Builtin(spec); found {
		return data, nil
	}
	return nil, fmt.Errorf("unknown payload %q (use builtin:name, hex:... or base64:...)", spec)
}
