package portscan

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParseProcNetTCP(t *testing.T) {
	tests := []struct {
		name string
		file string
		v6   bool
		want []netEntry
	}{
		{
			name: "ipv4 keeps only LISTEN rows",
			file: "testdata/proc/net/tcp",
			want: []netEntry{
				{Bind: "127.0.0.1", Port: 3000, UID: 1000, Inode: 1001},
				{Bind: "0.0.0.0", Port: 5173, UID: 1000, Inode: 1002},
			},
		},
		{
			name: "ipv6 decodes word order and keeps only LISTEN rows",
			file: "testdata/proc/net/tcp6",
			v6:   true,
			want: []netEntry{
				{Bind: "::", Port: 8080, UID: 1000, Inode: 2001},
				{Bind: "::1", Port: 9000, UID: 0, Inode: 2002},
				{Bind: "::", Port: 5173, UID: 1000, Inode: 2003},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := os.Open(tt.file)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			got, err := parseProcNetTCP(f, tt.v6)
			if err != nil {
				t.Fatalf("parseProcNetTCP: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestParseProcNetTCPSkipsMalformedRows(t *testing.T) {
	input := strings.Join([]string{
		"  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode",
		"   0: ZZZZ:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 1001 1",
		"   1: 0100007F:0BB8 00000000:0000 0A",
		"   2: 0100007F:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 1001 1",
	}, "\n")
	got, err := parseProcNetTCP(strings.NewReader(input), false)
	if err != nil {
		t.Fatal(err)
	}
	want := []netEntry{{Bind: "127.0.0.1", Port: 3000, UID: 1000, Inode: 1001}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestDecodeAddr(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		v6      bool
		want    string
		wantErr bool
	}{
		{name: "ipv4 loopback", addr: "0100007F", want: "127.0.0.1"},
		{name: "ipv4 any", addr: "00000000", want: "0.0.0.0"},
		{name: "ipv4 wrong length", addr: "0100", wantErr: true},
		{name: "ipv6 any", addr: "00000000000000000000000000000000", v6: true, want: "::"},
		{name: "ipv6 loopback", addr: "00000000000000000000000001000000", v6: true, want: "::1"},
		{name: "ipv6 mapped ipv4", addr: "0000000000000000FFFF00000100007F", v6: true, want: "127.0.0.1"},
		{name: "ipv6 wrong length", addr: "0100007F", v6: true, wantErr: true},
		{name: "not hex", addr: "XYZ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeAddr(tt.addr, tt.v6)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
