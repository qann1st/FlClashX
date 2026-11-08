package core

import (
	"testing"

	"github.com/metacubex/mihomo/component/powersave"
)

func TestSetPowersaveTakesTheModesTheCoreKnows(t *testing.T) {
	previous := powersave.Current()
	t.Cleanup(func() { powersave.Set(previous) })

	if !handleSetPowersave(int(powersave.Background)) {
		t.Fatal("background was refused")
	}
	if !handleSetPowersave(int(powersave.Idle)) {
		t.Fatal("idle was refused")
	}
	if !handleSetPowersave(int(powersave.Active)) {
		t.Fatal("active was refused")
	}

	if handleSetPowersave(-1) {
		t.Fatal("a negative mode was accepted")
	}
	if handleSetPowersave(int(powersave.Idle) + 1) {
		t.Fatal("a mode past idle was accepted")
	}
}
