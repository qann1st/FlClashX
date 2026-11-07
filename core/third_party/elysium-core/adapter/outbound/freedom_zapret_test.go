package outbound

import (
	"runtime"
	"testing"

	"github.com/metacubex/mihomo/common/structure"
	"github.com/metacubex/mihomo/transport/zapret"
)

// zapret is Windows-only; everywhere else the block is ignored and the
// original fragment/finalmask path stays in effect.
func TestFreedomZapretIgnoredOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows needs WinDivert")
	}
	proxy, err := NewFreedom(FreedomOption{
		Name:   "ZAPRET",
		Zapret: &zapret.Option{TCP: []zapret.StrategyOption{{Desync: "fake"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if proxy.zapret != nil {
		t.Fatal("zapret engine created off Windows")
	}
}

func TestFreedomZapretFallsBackToFragmentOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows needs WinDivert")
	}
	_, err := NewFreedom(FreedomOption{
		Name:     "ZAPRET",
		Zapret:   &zapret.Option{TCP: []zapret.StrategyOption{{Desync: "fake"}}},
		Fragment: &FreedomFragmentOption{Packets: "tlshello", Length: "1-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFreedomZapretOptionDecode(t *testing.T) {
	decoder := structure.NewDecoder(structure.Option{
		TagName:          "proxy",
		WeaklyTypedInput: true,
		KeyReplacer:      structure.DefaultKeyReplacer,
	})
	mapping := map[string]any{
		"name": "ZAPRET",
		"type": "freedom",
		"zapret": map[string]any{
			"tcp": []any{map[string]any{
				"ports":     "443,2053,2083,2087,2096,8443",
				"desync":    "fake,multisplit",
				"split-pos": "1",
				"seqovl":    681,
				"repeats":   8,
				"cutoff":    1,
				"ttl":       3,
				"fooling":   "ts",
				"fake-tls":  "builtin:tls-google",
			}},
			"udp": []any{map[string]any{
				"ports":   "19294-19344,50000-50100",
				"desync":  "fake",
				"repeats": 6,
				"cutoff":  4,
				"ttl":     3,
				"fake":    "builtin:discord-udp",
			}},
		},
	}
	option := &FreedomOption{}
	if err := decoder.Decode(mapping, option); err != nil {
		t.Fatal(err)
	}
	if option.Zapret == nil {
		t.Fatal("zapret block was not decoded")
	}
	cfg, err := zapret.Parse(option.Zapret)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TCPFor(443) == nil || cfg.UDPFor(50001) == nil {
		t.Fatal("strategies were not built")
	}
}
