//go:build windows && amd64

package zapret2

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/metacubex/mihomo/log"
)

const maxCaptures = 256

var (
	activeEngines sync.Map
	divertMu      sync.Mutex
	divert        *winDivert
)

type Engine struct {
	config settings
	wd     *winDivert

	mu      sync.Mutex
	retired bool
	worker  *workerProcess
	flows   map[*capture]struct{}
}

func New(options []string, assetsDir string) (*Engine, error) {
	if _, err := strategyArgs(options); err != nil {
		return nil, err
	}
	wd, err := probeWinDivert()
	if err != nil {
		return nil, fmt.Errorf("zapret2 WinDivert: %w (Windows administrator, WinDivert.dll and WinDivert64.sys required)", err)
	}
	config := settings{Options: append([]string(nil), options...), AssetsDir: assetsDir}
	worker, err := launchWorker(config, workerStartupTimeout)
	if err != nil {
		return nil, err
	}
	worker.stop()
	return &Engine{config: config, wd: wd, flows: make(map[*capture]struct{})}, nil
}

func probeWinDivert() (*winDivert, error) {
	divertMu.Lock()
	defer divertMu.Unlock()
	if divert == nil {
		wd, err := loadWinDivert()
		if err != nil {
			return nil, err
		}
		divert = wd
	}
	handle, err := divert.Open("false")
	if err != nil {
		return nil, err
	}
	return divert, divert.Close(handle)
}

func launchWorker(config settings, timeout time.Duration) (*workerProcess, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	command := exec.Command(executable, "--"+WorkerFlag)
	command.Env = []string{}
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return startWorker(command, config, timeout)
}

// Attach is called on a connected TCP or UDP socket before it carries payload.
func (e *Engine) Attach(ctx context.Context, conn net.Conn, tcp bool) (func() error, error) {
	local, err := netip.ParseAddrPort(conn.LocalAddr().String())
	if err != nil {
		return nil, err
	}
	remote, err := netip.ParseAddrPort(conn.RemoteAddr().String())
	if err != nil {
		return nil, err
	}
	flow, err := newSocketFlow(local, remote, tcp)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e.retired {
		return nil, net.ErrClosed
	}
	if len(e.flows) >= maxCaptures {
		return nil, errors.New("zapret2 flow limit reached")
	}
	if e.worker != nil {
		select {
		case <-e.worker.done:
			e.worker = nil
		default:
		}
	}
	if e.worker == nil {
		timeout := workerStartupTimeout
		if deadline, ok := ctx.Deadline(); ok {
			if remaining := time.Until(deadline); remaining < timeout {
				timeout = remaining
			}
		}
		worker, err := launchWorker(e.config, timeout)
		if err != nil {
			return nil, err
		}
		e.worker = worker
		activeEngines.Store(e, struct{}{})
		go e.watch(worker)
	}
	err = ctx.Err()
	var handle syscall.Handle
	if err == nil {
		handle, err = e.wd.Open(flow.filter())
	}
	if err != nil {
		if len(e.flows) == 0 {
			worker := e.worker
			e.worker = nil
			e.removeIfIdle()
			worker.kill()
		}
		return nil, fmt.Errorf("open zapret2 capture: %w", err)
	}
	f := &capture{flow: flow, engine: e, worker: e.worker, conn: conn, handle: handle, done: make(chan struct{})}
	e.flows[f] = struct{}{}
	go f.loop()
	return f.close, nil
}

func (e *Engine) watch(worker *workerProcess) {
	<-worker.done
	defer worker.output.Close()
	e.mu.Lock()
	flows := make([]*capture, 0)
	for f := range e.flows {
		if f.worker == worker {
			flows = append(flows, f)
		}
	}
	if e.worker == worker {
		e.worker = nil
	}
	e.removeIfIdle()
	e.mu.Unlock()
	for _, f := range flows {
		_ = f.close()
	}
	if len(flows) != 0 {
		log.Warnln("[zapret2] %v", worker.exitError())
	}
}

func (e *Engine) removeIfIdle() {
	if len(e.flows) == 0 && e.worker == nil {
		activeEngines.Delete(e)
	}
}

// Close retires this configuration. The last active socket stops its worker.
func (e *Engine) Close() error {
	e.mu.Lock()
	e.retired = true
	worker := e.stopIfDrained()
	e.mu.Unlock()
	if worker != nil {
		worker.stop()
	}
	return nil
}

func (e *Engine) stopIfDrained() *workerProcess {
	if !e.retired || len(e.flows) != 0 {
		return nil
	}
	worker := e.worker
	e.worker = nil
	e.removeIfIdle()
	return worker
}

func Shutdown() {
	activeEngines.Range(func(key, _ any) bool {
		e := key.(*Engine)
		e.mu.Lock()
		e.retired = true
		worker := e.worker
		e.worker = nil
		flows := make([]*capture, 0, len(e.flows))
		for f := range e.flows {
			flows = append(flows, f)
		}
		e.removeIfIdle()
		e.mu.Unlock()
		if worker != nil {
			worker.kill()
		}
		for _, f := range flows {
			_ = f.close()
		}
		if worker != nil {
			worker.stop()
		}
		return true
	})
}

type capture struct {
	flow    socketFlow
	engine  *Engine
	worker  *workerProcess
	conn    net.Conn
	handle  syscall.Handle
	mu      sync.Mutex
	closing bool
	done    chan struct{}
	err     error
}

func (f *capture) close() error {
	f.mu.Lock()
	if !f.closing {
		f.closing = true
		f.engine.wd.Shutdown(f.handle)
	}
	f.mu.Unlock()
	<-f.done
	return f.err
}

func (f *capture) finish() {
	f.mu.Lock()
	f.closing = true
	f.err = errors.Join(f.engine.wd.Close(f.handle), f.conn.Close())
	f.mu.Unlock()
	e := f.engine
	e.mu.Lock()
	delete(e.flows, f)
	worker := e.stopIfDrained()
	e.removeIfIdle()
	e.mu.Unlock()
	if worker != nil {
		worker.stop()
	}
	close(f.done)
}

func (f *capture) loop() {
	defer f.finish()
	var address divertAddress
	buffer := make([]byte, packetLimit)
	wd := f.engine.wd
	for {
		n, err := wd.Recv(f.handle, buffer, &address)
		f.mu.Lock()
		closing := f.closing
		f.mu.Unlock()
		if closing {
			return
		}
		if err != nil {
			log.Warnln("[zapret2] receive packet: %v", err)
			return
		}
		packet := buffer[:n]
		outbound := f.flow.matchesPacket(packet, true)
		inbound := !outbound && f.flow.matchesPacket(packet, false)
		result := packetResult{keep: true}
		if (outbound || inbound) && !address.Impostor() {
			result, err = f.worker.process(packet, outbound)
			if err != nil {
				log.Warnln("[zapret2] packet processing: %v", err)
				return
			}
		}
		for _, output := range result.packets {
			if !f.flow.matchesPacket(output, outbound) {
				log.Warnln("[zapret2] worker returned a packet outside its flow")
				f.worker.kill()
				return
			}
		}
		rebuilt := address
		rebuilt.MarkRebuilt(f.flow.remote.Addr().Is6(), f.flow.tcp)
		for _, output := range result.packets {
			if err := wd.Send(f.handle, output, &rebuilt); err != nil {
				log.Warnln("[zapret2] send modified packet: %v", err)
				return
			}
		}
		if result.keep {
			if err := wd.Send(f.handle, packet, &address); err != nil {
				log.Warnln("[zapret2] forward packet: %v", err)
				return
			}
		}
	}
}
