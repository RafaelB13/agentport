//go:build e2e

package e2e

import (
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rafaelb13/agentport/internal/forward"
)

// TestProductFlow dirige o produto real de ponta a ponta: Companion, bridge,
// `agentport mcp` dentro do container, aprovação pelo CLI do host e forwards.
// Os passos seguem a fase C: list, forward com conflito, aprovação, segunda
// sessão, unforward compartilhado, deny e restart do Companion.
func TestProductFlow(t *testing.T) {
	p := startProduct(t)

	// Passo 2: túnel e ControlMaster de pé.
	waitFor(t, 60*time.Second, "túnel do Companion conectado", p.tunnelConnected)
	t.Log("passo 2: Companion conectado")

	s1 := p.openMCP()
	s1.waitBridgeOnline(90 * time.Second)
	t.Run("list de ferramentas e ports_list", func(t *testing.T) {
		got := strings.Join(s1.tools(), ",")
		for _, want := range []string{"ports_list", "ports_forward", "ports_unforward", "forwards_list"} {
			if !strings.Contains(got, want) {
				t.Errorf("tools/list sem %s: %s", want, got)
			}
		}
		text, err := s1.call("ports_list", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("ports_list: %s", text)
		for _, port := range []int{3000, 5173, 8080} {
			if !strings.Contains(text, `"port":`+strconv.Itoa(port)+",") {
				t.Errorf("ports_list sem a porta %d", port)
			}
		}
		if strings.Contains(text, `"forwarded"`) {
			t.Errorf("ports_list já mostra forwarded antes de qualquer forward: %s", text)
		}
	})

	// Passo 5 (conflito): ocupa 5173 no host ANTES do forward.
	// Se outro processo do host já ocupa 5173 (um dev server, por exemplo), o
	// conflito já existe e este bind é desnecessário: a asserção abaixo vale igual.
	busy, err := net.Listen("tcp", "127.0.0.1:5173")
	if err != nil {
		t.Logf("5173 já ocupada no host (%v): o conflito já existe", err)
		busy = nil
	} else {
		defer busy.Close()
	}

	// A porta local do forward não é fixa: o host pode ter qualquer coisa em 3000
	// (ex.: um dev server em *:3000). Registra antes do forward se 3000 já está
	// tomada, e a asserção abaixo confere a porta que o forward devolveu.
	taken3000 := !forward.CanListen(3000)

	var api, web forwardResult
	t.Run("forward com aprovação e conflito", func(t *testing.T) {
		pending := s1.callAsync("ports_forward", map[string]any{
			"reason": "e2e", "group": "e2e",
			"ports": []map[string]any{
				{"remote_port": 3000, "label": "api", "path": "/"},
				{"remote_port": 5173},
			},
		})
		var id string
		waitFor(t, 30*time.Second, "pedido pendente em `agentport requests`", func() bool {
			id = p.pendingRequestID()
			return id != ""
		})
		t.Logf("passo 4: requests -> %s", id)
		if out, err := p.ag("approve", id, "--all"); err != nil {
			t.Fatalf("approve: %v\n%s", err, out)
		} else {
			t.Logf("approve --all:\n%s", out)
		}
		r := <-pending
		if r.err != nil {
			t.Fatalf("ports_forward: %v", r.err)
		}
		t.Logf("ports_forward: %s", r.text)
		results := parseForwardResults(t, r.text)
		if len(results) != 2 {
			t.Fatalf("ports_forward devolveu %d resultados; quero 2", len(results))
		}
		for _, x := range results {
			if x.State != "active" {
				t.Fatalf("porta %d: state %q; quero active (%s)", x.RemotePort, x.State, x.Message)
			}
			switch x.RemotePort {
			case 3000:
				api = x
			case 5173:
				web = x
			}
		}
		if api.LocalPort == 0 {
			t.Errorf("api local = 0; o forward de 3000 não devolveu porta local")
		}
		if taken3000 && api.LocalPort == 3000 {
			t.Errorf("api local = 3000, mas 3000 já estava ocupada no host; o forward tinha que pular")
		}
		t.Logf("forward 3000 -> local %d", api.LocalPort)
		if web.LocalPort == 5173 || web.LocalPort == 0 {
			t.Errorf("web local = %d; a porta 5173 estava ocupada e não pode ser usada", web.LocalPort)
		}
		if !strings.Contains(web.URL, ":"+strconv.Itoa(web.LocalPort)) {
			t.Errorf("URL da web = %q; não reflete a porta local %d", web.URL, web.LocalPort)
		}
	})

	t.Run("URLs respondem no host, só em 127.0.0.1", func(t *testing.T) {
		if api.URL == "" || web.URL == "" {
			t.Skip("forward não ficou ativo")
		}
		expectURLBody(t, api.URL, "api-3000")
		expectURLBody(t, web.URL, "web-5173")
		for _, port := range []int{api.LocalPort, web.LocalPort} {
			names, err := listenerNames(port)
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range names {
				if n != "127.0.0.1:"+strconv.Itoa(port) {
					t.Errorf("porta %d escuta em %q; quero só 127.0.0.1", port, n)
				}
			}
		}
	})

	t.Run("agentport forwards bate com o resultado", func(t *testing.T) {
		out, err := p.ag("forwards")
		if err != nil {
			t.Fatalf("forwards: %v\n%s", err, out)
		}
		t.Logf("forwards:\n%s", out)
		for _, x := range []forwardResult{api, web} {
			if !hasForwardRow(out, x.RemotePort, x.LocalPort) {
				t.Errorf("forwards não lista %d->%d", x.RemotePort, x.LocalPort)
			}
		}
	})

	// Passo 6: segunda sessão pede 3000 e recebe a mesma URL, sem novo pedido.
	s2 := p.openMCP()
	t.Run("segunda sessão recebe a mesma URL sem pedido novo", func(t *testing.T) {
		text, err := s2.call("ports_forward", map[string]any{
			"reason": "e2e session 2", "ports": []map[string]any{{"remote_port": 3000}},
		})
		if err != nil {
			t.Fatalf("ports_forward s2: %v", err)
		}
		results := parseForwardResults(t, text)
		if len(results) != 1 || results[0].State != "active" || results[0].URL != api.URL {
			t.Errorf("s2 devolveu %+v; quero active com URL %s", results, api.URL)
		}
		if id := p.pendingRequestID(); id != "" {
			t.Errorf("sessão 2 abriu um pedido novo: %s", id)
		}
	})

	t.Run("unforward da sessão 1 mantém a porta da sessão 2", func(t *testing.T) {
		text, err := s1.call("ports_unforward", map[string]any{"ports": []int{3000}})
		if err != nil {
			t.Fatalf("ports_unforward s1: %v", err)
		}
		t.Logf("unforward s1: %s", text)
		if !strings.Contains(text, "kept") || !strings.Contains(text, "3000") {
			t.Errorf("s1 não reportou 3000 como mantida pela outra sessão: %s", text)
		}
		expectURLBody(t, api.URL, "api-3000")
	})

	t.Run("unforward da sessão 2 fecha a porta", func(t *testing.T) {
		text, err := s2.call("ports_unforward", map[string]any{"ports": []int{3000}})
		if err != nil {
			t.Fatalf("ports_unforward s2: %v", err)
		}
		t.Logf("unforward s2: %s", text)
		waitFor(t, 10*time.Second, "porta local do forward 3000 parar de responder", func() bool {
			_, err := httpGet(api.LocalPort)
			return err != nil
		})
	})

	// Passo 7: deny.
	t.Run("deny devolve denied", func(t *testing.T) {
		pending := s1.callAsync("ports_forward", map[string]any{
			"reason": "e2e deny", "ports": []map[string]any{{"remote_port": 8080}},
		})
		var id string
		waitFor(t, 30*time.Second, "pedido de 8080 pendente", func() bool {
			id = p.pendingRequestID()
			return id != ""
		})
		if out, err := p.ag("deny", id); err != nil {
			t.Fatalf("deny: %v\n%s", err, out)
		}
		r := <-pending
		if r.err != nil {
			t.Fatalf("ports_forward (deny): %v", r.err)
		}
		results := parseForwardResults(t, r.text)
		if len(results) != 1 || results[0].State != "denied" {
			t.Errorf("deny devolveu %+v; quero denied", results)
		}
	})

	// Passo 8: a porta de 5173 segue ativa; o restart do Companion precisa restaurá-la.
	t.Run("restart do Companion restaura os forwards", func(t *testing.T) {
		if out, err := p.ag("companion", "stop", p.profile); err != nil {
			t.Fatalf("companion stop: %v\n%s", err, out)
		}
		waitFor(t, 20*time.Second, "porta da web parar após o stop", func() bool {
			_, err := httpGet(web.LocalPort)
			return err != nil
		})
		if out, err := p.ag("companion", "start", p.profile); err != nil {
			t.Fatalf("companion start: %v\n%s", err, out)
		}
		waitFor(t, 60*time.Second, "túnel reconectado", p.tunnelConnected)
		waitFor(t, 60*time.Second, "forward da web restaurado", func() bool {
			body, err := httpGet(web.LocalPort)
			return err == nil && body == "web-5173"
		})
		out, _ := p.ag("forwards")
		t.Logf("forwards após restart:\n%s", out)
	})

	// Passo 9: a sessão do agente (s1) não faz nenhuma chamada depois do restart.
	// O heartbeat falha, a sessão registra de novo e reclama a web: o forward volta
	// active, com s1 como único dono, e a URL responde.
	t.Run("agente recupera o forward após o restart sem nova chamada", func(t *testing.T) {
		var last string
		waitFor(t, 70*time.Second, "forward da web de volta ao agente s1", func() bool {
			text, err := s1.call("forwards_list", map[string]any{})
			last = text
			return err == nil && agentOwnsForward(text, web.RemotePort)
		})
		t.Logf("forwards_list de s1 após restart: %s", last)
		expectURLBody(t, web.URL, "web-5173")
	})

	_ = busy
}

// agentOwnsForward reports whether a forwards_list answer shows remote active and
// owned by exactly one session: the calling one (mine).
func agentOwnsForward(text string, remote int) bool {
	var out struct {
		Forwards []struct {
			RemotePort int      `json:"remote_port"`
			State      string   `json:"state"`
			Mine       bool     `json:"mine"`
			Owners     []string `json:"owners"`
		} `json:"forwards"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return false
	}
	for _, f := range out.Forwards {
		if f.RemotePort == remote && f.State == "active" && f.Mine && len(f.Owners) == 1 {
			return true
		}
	}
	return false
}

// expectURLBody faz GET na URL do forward e exige o corpo esperado.
func expectURLBody(t *testing.T, url, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var body string
	var err error
	for time.Now().Before(deadline) {
		body, err = getURL(url)
		if err == nil && body == want {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("GET %s = %q (%v); quero %q", url, body, err, want)
}

// hasForwardRow confere uma linha REMOTE LOCAL ... da tabela de `agentport forwards`.
func hasForwardRow(table string, remote, local int) bool {
	for _, line := range strings.Split(table, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == strconv.Itoa(remote) && f[1] == strconv.Itoa(local) {
			return true
		}
	}
	return false
}
