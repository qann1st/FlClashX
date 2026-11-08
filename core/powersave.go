package core

import "github.com/metacubex/mihomo/component/powersave"

// The host reports what the device is doing so the core's periodic work slows
// down when nobody is looking: health checks, the smart group's timers and the
// speed ticker all read this mode. Built without the with_powersave tag, Set is
// a no-op and the core keeps its full-rate schedule.
func handleSetPowersave(mode int) bool {
	if mode < int(powersave.Active) || mode > int(powersave.Idle) {
		return false
	}
	powersave.Set(powersave.Mode(mode))
	return true
}
