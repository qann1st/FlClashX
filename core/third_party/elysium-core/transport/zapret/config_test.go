package zapret

import "testing"

func TestParseTCPStrategy(t *testing.T) {
	cfg, err := Parse(&Option{TCP: []StrategyOption{{
		Ports:    "443,2053,2083,2087,2096,8443",
		Desync:   "fake,multisplit",
		SplitPos: "1",
		SeqOvl:   681,
		Repeats:  8,
		Cutoff:   1,
		TTL:      3,
		Fooling:  "ts",
		FakeTLS:  "builtin:tls-google",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.TCPFor(443)
	if s == nil {
		t.Fatal("no strategy for 443")
	}
	if !s.fake || !s.split {
		t.Fatal("fake and split are not both set")
	}
	if s.seqovl != 681 || s.repeats != 8 || s.cutoff != 1 || s.ttl != 3 {
		t.Fatalf("unexpected fields: %+v", s)
	}
	if len(s.fakeRaw) != 681 {
		t.Fatalf("tls-google length %d", len(s.fakeRaw))
	}
	if cfg.TCPFor(2053) == nil || cfg.TCPFor(80) != nil {
		t.Fatal("port matching is wrong")
	}
}

func TestParseUDPStrategy(t *testing.T) {
	cfg, err := Parse(&Option{UDP: []StrategyOption{{
		Ports:   "19294-19344,50000-50100",
		Desync:  "fake",
		Repeats: 6,
		Cutoff:  4,
		TTL:     3,
		Fake:    "builtin:discord-udp",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.UDPFor(50001)
	if s == nil {
		t.Fatal("no strategy for 50001")
	}
	if !s.fake || len(s.fakeRaw) != 1200 {
		t.Fatalf("unexpected udp strategy: %+v", s)
	}
	if cfg.UDPFor(443) != nil {
		t.Fatal("port 443 should not match")
	}
}

func TestCatchAll(t *testing.T) {
	cfg, err := Parse(&Option{TCP: []StrategyOption{{Desync: "split"}}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TCPFor(1) == nil || cfg.TCPFor(65535) == nil {
		t.Fatal("empty ports should match every port")
	}
}

func TestRejects(t *testing.T) {
	for name, o := range map[string]*Option{
		"disorder":     {TCP: []StrategyOption{{Desync: "disorder"}}},
		"udp split":    {UDP: []StrategyOption{{Desync: "split"}}},
		"bad split":    {TCP: []StrategyOption{{Desync: "split", SplitPos: "x"}}},
		"bad port":     {TCP: []StrategyOption{{Ports: "x"}}},
		"bad payload":  {TCP: []StrategyOption{{Desync: "fake", FakeTLS: "builtin:nope"}}},
		"bad fooling":  {TCP: []StrategyOption{{Desync: "fake", Fooling: "badsum"}}},
		"bad udp fake": {UDP: []StrategyOption{{Desync: "fake", Fake: "builtin:nope"}}},
	} {
		if _, err := Parse(o); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}
