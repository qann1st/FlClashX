package zapret2

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const WorkerFlag = "internal-zapret2-worker"

const (
	replyPass byte = iota
	replyReplace
	replyError
	maxFrame = byteLimit + 4*sendLimit + 1
)

const protocolVersion byte = 1

type settings struct {
	Options   []string
	AssetsDir string
}

// RunWorker serves one configuration over inherited pipes. The guest has no
// capture or socket API. Its output is checked again by the parent.
func RunWorker(input io.Reader, output io.Writer) error {
	frame, err := readFrame(input, argumentLimit*6+1024)
	if err != nil {
		return err
	}
	if frame[0] != protocolVersion {
		return writeError(output, errors.New("unsupported zapret2 protocol version"))
	}
	var config settings
	if err := json.Unmarshal(frame[1:], &config); err != nil {
		return err
	}
	args, err := strategyArgs(config.Options)
	if err != nil {
		return writeError(output, err)
	}
	g, err := newGuest(args, config.AssetsDir)
	if err != nil {
		return writeError(output, err)
	}
	defer g.close()
	if err := writeFrame(output, replyPass, []byte{protocolVersion}); err != nil {
		return err
	}
	for {
		frame, err := readFrame(input, packetLimit+1)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if len(frame) < 2 || frame[0] > 1 {
			return errors.New("bad zapret2 packet request")
		}
		packet := frame[1:]
		src, dst, tcp, ok := packetEndpoints(packet)
		if !ok {
			return errors.New("invalid zapret2 input packet")
		}
		result, err := g.process(packet, frame[0] == 1, func(output []byte) bool {
			from, to, outputTCP, valid := packetEndpoints(output)
			return valid && from == src && to == dst && outputTCP == tcp
		})
		if err != nil {
			return writeError(output, err)
		}
		frame = []byte{replyReplace}
		if result.keep {
			frame[0] = replyPass
		}
		for _, p := range result.packets {
			frame = binary.BigEndian.AppendUint32(frame, uint32(len(p)))
			frame = append(frame, p...)
		}
		if err := writeFrame(output, frame[0], frame[1:]); err != nil {
			return err
		}
	}
}

func writeError(output io.Writer, err error) error {
	message := err.Error()
	if len(message) > 4095 {
		message = message[:4095]
	}
	return writeFrame(output, replyError, []byte(message))
}

func writeFrame(w io.Writer, tag byte, data []byte) error {
	var header [5]byte
	binary.BigEndian.PutUint32(header[:4], uint32(len(data)+1))
	header[4] = tag
	if n, err := w.Write(header[:]); err != nil {
		return err
	} else if n != len(header) {
		return io.ErrShortWrite
	}
	if len(data) == 0 {
		return nil
	}
	n, err := w.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

func readFrame(r io.Reader, limit uint32) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(header[:])
	if n == 0 || n > limit {
		return nil, fmt.Errorf("invalid zapret2 frame length %d", n)
	}
	frame := make([]byte, n)
	_, err := io.ReadFull(r, frame)
	return frame, err
}

func decodeResult(frame []byte) (packetResult, error) {
	if len(frame) == 0 || frame[0] > replyError {
		return packetResult{}, errors.New("invalid zapret2 verdict")
	}
	if frame[0] == replyError {
		return packetResult{}, errors.New(string(frame[1:]))
	}
	result := packetResult{keep: frame[0] == replyPass}
	for frame = frame[1:]; len(frame) != 0; {
		if len(frame) < 4 || len(result.packets) >= sendLimit {
			return packetResult{}, errors.New("invalid zapret2 packet list")
		}
		n := binary.BigEndian.Uint32(frame)
		frame = frame[4:]
		if n == 0 || n > packetLimit || int(n) > len(frame) {
			return packetResult{}, errors.New("invalid zapret2 output packet length")
		}
		result.packets = append(result.packets, frame[:n])
		frame = frame[n:]
	}
	return result, nil
}
