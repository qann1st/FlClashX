package core

import "github.com/metacubex/mihomo/component/powersave"

// The host reports whether it is visible.  The Core uses this to slow its own
// scheduled health checks and to reap idle sessions while it is in background.
// The build tag is enabled on every platform; a host which never sends this
// method remains Active, preserving the Core's normal behaviour.
func handleSetPowersave(mode int) bool {
	if mode < int(powersave.Active) || mode > int(powersave.Idle) {
		return false
	}
	powersave.Set(powersave.Mode(mode))
	return true
}
