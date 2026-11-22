package zapret2

import (
	"errors"
	"fmt"
	"strings"
)

var ErrUnsupported = errors.New("zapret2 is supported only on Windows amd64")

const argumentLimit = 64 * 1024

func strategyArgs(options []string) ([]string, error) {
	if len(options) > 128 {
		return nil, errors.New("too many zapret2 options")
	}
	args := []string{"zapret2", "--lua-init=@/lua/zapret-lib.lua", "--lua-init=@/lua/zapret-antidpi.lua", "--lua-init=@/lua/zapret-auto.lua"}
	var size int
	hasStrategy := false
	for i, arg := range options {
		size += len(arg)
		if size > argumentLimit || strings.ContainsRune(arg, 0) {
			return nil, fmt.Errorf("zapret2 option %d exceeds the supported size or contains NUL", i+1)
		}
		key, value, hasValue := strings.Cut(arg, "=")
		switch key {
		case "--new", "--template", "--comment", "--ctrack-disable", "--ipcache-hostname", "--reasm-disable", "--payload-disable":
			if hasValue && value == "" {
				return nil, fmt.Errorf("zapret2 option %d: empty value for %s", i+1, key)
			}
		case "--skip":
			if hasValue {
				return nil, fmt.Errorf("zapret2 option %d: %s takes no value", i+1, key)
			}
		case "--name", "--import", "--cookie", "--ctrack-timeouts", "--ipcache-lifetime", "--lua-gc",
			"--filter-l3", "--filter-tcp", "--filter-udp", "--filter-l7",
			"--hostlist", "--hostlist-exclude", "--hostlist-domains", "--hostlist-exclude-domains",
			"--ipset", "--ipset-exclude", "--ipset-ip", "--ipset-exclude-ip",
			"--payload", "--in-range", "--out-range", "--blob", "--lua-init", "--lua-desync":
			if !hasValue || value == "" {
				return nil, fmt.Errorf("zapret2 option %d: %s needs a value", i+1, key)
			}
		default:
			return nil, fmt.Errorf("zapret2 option %d: unsupported strategy option %q", i+1, key)
		}
		hasStrategy = hasStrategy || key == "--lua-desync"
		args = append(args, arg)
	}
	if !hasStrategy {
		return nil, errors.New("zapret2 needs at least one --lua-desync")
	}
	return args, nil
}
