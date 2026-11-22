//go:build !windows || !amd64

package adapter_test

import (
	"errors"
	"testing"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/config"
	"github.com/metacubex/mihomo/transport/zapret2"
)

func TestZapret2YAMLReportsUnsupportedPlatform(t *testing.T) {
	raw, err := config.UnmarshalRawConfig([]byte(`proxies:
  - name: ZAPRET2
    type: zapret2
    options:
      - "--filter-tcp=443"
      - "--lua-desync=fake"
rules:
  - MATCH,ZAPRET2
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw.Proxy) != 1 {
		t.Fatalf("got %d proxies", len(raw.Proxy))
	}
	_, err = adapter.ParseProxy(raw.Proxy[0])
	if !errors.Is(err, zapret2.ErrUnsupported) {
		t.Fatalf("expected unsupported platform, got %v", err)
	}
}
