package zapret2

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"
	"time"

	"github.com/metacubex/wazero"
	"github.com/metacubex/wazero/api"
	"github.com/metacubex/wazero/experimental"
	"github.com/metacubex/wazero/imports/wasi_snapshot_preview1"
)

//go:embed assets/engine.wasm
var wasm []byte

//go:embed assets/lua assets/files
var embeddedFiles embed.FS

const (
	packetLimit = 65535
	sendLimit   = 128
	byteLimit   = 512 * 1024
)

var (
	compileOnce sync.Once
	compiled    wazero.CompiledModule
	wasmRuntime wazero.Runtime
	startError  error
)

type packetResult struct {
	packets [][]byte
	keep    bool
}

type packetEmission struct {
	accept   func([]byte) bool
	packets  [][]byte
	bytes    uint32
	rejected bool
}

type emissionKey struct{}

func emit(ctx context.Context, mod api.Module, ptr, size uint32) uint32 {
	send, _ := ctx.Value(emissionKey{}).(*packetEmission)
	if send == nil || size == 0 || size > packetLimit || len(send.packets) >= sendLimit || send.bytes+size > byteLimit {
		if send != nil {
			send.rejected = true
		}
		return 0
	}
	packet, ok := mod.Memory().Read(ptr, size)
	if !ok || !send.accept(packet) {
		send.rejected = true
		return 0
	}
	send.packets = append(send.packets, append([]byte(nil), packet...))
	send.bytes += size
	return 1
}

func compile() {
	ctx := context.Background()
	wasmRuntime = wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		WithCoreFeatures(api.CoreFeaturesV2|experimental.CoreFeaturesExceptionHandling).
		WithMemoryLimitPages(1024).WithCloseOnContextDone(true))
	_, startError = wasi_snapshot_preview1.Instantiate(ctx, wasmRuntime)
	if startError == nil {
		_, startError = wasmRuntime.NewHostModuleBuilder("prizrak").
			NewFunctionBuilder().WithFunc(emit).Export("send").Instantiate(ctx)
	}
	if startError == nil {
		compiled, startError = wasmRuntime.CompileModule(ctx, wasm)
	}
	if startError != nil {
		_ = wasmRuntime.Close(ctx)
	}
}

type guest struct {
	mod       api.Module
	processFn api.Function
	lengthFn  api.Function
	input     uint32
	output    uint32
	assets    io.Closer
}

type startupLog struct{ bytes.Buffer }

func (b *startupLog) Write(p []byte) (int, error) {
	n := len(p)
	if remain := 4096 - b.Len(); remain > 0 {
		if len(p) > remain {
			p = p[:remain]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}

func newGuest(args []string, assetsDir string) (_ *guest, err error) {
	compileOnce.Do(compile)
	if startError != nil {
		return nil, startError
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	files, err := fs.Sub(embeddedFiles, "assets")
	if err != nil {
		return nil, err
	}
	mounts := wazero.NewFSConfig().WithFSMount(files, "/")
	var assets io.Closer
	if assetsDir != "" {
		var customFiles fs.FS
		customFiles, assets, err = openCustomAssets(assetsDir)
		if err != nil {
			return nil, fmt.Errorf("open zapret2 assets: %w", err)
		}
		defer func() {
			if err != nil {
				_ = assets.Close()
			}
		}()
		mounts = mounts.WithFSMount(customFiles, "/custom")
	}
	var log startupLog
	mod, err := wasmRuntime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().
		WithName("").WithStartFunctions("_initialize").
		WithFSConfig(mounts).WithStdout(&log).WithStderr(&log).
		WithSysWalltime().WithSysNanotime().WithRandSource(rand.Reader))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = mod.Close(context.Background())
			if log.Len() != 0 {
				err = fmt.Errorf("%w: %s", err, strings.TrimSpace(log.String()))
			}
		}
	}()
	if err := initArgs(ctx, mod, args); err != nil {
		return nil, err
	}
	input, err := call(ctx, mod.ExportedFunction("zapret_input"))
	if err != nil {
		return nil, err
	}
	output, err := call(ctx, mod.ExportedFunction("zapret_output"))
	if err != nil {
		return nil, err
	}
	// Retain only startup diagnostics. Packet logs stay in the guest.
	log.Reset()
	log.Grow(4096)
	return &guest{mod: mod, input: input, output: output, assets: assets,
		processFn: mod.ExportedFunction("zapret_process"), lengthFn: mod.ExportedFunction("zapret_output_length")}, nil
}

func initArgs(ctx context.Context, mod api.Module, args []string) error {
	size := 4 * (len(args) + 1)
	for _, arg := range args {
		size += len(arg) + 1
	}
	argv, err := call(ctx, mod.ExportedFunction("malloc"), uint64(size))
	if err != nil || argv == 0 {
		return fmt.Errorf("allocate strategy arguments: %v", err)
	}
	mem, next := mod.Memory(), argv+4*uint32(len(args)+1)
	for i, arg := range args {
		value := append([]byte(arg), 0)
		if !mem.WriteUint32Le(argv+4*uint32(i), next) || !mem.Write(next, value) {
			return errors.New("write strategy arguments")
		}
		next += uint32(len(value))
	}
	if !mem.WriteUint32Le(argv+4*uint32(len(args)), 0) {
		return errors.New("write strategy argument terminator")
	}
	status, err := call(ctx, mod.ExportedFunction("zapret_init"), uint64(len(args)), uint64(argv))
	if err != nil {
		return fmt.Errorf("initialize zapret2: %w", err)
	}
	if status != 0 {
		return fmt.Errorf("initialize zapret2: status %d", status)
	}
	return nil
}

func call(ctx context.Context, fn api.Function, args ...uint64) (uint32, error) {
	if fn == nil {
		return 0, errors.New("missing zapret2 WASM export")
	}
	result, err := fn.Call(ctx, args...)
	if err != nil {
		return 0, err
	}
	if len(result) != 1 {
		return 0, errors.New("zapret2 WASM export returned no result")
	}
	return uint32(result[0]), nil
}

func (g *guest) close() {
	_ = g.mod.Close(context.Background())
	if g.assets != nil {
		_ = g.assets.Close()
	}
}

// process must be serialized by the caller. The callback limits guest output
// to the intercepted connection before returning a successful send verdict.
func (g *guest) process(packet []byte, outbound bool, accept func([]byte) bool) (packetResult, error) {
	if len(packet) == 0 || len(packet) > packetLimit || !g.mod.Memory().Write(g.input, packet) {
		return packetResult{}, errors.New("cannot write packet to zapret2")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	send := &packetEmission{accept: accept}
	ctx = context.WithValue(ctx, emissionKey{}, send)
	var direction uint64
	if outbound {
		direction = 1
	}
	verdict, err := call(ctx, g.processFn, uint64(len(packet)), direction)
	if err != nil {
		return packetResult{}, err
	}
	if send.rejected {
		return packetResult{}, errors.New("zapret2 strategy exceeded its packet limits or tried to emit outside its connection")
	}
	result := packetResult{packets: send.packets}
	switch verdict {
	case 0:
		result.keep = true
	case 1:
		length, err := call(ctx, g.lengthFn)
		if err != nil {
			return packetResult{}, err
		}
		data, ok := g.mod.Memory().Read(g.output, length)
		if !ok || length == 0 || length > packetLimit || !accept(data) ||
			len(result.packets) >= sendLimit || send.bytes+length > byteLimit {
			return packetResult{}, errors.New("zapret2 modified packet outside its connection")
		}
		result.packets = append(result.packets, append([]byte(nil), data...))
	case 2:
	default:
		return packetResult{}, fmt.Errorf("unexpected zapret2 verdict %d", verdict)
	}
	return result, nil
}
