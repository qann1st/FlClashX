//go:build !android && elysium_core

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/metacubex/mihomo/transport/zapret2"
)

func TestZapret2WorkerMode(t *testing.T) {
	const child = "FLCLASHX_TEST_ZAPRET2_WORKER"
	if os.Getenv(child) == "1" {
		os.Args = []string{os.Args[0], "--" + zapret2.WorkerFlag}
		main()
		os.Exit(0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestZapret2WorkerMode$")
	cmd.Env = append(os.Environ(), child+"=1")
	// Start the worker with a no-op strategy and close its input after the handshake.
	const config = `{"Options":["--lua-init=identity = function() end","--lua-desync=identity"]}`
	request := append(binary.BigEndian.AppendUint32(nil, 1+uint32(len(config))), 1)
	cmd.Stdin = bytes.NewReader(append(request, config...))
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("worker did not handle its startup flag: %v", err)
	}
	want := []byte{0, 0, 0, 2, 0, 1}
	if !bytes.Equal(output, want) {
		t.Fatalf("worker response = %q, want %q", output, want)
	}
}
