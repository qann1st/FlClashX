//go:build !android && !elysium_core

package main

func runWorkerMode(_ []string) bool {
	return false
}
