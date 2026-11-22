package adapter_test

import (
	"testing"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/config"
	C "github.com/metacubex/mihomo/constant"
)

func TestFreedomYAMLIgnoresRemovedZapret(t *testing.T) {
	raw, err := config.UnmarshalRawConfig([]byte(`proxies:
  - name: legacy
    type: freedom
    zapret:
      tcp:
        - desync: removed-strategy
    fragment:
      packets: tlshello
      length: "1-1"
    finalmask:
      udp:
        - type: fake
          payloads:
            - builtin: discord-udp
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw.Proxy) != 1 {
		t.Fatalf("got %d proxies, want 1", len(raw.Proxy))
	}
	proxy, err := adapter.ParseProxy(raw.Proxy[0])
	if err != nil {
		t.Fatal(err)
	}
	if proxy.Type() != C.Freedom || !proxy.SupportUDP() {
		t.Fatalf("expected freedom with UDP support, got %s", proxy.Type())
	}
}
