package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/config"
)

// newMoveApp is newApp behind a Pocket Core that vouches for Pocket Move's
// link tokens: pcx_<alice|bob>_<in|out|read> (move:import / move:export / another scope).
func newMoveApp(t *testing.T) *app {
	t.Helper()
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct{ Token string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.Split(in.Token, "_")
		if len(parts) != 3 || parts[0] != "pcx" {
			io.WriteString(w, `{"active":false}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "audience": "printfarm",
			"scope": map[string]string{"in": core.ScopeMoveImport, "out": core.ScopeMoveExport, "read": "printfarm:read"}[parts[2]],
			"user":  map[string]any{"id": 1, "sub": parts[1] + "@example.com", "email": parts[1] + "@example.com", "name": parts[1], "role": "member"},
			"app":   map[string]any{"id": 9, "slug": "move", "name": "Pocket Move"}})
	}))
	t.Cleanup(coreSrv.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL = coreSrv.URL; noPoll(c) })
}

func (a *app) moveRaw(auth, method, path, ctype string, body []byte) (int, []byte) {
	a.t.Helper()
	req, _ := http.NewRequest(method, a.url+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+auth)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

// moveImport posts a file to an import as Pocket Move does and returns the answer.
func (a *app) moveImport(auth, path, ctype string, body []byte) map[string]any {
	a.t.Helper()
	code, out := a.moveRaw(auth, "POST", path, ctype, body)
	if code != 200 {
		a.t.Fatalf("POST %s → %d %s", path, code, out)
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	return m
}

type moveCase struct {
	tok, method, path string
	want              int
}

// checkMoveScopes: move:export reaches the description and the exports,
// move:import the description and the imports, another scope neither.
func checkMoveScopes(t *testing.T, a *app, exports, imports int, cases []moveCase) {
	t.Helper()
	for _, c := range append([]moveCase{
		{"pcx_bob_out", "GET", core.PortablePath, 200},
		{"pcx_bob_in", "GET", core.PortablePath, 200},
		{"pcx_bob_read", "GET", core.PortablePath, 403},
		{"pcx_bob_out", "GET", "/api/v1/settings", 403},
		{"pcx_bob_in", "PUT", "/api/v1/settings", 403},
		{"pcx_bob_out", "GET", "/api/v1/backup", 403},
	}, cases...) {
		if code, body := a.moveRaw(c.tok, c.method, c.path, "application/json", []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	_, body := a.moveRaw("pcx_bob_out", "GET", core.PortablePath, "", nil)
	var p core.Portable
	if err := json.Unmarshal(body, &p); err != nil || len(p.Exports) != exports || len(p.Imports) != imports {
		t.Fatalf("portable: %s", body)
	}
	kinds := 0
	for _, e := range p.Exports {
		if e.Kind == core.KindPocket {
			kinds++
		}
	}
	if kinds != 1 {
		t.Fatalf("exactly one pocket export: %s", body)
	}
}

func (a *app) moveExport(auth, path string) []byte {
	a.t.Helper()
	code, b := a.moveRaw(auth, "GET", path, "", nil)
	if code != 200 {
		a.t.Fatalf("GET %s → %d %s", path, code, b)
	}
	return b
}

func TestMoveLinkScopes(t *testing.T) {
	a := newMoveApp(t)
	checkMoveScopes(t, a, 2, 2, []moveCase{
		{"pcx_bob_out", "GET", "/api/v1/export.json", 200},
		{"pcx_bob_out", "GET", "/api/v1/export.csv", 200},
		{"pcx_bob_out", "POST", "/api/v1/import", 403},
		{"pcx_bob_out", "POST", "/api/v1/import/spoolman", 403},
		{"pcx_bob_out", "GET", "/api/v1/printers", 403},
		{"pcx_bob_in", "GET", "/api/v1/export.json", 403},
		{"pcx_bob_in", "POST", "/api/v1/printers", 403},
		{"pcx_bob_in", "POST", "/api/v1/prints", 403},
		{"pcx_bob_in", "POST", "/api/v1/models/upload", 403},
		{"pcx_bob_read", "GET", "/api/v1/export.json", 403},
	})
}

// Move brings Alice's farm to Bob: each import adds once, and nothing of one
// person shows up for the other.
func TestMoveImportsAddOnce(t *testing.T) {
	a := newMoveApp(t)
	p := obj2(a.ok(a.alice, "POST", "/api/v1/printers", map[string]any{"name": "Mk4", "connection": "octoprint", "url": "http://octopi.local", "api_key": "SECRET"}), "printer")
	a.ok(a.alice, "POST", "/api/v1/spools", map[string]any{"filament": map[string]any{"material": "PLA", "name": "Red"}})
	a.ok(a.alice, "POST", "/api/v1/prints", map[string]any{"record": true, "title": "Thing", "printer_id": num(p, "id"), "hours": 1})

	dump := a.moveExport("pcx_alice_out", "/api/v1/export.json")
	if !strings.Contains(string(dump), "Mk4") || strings.Contains(string(dump), "SECRET") {
		t.Fatalf("alice's export: %s", dump)
	}
	if b := a.moveExport("pcx_bob_out", "/api/v1/export.json"); strings.Contains(string(b), "Mk4") {
		t.Fatalf("bob's export holds alice's farm: %s", b)
	}
	if c := a.moveExport("pcx_alice_out", "/api/v1/export.csv"); !strings.Contains(string(c), "Thing") {
		t.Fatalf("alice's csv: %s", c)
	}
	first := a.moveImport("pcx_bob_in", "/api/v1/import", "application/json", dump)
	second := a.moveImport("pcx_bob_in", "/api/v1/import", "application/json", dump)
	if num(first, "printers") != 1 || num(first, "spools") != 1 || num(first, "prints") != 1 || num(second, "imported") != 0 {
		t.Fatalf("imports: %v then %v", first, second)
	}

	spoolman := []byte(`[{"id":4,"filament":{"id":2,"name":"Galaxy Black","material":"PETG","vendor":{"name":"Prusament"},"weight":1000},"remaining_weight":640}]`)
	s1 := a.moveImport("pcx_bob_in", "/api/v1/import/spoolman", "application/json", spoolman)
	s2 := a.moveImport("pcx_bob_in", "/api/v1/import/spoolman", "application/json", spoolman)
	if num(s1, "spools") != 1 || num(s1, "filaments") != 1 || num(s2, "spools")+num(s2, "filaments") != 0 {
		t.Fatalf("spoolman: %v then %v", s1, s2)
	}
	if l, _ := a.ok(a.bob, "GET", "/api/v1/printers", nil)["printers"].([]any); len(l) != 1 {
		t.Fatalf("bob has %d printers", len(l))
	}
	if l, _ := a.ok(a.alice, "GET", "/api/v1/printers", nil)["printers"].([]any); len(l) != 1 {
		t.Fatalf("alice has %d printers", len(l))
	}
}
