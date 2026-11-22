//go:build windows && amd64

package zapret2

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	windivertLayerNetwork     = 0
	windivertPriority         = 1
	windivertParamQueueLength = 0
	windivertParamQueueSize   = 2

	// One WinDivert handle is opened per flow, so its kernel queues must be
	// small: the default 4 MB queue per handle adds up fast.
	windivertQueueLength = 512
	windivertQueueSize   = 256 * 1024
)

// windivertAddress mirrors WINDIVERT_ADDRESS from windivert.h. The bitfield
// word is kept opaque: it is copied from the received packet and reused when
// the crafted packet is injected.
type divertAddress struct {
	Timestamp int64
	Flags     uint32
	Reserved2 uint32
	Data      [64]byte
}

func (a *divertAddress) MarkRebuilt(ipv6, tcp bool) {
	a.Flags &^= 7 << 21
	if !ipv6 {
		a.Flags |= 1 << 21 // IPChecksum
	}
	if tcp {
		a.Flags |= 1 << 22 // TCPChecksum
	} else {
		a.Flags |= 1 << 23 // UDPChecksum
	}
}

func (a *divertAddress) Impostor() bool { return a.Flags&(1<<19) != 0 }

type winDivert struct {
	openFn  *syscall.Proc
	recvFn  *syscall.Proc
	sendFn  *syscall.Proc
	shutFn  *syscall.Proc
	closeFn *syscall.Proc
	paramFn *syscall.Proc
}

func loadWinDivert() (*winDivert, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	dll, err := syscall.LoadDLL(filepath.Join(filepath.Dir(executable), "WinDivert.dll"))
	if err != nil {
		return nil, fmt.Errorf("load WinDivert.dll: %w (put WinDivert.dll and WinDivert64.sys next to the exe)", err)
	}
	w := &winDivert{}
	for _, proc := range []struct {
		name string
		dest **syscall.Proc
	}{
		{"WinDivertOpen", &w.openFn}, {"WinDivertRecv", &w.recvFn},
		{"WinDivertSend", &w.sendFn}, {"WinDivertShutdown", &w.shutFn},
		{"WinDivertClose", &w.closeFn}, {"WinDivertSetParam", &w.paramFn},
	} {
		*proc.dest, err = dll.FindProc(proc.name)
		if err != nil {
			_ = dll.Release()
			return nil, fmt.Errorf("WinDivert.dll: %s: %w", proc.name, err)
		}
	}
	return w, nil
}

func winErr(err error) error {
	if err != nil && err != syscall.Errno(0) {
		return err
	}
	return syscall.EINVAL
}

func (w *winDivert) Open(filter string) (syscall.Handle, error) {
	fp, err := syscall.BytePtrFromString(filter)
	if err != nil {
		return 0, err
	}
	r1, _, e := w.openFn.Call(
		uintptr(unsafe.Pointer(fp)),
		windivertLayerNetwork,
		windivertPriority,
		0,
	)
	runtime.KeepAlive(fp)
	if r1 == 0 || r1 == uintptr(syscall.InvalidHandle) {
		return 0, winErr(e)
	}
	h := syscall.Handle(r1)
	// Keep per-flow kernel queues small: the default 4 MB queue per handle
	// multiplied by every intercepted connection is significant memory.
	if ok, _, err := w.paramFn.Call(uintptr(h), windivertParamQueueLength, windivertQueueLength); ok == 0 {
		_ = w.Close(h)
		return 0, fmt.Errorf("set WinDivert queue length: %w", winErr(err))
	}
	if ok, _, err := w.paramFn.Call(uintptr(h), windivertParamQueueSize, windivertQueueSize); ok == 0 {
		_ = w.Close(h)
		return 0, fmt.Errorf("set WinDivert queue size: %w", winErr(err))
	}
	return h, nil
}

func (w *winDivert) Recv(h syscall.Handle, buf []byte, addr *divertAddress) (int, error) {
	var n uint32
	r1, _, e := w.recvFn.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		uintptr(unsafe.Pointer(&n)),
		uintptr(unsafe.Pointer(addr)),
	)
	runtime.KeepAlive(buf)
	runtime.KeepAlive(addr)
	if r1 == 0 {
		return 0, winErr(e)
	}
	if n == 0 || int(n) > len(buf) {
		return 0, fmt.Errorf("WinDivertRecv length %d", n)
	}
	return int(n), nil
}

func (w *winDivert) Send(h syscall.Handle, data []byte, addr *divertAddress) error {
	if len(data) == 0 {
		return nil
	}
	var n uint32
	r1, _, e := w.sendFn.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)),
		uintptr(unsafe.Pointer(&n)),
		uintptr(unsafe.Pointer(addr)),
	)
	runtime.KeepAlive(data)
	runtime.KeepAlive(addr)
	if r1 == 0 {
		return winErr(e)
	}
	return nil
}

func (w *winDivert) Close(h syscall.Handle) error {
	w.Shutdown(h)
	r1, _, e := w.closeFn.Call(uintptr(h))
	if r1 == 0 {
		return winErr(e)
	}
	return nil
}

func (w *winDivert) Shutdown(h syscall.Handle) {
	_, _, _ = w.shutFn.Call(uintptr(h), 3) // WINDIVERT_SHUTDOWN_BOTH
}
