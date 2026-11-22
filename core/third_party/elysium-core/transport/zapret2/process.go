package zapret2

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

type workerProcess struct {
	command *exec.Cmd
	input   *os.File
	output  *os.File
	done    chan struct{}
	slot    chan struct{}
	waitErr error
}

const workerStartupTimeout = 10 * time.Second

func startWorker(command *exec.Cmd, config settings, timeout time.Duration) (*workerProcess, error) {
	args, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	childInput, input, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer childInput.Close()
	output, childOutput, err := os.Pipe()
	if err != nil {
		_ = input.Close()
		return nil, err
	}
	defer childOutput.Close()
	command.Stdin, command.Stdout = childInput, childOutput
	if err := command.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		return nil, err
	}
	// The child's exit must produce EOF even during the startup handshake.
	_ = childInput.Close()
	_ = childOutput.Close()
	p := &workerProcess{command: command, input: input, output: output, done: make(chan struct{}), slot: make(chan struct{}, 1)}
	go func() {
		p.waitErr = command.Wait()
		_ = input.Close()
		close(p.done)
	}()
	reply, err := p.exchange(protocolVersion, args, timeout)
	if err == nil {
		switch {
		case reply[0] == replyError:
			err = errors.New(string(reply[1:]))
		case len(reply) != 2 || reply[0] != replyPass || reply[1] != protocolVersion:
			err = errors.New("invalid zapret2 worker handshake")
		}
	}
	if err != nil {
		p.stop()
		return nil, fmt.Errorf("start zapret2 worker: %w", errors.Join(err, p.waitErr))
	}
	return p, nil
}

func (p *workerProcess) exchange(tag byte, data []byte, timeout time.Duration) ([]byte, error) {
	queue := time.NewTimer(timeout)
	defer queue.Stop()
	select {
	case <-p.done:
		return nil, p.exitError()
	case p.slot <- struct{}{}:
		defer func() { <-p.slot }()
	case <-queue.C:
		return nil, errors.New("zapret2 worker is busy")
	}

	timer := time.AfterFunc(timeout, p.kill)
	defer timer.Stop()
	if err := writeFrame(p.input, tag, data); err != nil {
		p.kill()
		return nil, fmt.Errorf("write to zapret2 worker: %w", err)
	}
	reply, err := readFrame(p.output, maxFrame)
	if err != nil {
		p.kill()
		return nil, fmt.Errorf("read from zapret2 worker: %w", err)
	}
	return reply, nil
}

func (p *workerProcess) process(packet []byte, outbound bool) (packetResult, error) {
	var direction byte
	if outbound {
		direction = 1
	}
	reply, err := p.exchange(direction, packet, time.Second)
	if err != nil {
		return packetResult{}, err
	}
	result, err := decodeResult(reply)
	if err != nil {
		p.kill()
	}
	return result, err
}

func (p *workerProcess) exitError() error {
	if p.waitErr != nil {
		return fmt.Errorf("zapret2 worker exited: %w", p.waitErr)
	}
	return errors.New("zapret2 worker exited")
}

func (p *workerProcess) kill() {
	_ = p.command.Process.Kill()
}

func (p *workerProcess) stop() {
	timer := time.AfterFunc(time.Second, p.kill)
	defer timer.Stop()
	_ = p.input.Close()
	<-p.done
	_ = p.output.Close()
}
