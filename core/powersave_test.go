package core

import (
	"testing"

	"github.com/metacubex/mihomo/component/powersave"
)

func TestSetPowersaveAcceptsOnlyCoreModes(t *testing.T) {
	previous := powersave.Current()
	t.Cleanup(func() { powersave.Set(previous) })

	for _, mode := range []powersave.Mode{
		powersave.Active,
		powersave.Background,
		powersave.Idle,
	} {
		if !handleSetPowersave(int(mode)) {
			t.Fatalf("mode %v was refused", mode)
		}
	}
	if handleSetPowersave(-1) || handleSetPowersave(int(powersave.Idle)+1) {
		t.Fatal("an invalid power-save mode was accepted")
	}
}
