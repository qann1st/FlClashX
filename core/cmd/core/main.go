//go:build !android

package main

import (
	"core"
	"fmt"
	"os"
)

func main() {
	args := os.Args
	if len(args) <= 1 {
		fmt.Fprintln(os.Stderr, "Arguments error")
		os.Exit(1)
	}
	if runWorkerMode(args[1:]) {
		return
	}
	go core.ExitOnTermination()
	core.StartServer(args[1])
}
