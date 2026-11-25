package companion

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// maxTerminals bounds how many server terminals one Companion keeps open.
const maxTerminals = 4

// terminalMaxMessage bounds one message from the panel: keystrokes and resize
// requests are small.
const terminalMaxMessage = 64 << 10

// terminalStopGrace bounds how long Close waits for terminals to end their ssh.
const terminalStopGrace = 3 * time.Second

// errTerminalUnsupported means this platform cannot run the server terminal.
var errTerminalUnsupported = errors.New("the server terminal is not available on this platform")

// terminalMessage is a text frame from the panel: {"type":"resize","cols":80,"rows":24}.
// Keyboard input travels as binary frames and is written to the terminal as is.
type terminalMessage struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// terminalRegistry counts the open terminals and cancels them when the Companion stops.
type terminalRegistry struct {
	mu      sync.Mutex
	stopped bool
	next    int
	cancels map[int]context.CancelFunc
	wg      sync.WaitGroup
}

// begin reserves a terminal slot. It fails when the limit is reached or the
// Companion is stopping.
func (r *terminalRegistry) begin(cancel context.CancelFunc) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped || len(r.cancels) >= maxTerminals {
		return 0, false
	}
	if r.cancels == nil {
		r.cancels = map[int]context.CancelFunc{}
	}
	r.next++
	r.cancels[r.next] = cancel
	r.wg.Add(1)
	return r.next, true
}

func (r *terminalRegistry) end(id int) {
	r.mu.Lock()
	delete(r.cancels, id)
	r.mu.Unlock()
	r.wg.Done()
}

// stop cancels every open terminal and waits, for a short grace, until each has
// killed its ssh process.
func (r *terminalRegistry) stop() {
	r.mu.Lock()
	r.stopped = true
	for _, cancel := range r.cancels {
		cancel()
	}
	r.mu.Unlock()
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(terminalStopGrace):
	}
}

// terminalOriginAllowed accepts only the panel's own page: a browser sends the
// Origin of the page that opened the socket, so a foreign site is refused here.
func terminalOriginAllowed(r *http.Request, address string) bool {
	origin := r.Header.Get("Origin")
	return origin != "" && origin == "http://"+address
}

// terminalAPI upgrades the request to a WebSocket that is attached to a shell on
// the server. The command is built by ControlOptions.Terminal (ssh in production).
func (c *ControlServer) terminalAPI(w http.ResponseWriter, r *http.Request, options ControlOptions) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// A plain GET asks what the panel may offer; it opens nothing, so it needs no Origin.
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		writeJSON(w, map[string]any{"available": options.Terminal != nil && terminalSupported(), "max": maxTerminals})
		return
	}
	if !terminalOriginAllowed(r, c.State.Address) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	cols, rows := terminalSize(r)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	id, ok := c.terminals.begin(cancel)
	if !ok {
		http.Error(w, "too many terminals are open", http.StatusTooManyRequests)
		return
	}
	defer c.terminals.end(id)

	if options.Terminal == nil {
		http.Error(w, "the server terminal is not available", http.StatusNotImplemented)
		return
	}
	command, err := options.Terminal()
	if err != nil {
		if errors.Is(err, errTerminalUnsupported) {
			http.Error(w, err.Error(), http.StatusNotImplemented)
			return
		}
		http.Error(w, "the server terminal could not start", http.StatusConflict)
		return
	}
	term, err := startTerminal(command, cols, rows)
	if err != nil {
		if errors.Is(err, errTerminalUnsupported) {
			http.Error(w, err.Error(), http.StatusNotImplemented)
			return
		}
		http.Error(w, "the server terminal could not start", http.StatusInternalServerError)
		return
	}
	// The server's read and write timeouts would cut a long session: clear them
	// before the connection is upgraded.
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Time{})
	_ = controller.SetWriteDeadline(time.Time{})
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		term.stop()
		return
	}
	conn.SetReadLimit(terminalMaxMessage)
	runTerminal(ctx, cancel, conn, term)
}

// terminalSize reads the requested size, with a safe default.
func terminalSize(r *http.Request) (int, int) {
	cols, rows := 80, 24
	if v, err := strconv.Atoi(r.URL.Query().Get("cols")); err == nil && v >= 2 && v <= 500 {
		cols = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("rows")); err == nil && v >= 1 && v <= 200 {
		rows = v
	}
	return cols, rows
}

// runTerminal pumps bytes both ways until either side ends, then kills the process.
func runTerminal(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, term terminal) {
	defer term.stop()
	output := make(chan struct{})
	go func() {
		defer close(output)
		buf := make([]byte, 4096)
		for {
			n, err := term.Read(buf)
			if n > 0 {
				if conn.Write(ctx, websocket.MessageBinary, buf[:n]) != nil {
					cancel()
					return
				}
			}
			if err != nil {
				// The shell ended: tell the panel and close the socket.
				_ = conn.Close(websocket.StatusNormalClosure, "shell ended")
				cancel()
				return
			}
		}
	}()
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		switch kind {
		case websocket.MessageBinary:
			if _, err := term.Write(data); err != nil {
				cancel()
			}
		case websocket.MessageText:
			var message terminalMessage
			if json.Unmarshal(data, &message) == nil && message.Type == "resize" && message.Cols >= 2 && message.Rows >= 1 {
				_ = term.Resize(message.Cols, message.Rows)
			}
		}
	}
	cancel()
	conn.CloseNow()
	// Stopping the process closes the terminal, which unblocks the output reader.
	term.stop()
	<-output
}

// terminal is a command attached to a pseudo-terminal. Read returns the output;
// stop kills the process and releases the terminal.
type terminal interface {
	io.ReadWriter
	Resize(cols, rows int) error
	stop()
}

// ServerTerminalCommand is the ssh command of the server terminal for profile.
// It reuses the tunnel's ControlMaster socket, so no second login is made where
// unix sockets exist; elsewhere it is a plain ssh session.
func ServerTerminalCommand(profile string) (*exec.Cmd, error) {
	saved, err := LoadProfile(profile)
	if err != nil {
		return nil, err
	}
	if err := saved.Validate(); err != nil {
		return nil, err
	}
	arguments := []string{"-tt"}
	if saved.SSHIdentityFile != "" {
		arguments = append(arguments, "-i", saved.SSHIdentityFile, "-o", "IdentitiesOnly=yes")
	}
	if controlMasterSupported {
		controlPath, err := ControlPath(profile)
		if err != nil {
			return nil, err
		}
		arguments = append(arguments, "-S", controlPath, "-o", "ControlMaster=no")
	}
	arguments = append(arguments, saved.Destination)
	return exec.Command("ssh", arguments...), nil
}
