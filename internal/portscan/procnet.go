package portscan

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

// tcpStateListen is the hex state code for LISTEN in /proc/net/tcp{,6}.
const tcpStateListen = "0A"

// netEntry is one LISTEN row of /proc/net/tcp or /proc/net/tcp6.
type netEntry struct {
	Bind  string // textual IP address
	Port  int
	UID   uint32 // owner of the socket
	Inode uint64 // socket inode, matched against /proc/<pid>/fd links
}

// parseProcNetTCP reads a /proc/net/tcp (v6 == false) or /proc/net/tcp6
// (v6 == true) table and returns its LISTEN rows. Other states are ignored, and
// so are malformed rows, including the header line.
//
// Format (one row per socket, fields separated by whitespace):
//
//	sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode ...
//
// local_address is "HEXADDR:HEXPORT". The address is a sequence of 32-bit words
// printed in host (little-endian) byte order; the port is big-endian.
func parseProcNetTCP(r io.Reader, v6 bool) ([]netEntry, error) {
	var out []netEntry
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 10 || f[3] != tcpStateListen {
			continue
		}
		local := f[1]
		colon := strings.LastIndexByte(local, ':')
		if colon < 0 {
			continue
		}
		bind, err := decodeAddr(local[:colon], v6)
		if err != nil {
			continue
		}
		port, err := strconv.ParseUint(local[colon+1:], 16, 16)
		if err != nil {
			continue
		}
		uid, err := strconv.ParseUint(f[7], 10, 32)
		if err != nil {
			continue
		}
		inode, err := strconv.ParseUint(f[9], 10, 64)
		if err != nil {
			continue
		}
		out = append(out, netEntry{Bind: bind, Port: int(port), UID: uint32(uid), Inode: inode})
	}
	return out, sc.Err()
}

// decodeAddr turns the hex address of a /proc/net/tcp row into text.
// IPv4 is one little-endian word (8 hex digits). IPv6 is four little-endian
// words (32 hex digits), so each group of four bytes is reversed.
func decodeAddr(s string, v6 bool) (string, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("portscan: address %q: %w", s, err)
	}
	if !v6 {
		if len(b) != net.IPv4len {
			return "", fmt.Errorf("portscan: IPv4 address %q has %d bytes", s, len(b))
		}
		return net.IPv4(b[3], b[2], b[1], b[0]).String(), nil
	}
	if len(b) != net.IPv6len {
		return "", fmt.Errorf("portscan: IPv6 address %q has %d bytes", s, len(b))
	}
	ip := make(net.IP, net.IPv6len)
	for w := 0; w < 4; w++ {
		for i := 0; i < 4; i++ {
			ip[w*4+i] = b[w*4+3-i]
		}
	}
	return ip.String(), nil
}
