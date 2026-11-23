// Package portscan lists the TCP ports that are listening on the machine the
// MCP server runs on, together with the process that owns each one. It is the
// data source behind an agent-driven "Ports" view, similar to the one in VS
// Code.
//
// Scan reads the kernel's socket tables: /proc/net/tcp and /proc/net/tcp6 on
// Linux (owners are resolved by walking /proc/<pid>/fd), and lsof on macOS.
// Other platforms return ErrUnsupported. The parsers are pure functions and
// the procfs root is a parameter, so the logic is tested with fixtures under
// testdata/ on every platform.
//
// Results are deduplicated by port (an IPv4 and an IPv6 socket on the same
// port become one entry, preferring the more specific bind address), filtered
// by Options, and sorted by port. Processes the caller cannot inspect keep
// PID 0 and an empty Process instead of producing an error.
//
// The package is self-contained: it imports nothing from the rest of the
// repository.
package portscan
