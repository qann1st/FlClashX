//go:build go1.24

package zapret2

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWorkerProcess(t *testing.T) {
	if os.Getenv("PRIZRAK_TEST_ZAPRET2_WORKER") == "1" {
		if err := RunWorker(os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	worker := func() *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestWorkerProcess$")
		cmd.Env = append(os.Environ(), "PRIZRAK_TEST_ZAPRET2_WORKER=1")
		return cmd
	}
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "profile.lua"), []byte("custom = fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := startWorker(worker(), settings{AssetsDir: assets, Options: []string{
		"--lua-init=@/custom/profile.lua",
		"--blob=first:0x01020304",
		"--blob=second:0x05060708",
		"--filter-udp=443",
		"--lua-desync=custom:blob=first:payload=all",
		"--new=alternate",
		"--filter-udp=444",
		"--lua-desync=custom:blob=second:payload=all",
	}}, workerStartupTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer p.stop()

	var wg sync.WaitGroup
	for _, profile := range []struct {
		port byte
		tail []byte
	}{{0xbb, []byte{1, 2, 3, 4}}, {0xbc, []byte{5, 6, 7, 8}}} {
		profile := profile
		wg.Add(1)
		go func() {
			defer wg.Done()
			packet := []byte{
				0x45, 0, 0, 32, 0, 0, 0, 0, 64, 17, 0, 0, 192, 0, 2, 10, 198, 51, 100, 10,
				0xc3, 0x50, 0x01, profile.port, 0, 12, 0, 0, 9, 9, 9, 9,
			}
			result, err := p.process(packet, true)
			if err != nil || !result.keep || len(result.packets) != 1 || !bytes.HasSuffix(result.packets[0], profile.tail) {
				t.Errorf("profile %d: %+v, %v", profile.port, result, err)
			}
		}()
	}
	wg.Wait()
	p.kill()
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not exit")
	}
	if _, err := p.process([]byte{1}, true); err == nil {
		t.Fatal("dead worker accepted request")
	}
	if invalid, err := startWorker(worker(), settings{Options: []string{"--lua-desync=missing_function"}}, workerStartupTimeout); err == nil {
		invalid.stop()
		t.Fatal("invalid strategy started")
	} else if !strings.Contains(err.Error(), "missing_function") {
		t.Fatal(err)
	}
}

func TestWorkerExitBeforeHandshake(t *testing.T) {
	const env = "PRIZRAK_TEST_ZAPRET2_EARLY_EXIT"
	if os.Getenv(env) == "1" {
		_, _ = readFrame(os.Stdin, 1024)
		os.Exit(2)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWorkerExitBeforeHandshake$")
	cmd.Env = append(os.Environ(), env+"=1")
	start := time.Now()
	p, err := startWorker(cmd, settings{}, 5*time.Second)
	if p != nil {
		p.stop()
	}
	if err == nil || time.Since(start) >= 4*time.Second {
		t.Fatalf("startup must notice EOF without waiting for its timeout: %s, %v", time.Since(start), err)
	}
}
