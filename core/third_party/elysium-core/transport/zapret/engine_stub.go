//go:build !windows

package zapret

func newBackend() (backend, error) {
	return nil, ErrUnsupported
}
