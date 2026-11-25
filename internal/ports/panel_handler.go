package ports

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/rafaelb13/agentport/internal/portsproto"
)

// eventInterval is how often the events stream checks the state version.
const eventInterval = time.Second

// keepAliveInterval keeps idle event streams open through proxies and timeouts.
const keepAliveInterval = 15 * time.Second

// events streams a "state" event whenever the state the panel shows changes.
// The event carries the new version only: the panel then reads the full state.
// It is a long-lived response, so it lifts the server's write deadline.
func (h *handler) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, portsproto.CodeInternal, "streaming unsupported")
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ticker := time.NewTicker(eventInterval)
	defer ticker.Stop()
	keepAlive := time.NewTicker(keepAliveInterval)
	defer keepAlive.Stop()
	last := ^uint64(0)
	send := func() {
		v := h.store.Version()
		if v == last {
			return
		}
		last = v
		_, _ = w.Write([]byte("event: state\ndata: {\"version\":" + strconv.FormatUint(v, 10) + "}\n\n"))
		flusher.Flush()
	}
	send()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			send()
		case <-keepAlive.C:
			_, _ = w.Write([]byte(": keep-alive\n\n"))
			flusher.Flush()
		}
	}
}

// remoteParam reads the {remote} path value, answering 400 when it is not a valid port.
func remoteParam(w http.ResponseWriter, r *http.Request) (int, bool) {
	port, err := strconv.Atoi(r.PathValue("remote"))
	if err != nil || !portsproto.ValidPort(port) {
		fail(w, &InvalidError{errors.New("ports must be between 1 and 65535")})
		return 0, false
	}
	return port, true
}

func validLocal(port int) error {
	if port != 0 && !portsproto.ValidPort(port) {
		return &InvalidError{errors.New("ports must be between 1 and 65535")}
	}
	return nil
}

type editBody struct {
	Label     *string `json:"label,omitempty"`
	Path      *string `json:"path,omitempty"`
	LocalPort int     `json:"local_port,omitempty"`
	Duration  string  `json:"duration,omitempty"`
}

func (h *handler) editForward(w http.ResponseWriter, r *http.Request) {
	remote, ok := remoteParam(w, r)
	if !ok {
		return
	}
	var body editBody
	if !decode(w, r, &body) {
		return
	}
	if err := validLocal(body.LocalPort); err != nil {
		fail(w, err)
		return
	}
	info, err := h.store.EditForward(r.Context(), remote, ForwardEdit(body))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

type renameBody struct {
	Label string `json:"label"`
}

func (h *handler) renameForward(w http.ResponseWriter, r *http.Request) {
	remote, ok := remoteParam(w, r)
	if !ok {
		return
	}
	var body renameBody
	if !decode(w, r, &body) {
		return
	}
	info, err := h.store.EditForward(r.Context(), remote, ForwardEdit{Label: &body.Label})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

type pinBody struct {
	Pinned bool `json:"pinned"`
}

func (h *handler) pinForward(w http.ResponseWriter, r *http.Request) {
	remote, ok := remoteParam(w, r)
	if !ok {
		return
	}
	var body pinBody
	if !decode(w, r, &body) {
		return
	}
	info, err := h.store.SetPinned(remote, body.Pinned)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (h *handler) closeAll(w http.ResponseWriter, _ *http.Request) {
	closed, err := h.store.CloseAll()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"closed": closed})
}

type stopOwnerBody struct {
	RemotePort int `json:"remote_port"`
	LocalPort  int `json:"local_port"`
}

// stopOwner stops the program that holds local_port and opens remote_port on it
// (or on the next free port, which the response reports).
func (h *handler) stopOwner(w http.ResponseWriter, r *http.Request) {
	var body stopOwnerBody
	if !decode(w, r, &body) {
		return
	}
	if !portsproto.ValidPort(body.RemotePort) || !portsproto.ValidPort(body.LocalPort) {
		fail(w, &InvalidError{errors.New("ports must be between 1 and 65535")})
		return
	}
	info, err := h.store.StopOwnerAndOpen(r.Context(), body.RemotePort, body.LocalPort)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

type reopenBody struct {
	RemotePort int       `json:"remote_port"`
	LocalPort  int       `json:"local_port,omitempty"`
	Label      string    `json:"label,omitempty"`
	Path       string    `json:"path,omitempty"`
	Duration   string    `json:"duration,omitempty"`
	ClosedAt   time.Time `json:"closed_at,omitempty"`
}

func (h *handler) reopenClosed(w http.ResponseWriter, r *http.Request) {
	var body reopenBody
	if !decode(w, r, &body) {
		return
	}
	if len(body.Label) > portsproto.MaxLabelLength || len(body.Path) > portsproto.MaxPathLength {
		fail(w, &InvalidError{errors.New("label or path is too long")})
		return
	}
	info, err := h.store.ReopenClosed(r.Context(), ReopenRequest(body))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

type forgetBody struct {
	RemotePort int       `json:"remote_port"`
	ClosedAt   time.Time `json:"closed_at,omitempty"`
}

func (h *handler) forgetClosed(w http.ResponseWriter, r *http.Request) {
	var body forgetBody
	if !decode(w, r, &body) {
		return
	}
	if !portsproto.ValidPort(body.RemotePort) {
		fail(w, &InvalidError{errors.New("ports must be between 1 and 65535")})
		return
	}
	if err := h.store.ForgetClosed(body.RemotePort, body.ClosedAt); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"forgotten": true})
}

// checkLocal answers GET /v1/ports/local/check?port=N for the panel's port modals.
func (h *handler) checkLocal(w http.ResponseWriter, r *http.Request) {
	port, err := strconv.Atoi(r.URL.Query().Get("port"))
	if err != nil {
		fail(w, &InvalidError{errors.New("port must be a number")})
		return
	}
	check, err := h.store.CheckLocal(r.Context(), port)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, check)
}
