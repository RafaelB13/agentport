package hostport

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
)

// lsofRecord is one process from `lsof -F pcuL` output.
type lsofRecord struct {
	PID     int
	Command string
	UID     string
	Login   string
}

// parseLsof parses `lsof -F pcuL` output. Each process starts with a `p` line;
// `c`, `u` and `L` lines carry its command, numeric uid and login. A process
// that appears again (one block per socket) is reported once, with its first
// block's fields. Unknown field letters are ignored.
func parseLsof(out []byte) ([]lsofRecord, error) {
	var recs []lsofRecord
	seen := map[int]bool{}
	cur := -1 // index into recs of the process being read; -1 skips its fields
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		val := line[1:]
		switch line[0] {
		case 'p':
			pid, err := strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("parse lsof pid %q: %w", val, err)
			}
			if seen[pid] {
				cur = -1
				continue
			}
			seen[pid] = true
			recs = append(recs, lsofRecord{PID: pid})
			cur = len(recs) - 1
		case 'c':
			if cur >= 0 {
				recs[cur].Command = val
			}
		case 'u':
			if cur >= 0 {
				recs[cur].UID = val
			}
		case 'L':
			if cur >= 0 {
				recs[cur].Login = val
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read lsof output: %w", err)
	}
	return recs, nil
}
