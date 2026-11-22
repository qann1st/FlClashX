//go:build !go1.24

package zapret2

import (
	"errors"
	"io"
	"io/fs"
)

func openCustomAssets(string) (fs.FS, io.Closer, error) {
	return nil, nil, errors.New("zapret2 assets-dir requires Go 1.24 or newer")
}
