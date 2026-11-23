package hostport

import (
	"slices"
	"testing"
)

func TestParseLsofFixtures(t *testing.T) {
	tests := []struct {
		file string
		want []lsofRecord
	}{
		{"lsof_proc.txt", []lsofRecord{{PID: 4242, Command: "node", UID: "501", Login: "rafael"}}},
		{"lsof_root.txt", []lsofRecord{{PID: 311, Command: "postgres", UID: "0", Login: "root"}}},
		// Same pid twice (one block per socket) is reported once.
		{"lsof_dup.txt", []lsofRecord{
			{PID: 4242, Command: "node", UID: "501", Login: "rafael"},
			{PID: 5150, Command: "docker-proxy", UID: "501", Login: "rafael"},
		}},
		{"lsof_empty.txt", nil},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			got, err := parseLsof([]byte(fixture(t, tc.file)))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("parseLsof = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseLsofRejectsBadPid(t *testing.T) {
	if _, err := parseLsof([]byte("pnotanumber\ncnode\n")); err == nil {
		t.Fatal("want error for non-numeric pid")
	}
}

func TestParseSystemctlUnit(t *testing.T) {
	if got := parseSystemctlUnit([]byte(fixture(t, "systemctl_service.txt"))); got != "app.service" {
		t.Errorf("service fixture: unit = %q, want app.service", got)
	}
	if got := parseSystemctlUnit([]byte(fixture(t, "systemctl_none.txt"))); got != "" {
		t.Errorf("no-unit fixture: unit = %q, want empty", got)
	}
}

func TestParseLaunchctlPidFixture(t *testing.T) {
	if got := pidLine.FindSubmatch([]byte(fixture(t, "launchctl_print.txt"))); got == nil || string(got[1]) != "4242" {
		t.Fatalf("pid match = %q, want 4242", got)
	}
}
