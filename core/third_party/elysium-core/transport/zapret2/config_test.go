package zapret2

import (
	"testing"

	"golang.org/x/exp/slices"
)

func TestStrategyArguments(t *testing.T) {
	options := []string{"--filter-tcp=443", "--lua-desync=fake", "--new", "--filter-udp=443", "--lua-desync=fake"}
	args, err := strategyArgs(options)
	if err != nil || !slices.Equal(args[len(args)-len(options):], options) {
		t.Fatalf("strategy arguments = %q, %v", args, err)
	}
	for _, option := range []string{"--wf-tcp-out=443", "--qnum=100", "--intercept", "--dry-run", "--new=", "--lua-init=x\x00y", "@config.txt"} {
		if _, err := strategyArgs(append(slices.Clone(options), option)); err == nil {
			t.Errorf("accepted %q", option)
		}
	}
}
