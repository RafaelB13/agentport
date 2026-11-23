package ports

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/rafaelb13/agentport/internal/hostport"
	"github.com/rafaelb13/agentport/internal/portsproto"
)

// SessionHeader carries the agent session ID on remote requests (see portsproto).
const SessionHeader = portsproto.SessionHeader

// approvalWait is how long a forward request may wait for the user. The bridge
// and the control server extend their write deadlines to cover it.
const approvalWait = 120 * time.Second

// writeDeadline leaves room for the approval wait plus the response.
const writeDeadline = approvalWait + 30*time.Second

const maxBody = 64 * 1024

// NewHandler serves the port protocol under /v1/ports/. The Companion mounts it
// behind its control token, so every route here is reached only by the bridge
// (remote protocol) or by the local panel and CLI (local API).
func NewHandler(store *Store) http.Handler {
	h := &handler{store: store}
	mux := http.NewServeMux()
	// Remote protocol, reached through the bridge.
	mux.HandleFunc("POST /v1/ports/sessions", h.register)
	mux.HandleFunc("POST /v1/ports/sessions/{id}/heartbeat", h.heartbeat)
	mux.HandleFunc("DELETE /v1/ports/sessions/{id}", h.unregister)
	mux.HandleFunc("POST /v1/ports/forwards", h.forward)
	mux.HandleFunc("GET /v1/ports/forwards", h.listForwards)
	mux.HandleFunc("POST /v1/ports/forwards/unforward", h.unforward)
	// Local API, used by the panel and the CLI.
	mux.HandleFunc("GET /v1/ports/state", h.state)
	mux.HandleFunc("GET /v1/ports/requests", h.requests)
	mux.HandleFunc("POST /v1/ports/requests/{id}/approve", h.approve)
	mux.HandleFunc("POST /v1/ports/requests/{id}/deny", h.deny)
	mux.HandleFunc("POST /v1/ports/forwards/open", h.open)
	mux.HandleFunc("POST /v1/ports/forwards/close", h.closeForwards)
	mux.HandleFunc("GET /v1/ports/events", h.events)
	mux.HandleFunc("POST /v1/ports/forwards/close-all", h.closeAll)
	mux.HandleFunc("POST /v1/ports/forwards/stop-owner", h.stopOwner)
	mux.HandleFunc("POST /v1/ports/forwards/{remote}/edit", h.editForward)
	mux.HandleFunc("POST /v1/ports/forwards/{remote}/rename", h.renameForward)
	mux.HandleFunc("POST /v1/ports/forwards/{remote}/pin", h.pinForward)
	mux.HandleFunc("POST /v1/ports/closed/reopen", h.reopenClosed)
	mux.HandleFunc("POST /v1/ports/closed/forget", h.forgetClosed)
	mux.HandleFunc("GET /v1/ports/local/check", h.checkLocal)
	return mux
}

type handler struct{ store *Store }

func (h *handler) register(w http.ResponseWriter, r *http.Request) {
	var req portsproto.RegisterSessionRequest
	if !decode(w, r, &req) {
		return
	}
	resp, err := h.store.RegisterSession(req)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) heartbeat(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Heartbeat(r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) unregister(w http.ResponseWriter, r *http.Request) {
	h.store.UnregisterSession(r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *handler) forward(w http.ResponseWriter, r *http.Request) {
	session, ok := sessionFrom(w, r)
	if !ok {
		return
	}
	var req portsproto.ForwardRequest
	if !decode(w, r, &req) {
		return
	}
	extendWriteDeadline(w)
	ctx, cancel := context.WithTimeout(r.Context(), approvalWait)
	defer cancel()
	resp, err := h.store.Request(ctx, session, req)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) listForwards(w http.ResponseWriter, r *http.Request) {
	session, ok := sessionFrom(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, portsproto.ForwardsResponse{Forwards: h.store.Forwards(session)})
}

func (h *handler) unforward(w http.ResponseWriter, r *http.Request) {
	session, ok := sessionFrom(w, r)
	if !ok {
		return
	}
	var req portsproto.UnforwardRequest
	if !decode(w, r, &req) {
		return
	}
	if err := req.Validate(); err != nil {
		fail(w, &InvalidError{err})
		return
	}
	resp, err := h.store.Unforward(session, req.Ports, req.All)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *handler) state(w http.ResponseWriter, _ *http.Request) {
	closed := h.store.Closed()
	if len(closed) > 50 {
		closed = closed[len(closed)-50:]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":  h.store.Version(),
		"pending":  h.store.Pending(),
		"forwards": h.store.Forwards(""),
		"closed":   closed,
		"activity": h.store.Activity(),
	})
}

func (h *handler) requests(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"requests": h.store.Pending()})
}

// approveBody is the panel's decision. Duration is session, 1h or pin.
type approveBody struct {
	Duration string `json:"duration"`
	Ports    []struct {
		RemotePort int  `json:"remote_port"`
		LocalPort  int  `json:"local_port,omitempty"`
		Skip       bool `json:"skip,omitempty"`
	} `json:"ports"`
}

func (h *handler) approve(w http.ResponseWriter, r *http.Request) {
	var body approveBody
	if !decode(w, r, &body) {
		return
	}
	approval := Approval{Duration: body.Duration}
	for _, p := range body.Ports {
		if !portsproto.ValidPort(p.RemotePort) || (p.LocalPort != 0 && !portsproto.ValidPort(p.LocalPort)) {
			fail(w, &InvalidError{errors.New("ports must be between 1 and 65535")})
			return
		}
		approval.Ports = append(approval.Ports, PortDecision{RemotePort: p.RemotePort, LocalPort: p.LocalPort, Skip: p.Skip})
	}
	results, err := h.store.Approve(r.Context(), r.PathValue("id"), approval)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, portsproto.ForwardResponse{Results: results})
}

func (h *handler) deny(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Deny(r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"denied": true})
}

type openBody struct {
	RemotePort int `json:"remote_port"`
	LocalPort  int `json:"local_port,omitempty"`
}

func (h *handler) open(w http.ResponseWriter, r *http.Request) {
	var body openBody
	if !decode(w, r, &body) {
		return
	}
	if !portsproto.ValidPort(body.RemotePort) || (body.LocalPort != 0 && !portsproto.ValidPort(body.LocalPort)) {
		fail(w, &InvalidError{errors.New("ports must be between 1 and 65535")})
		return
	}
	info, err := h.store.OpenByUser(r.Context(), body.RemotePort, body.LocalPort)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

type closeBody struct {
	RemotePorts []int `json:"remote_ports"`
}

func (h *handler) closeForwards(w http.ResponseWriter, r *http.Request) {
	var body closeBody
	if !decode(w, r, &body) {
		return
	}
	closed, notOpen := []int{}, []int{}
	for _, p := range body.RemotePorts {
		if err := h.store.CloseByUser(p); err != nil {
			if errors.Is(err, ErrNotFound) {
				notOpen = append(notOpen, p)
				continue
			}
			fail(w, err)
			return
		}
		closed = append(closed, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"closed": closed, "not_open": notOpen})
}

// sessionFrom reads the session header, answering 400 when it is missing.
func sessionFrom(w http.ResponseWriter, r *http.Request) (string, bool) {
	session := r.Header.Get(SessionHeader)
	if session == "" {
		writeError(w, http.StatusBadRequest, portsproto.CodeInvalidRequest, SessionHeader+" header is required")
		return "", false
	}
	return session, true
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	if err := decoder.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, portsproto.CodeInvalidRequest, "invalid json")
		return false
	}
	return true
}

// fail maps a Store error to the HTTP status and code the clients expect.
func fail(w http.ResponseWriter, err error) {
	var invalid *InvalidError
	switch {
	case errors.As(err, &invalid):
		writeError(w, http.StatusBadRequest, portsproto.CodeInvalidRequest, invalid.Error())
	case errors.Is(err, ErrUnknownSession):
		writeError(w, http.StatusNotFound, portsproto.CodeUnknownSession, err.Error())
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, portsproto.CodeNotFound, err.Error())
	case errors.Is(err, ErrNotPending), errors.Is(err, ErrAlreadyOpen), errors.Is(err, hostport.ErrNotStoppable), errors.Is(err, hostport.ErrStillBusy):
		writeError(w, http.StatusConflict, portsproto.CodeConflict, err.Error())
	case errors.Is(err, ErrLimitReached):
		writeError(w, http.StatusConflict, portsproto.CodeLimitReached, err.Error())
	case errors.Is(err, ErrInvalidDuration), errors.Is(err, ErrBlockedPort):
		writeError(w, http.StatusBadRequest, portsproto.CodeInvalidRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, portsproto.CodeInternal, "port operation failed")
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, portsproto.ErrorBody{Code: code, Message: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// extendWriteDeadline lets a long-poll answer after the server's default write
// timeout. Servers that do not support it keep their default.
func extendWriteDeadline(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(writeDeadline))
}
