//go:build !android && elysium_core

package main

import (
	"fmt"
	"os"

	"github.com/metacubex/mihomo/transport/zapret2"
)

func runWorkerMode(args []string) bool {
	if len(args) != 1 || args[0] != "--"+zapret2.WorkerFlag {
		return false
	}
	if err := zapret2.RunWorker(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return true
}
