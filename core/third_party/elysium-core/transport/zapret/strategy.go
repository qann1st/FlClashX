package zapret

import "errors"

// apply returns the packets to send and whether the original packet must be
// dropped. first reports that this data packet is within the configured cutoff.
func (s *Strategy) apply(p *packetInfo, first bool) ([][]byte, bool, error) {
	if !first {
		return nil, false, nil
	}
	if p.isTCP() {
		return s.applyTCP(p)
	}
	return s.applyUDP(p)
}

func (s *Strategy) applyTCP(p *packetInfo) ([][]byte, bool, error) {
	var out [][]byte
	if s.fake {
		if len(s.fakeRaw) == 0 {
			return nil, false, errors.New("empty fake-tls payload")
		}
		for i := 0; i < s.repeats; i++ {
			b, err := buildTCP(p, p.seq, s.fakeRaw, s.ttl)
			if err != nil {
				return nil, false, err
			}
			out = append(out, b)
		}
	}
	if !s.split {
		return out, false, nil
	}

	var positions []int
	for _, pos := range s.splitAt {
		if pos < len(p.payload) {
			positions = append(positions, pos)
		}
	}
	if len(positions) == 0 {
		return out, false, nil
	}

	// seqovl, as zapret DESYNC_MULTISPLIT: the first segment starts seqovl
	// bytes before the real stream, so the pattern sits before the window
	// and the peer drops it, while the real prefix after it stays valid.
	if s.seqovl > 0 {
		pattern := repeatTo(s.fakeRaw, s.seqovl)
		if len(pattern) != s.seqovl {
			return out, false, errors.New("empty seqovl pattern")
		}
		split := positions[0]
		firstPayload := make([]byte, 0, len(pattern)+split)
		firstPayload = append(firstPayload, pattern...)
		firstPayload = append(firstPayload, p.payload[:split]...)
		seg, err := buildTCP(p, p.seq-uint32(s.seqovl), firstPayload, 0)
		if err != nil {
			return nil, false, err
		}
		out = append(out, seg)
		if split < len(p.payload) {
			seg, err = buildTCP(p, p.seq+uint32(split), p.payload[split:], 0)
			if err != nil {
				return nil, false, err
			}
			out = append(out, seg)
		}
		return out, true, nil
	}

	from := 0
	for _, pos := range positions {
		if pos <= from {
			continue
		}
		seg, err := buildTCP(p, p.seq+uint32(from), p.payload[from:pos], 0)
		if err != nil {
			return nil, false, err
		}
		out = append(out, seg)
		from = pos
	}
	if from < len(p.payload) {
		seg, err := buildTCP(p, p.seq+uint32(from), p.payload[from:], 0)
		if err != nil {
			return nil, false, err
		}
		out = append(out, seg)
	}
	return out, true, nil
}

func (s *Strategy) applyUDP(p *packetInfo) ([][]byte, bool, error) {
	if !s.fake || len(s.fakeRaw) == 0 {
		return nil, false, nil
	}
	out := make([][]byte, 0, s.repeats)
	for i := 0; i < s.repeats; i++ {
		b, err := buildUDP(p, s.fakeRaw, s.ttl)
		if err != nil {
			return nil, false, err
		}
		out = append(out, b)
	}
	return out, false, nil
}

func repeatTo(pattern []byte, n int) []byte {
	if n <= 0 || len(pattern) == 0 {
		return nil
	}
	out := make([]byte, 0, n)
	for len(out) < n {
		take := n - len(out)
		if take > len(pattern) {
			take = len(pattern)
		}
		out = append(out, pattern[:take]...)
	}
	return out
}
