// Package paths decides where agentport keeps its cache, runtime state, logs
// and ControlMaster sockets.
//
// The cache base directory is chosen in this order:
//  1. AGENTPORT_CACHE_DIR, an absolute path (it plays the role of XDG_CACHE_HOME);
//  2. XDG_CACHE_HOME, when it is set to an absolute path, on every operating system;
//  3. the platform cache directory (os.UserCacheDir).
//
// agentport writes everything under <base>/agentport.
package paths

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// MaxSocketPath is a conservative limit for a unix socket path (104 on macOS,
// 108 on Linux).
const MaxSocketPath = 100

// CacheBase returns the directory that contains the agentport folder.
func CacheBase() (string, error) {
	if directory := strings.TrimSpace(os.Getenv("AGENTPORT_CACHE_DIR")); directory != "" {
		if !filepath.IsAbs(directory) {
			return "", errors.New("AGENTPORT_CACHE_DIR must be an absolute path")
		}
		return directory, nil
	}
	if directory := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); directory != "" {
		if filepath.IsAbs(directory) {
			return directory, nil
		}
		// A relative XDG_CACHE_HOME is ignored. On Linux and other XDG systems
		// os.UserCacheDir rejects it with an error, so use ~/.cache directly.
		if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("find agentport cache directory: %w", err)
			}
			return filepath.Join(home, ".cache"), nil
		}
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("find agentport cache directory: %w", err)
	}
	return base, nil
}

// Dir returns <cache base>/agentport, or the named subdirectory of it when
// names are given. It does not create anything.
func Dir(names ...string) (string, error) {
	base, err := CacheBase()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{base, "agentport"}, names...)...), nil
}

// ControlSocket returns the path of a ControlMaster socket named name. The
// socket lives in a private <cache>/agentport/cm directory. When that path is too
// long for a unix socket, the socket goes to a short hashed directory under the
// system temporary location instead (/tmp/agentport-<uid> on unix).
func ControlSocket(name string) (string, error) {
	sum := sha256.Sum256([]byte(name))
	socket := hex.EncodeToString(sum[:6]) + ".sock"
	dir, err := Dir("cm")
	if err != nil {
		return "", err
	}
	if len(filepath.Join(dir, socket)) > MaxSocketPath {
		dir = shortSocketDir()
	}
	if err := ensurePrivateDir(dir); err != nil {
		return "", err
	}
	return filepath.Join(dir, socket), nil
}

// shortSocketDir is the fallback socket directory. On unix it is fixed to /tmp:
// os.TempDir can itself be a long path on macOS.
func shortSocketDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.TempDir(), "agentport-cm")
	}
	return filepath.Join(fallbackSocketRoot, fmt.Sprintf("agentport-%d", os.Getuid()))
}

// fallbackSocketRoot is /tmp; tests replace it with a temporary directory.
var fallbackSocketRoot = "/tmp"

func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create agentport directory: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return fmt.Errorf("secure agentport directory: %w", err)
	}
	return nil
}
