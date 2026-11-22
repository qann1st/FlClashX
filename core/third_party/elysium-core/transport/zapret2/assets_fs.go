//go:build go1.24

package zapret2

import (
	"io"
	"io/fs"
	"os"
)

func openCustomAssets(dir string) (fs.FS, io.Closer, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, err
	}
	return root.FS(), root, nil
}
