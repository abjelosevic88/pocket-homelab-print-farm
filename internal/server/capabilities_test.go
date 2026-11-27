package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/config"
)

// fakeCapCore is a Pocket Core that vouches for link tokens
// pcx_<alice|bob>_<search|day|widget|capture|read|write> from Pocket Hub
// and remembers the manifest Print Farm registers with.
type fakeCapCore struct {
	*httptest.Server
	mu       sync.Mutex
	manifest map[string]any
}

func newCapApp(t *testing.T) (*app, *fakeCapCore) {
	t.Helper()
	fc := &fakeCapCore{}
	scopes := map[string]string{"search": core.ScopeSearch, "day": core.ScopeDay, "widget": core.ScopeWidget, "capture": core.ScopeCapture,
		"read": "printfarm:read", "write": "printfarm:write"}
	fc.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/apps/register":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			fc.mu.Lock()
			fc.manifest = m
			fc.mu.Unlock()
			io.WriteString(w, `{}`)
		case "/api/v1/auth/introspect":
			var in struct{ Token string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			parts := strings.Split(in.Token, "_")
			if len(parts) != 3 || parts[0] != "pcx" || scopes[parts[2]] == "" {
				io.WriteString(w, `{"active":false}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "audience": "printfarm", "scope": scopes[parts[2]],
				"user": map[string]any{"id": 1, "sub": parts[1] + "@example.com", "email": parts[1] + "@example.com", "name": parts[1], "role": "member"},
				"app":  map[string]any{"id": 9, "slug": "hub", "name": "Pocket Hub"}})
		default:
			io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(fc.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL, c.CoreRegistrationKey = fc.URL, "key"; noPoll(c) }), fc
}

// items are the rows of a capability answer.
func items(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	l, ok := m["items"].([]any)
	if !ok {
		t.Fatalf("no items list in %v", m)
	}
	out := []map[string]any{}
	for _, v := range l {
		out = append(out, v.(map[string]any))
	}
	return out
}

// item finds the row with that title.
func item(t *testing.T, m map[string]any, title string) map[string]any {
	t.Helper()
	for _, it := range items(t, m) {
		if it["title"] == title {
			return it
		}
	}
	t.Fatalf("no %q in %v", title, m["items"])
	return nil
}

// metrics are a capability answer's numbers by label.
func metrics(m map[string]any) map[string]string {
	out := map[string]string{}
	l, _ := m["metrics"].([]any)
	for _, v := range l {
		x := v.(map[string]any)
		out[str(x, "label")] = str(x, "value")
	}
	return out
}

// seedFarm gives a person a printer, a filament with one spool, a model with
// its G-code, a job for Ana in the queue and a print made in March.
func (a *app) seedFarm(auth string) map[string]string {
	a.t.Helper()
	p := obj2(a.ok(auth, "POST", "/api/v1/printers", map[string]any{"name": "Mk4", "model": "Prusa MK4", "location": "Workshop"}), "printer")
	sp := obj2(a.ok(auth, "POST", "/api/v1/spools", map[string]any{"filament": map[string]any{"brand": "Prusament", "material": "PETG", "name": "Galaxy Black",
		"net_weight": 1000, "price": 2000}}), "spool")
	m, _ := a.uploadModel(auth, "cube.gcode", []byte(testGcode), nil)
	j := a.ok(auth, "POST", "/api/v1/jobs", map[string]any{"model_id": num(m, "id"), "for_whom": "Ana"})
	pr := a.ok(auth, "POST", "/api/v1/prints", map[string]any{"record": true, "model_id": num(m, "id"), "printer_id": num(p, "id"), "status": "done",
		"started_at": "2026-03-10T09:00", "hours": 2, "grams": 45, "notes": "for the shelf"})
	return map[string]string{"printer": id(p), "spool": id(sp), "filament": itoa(int64(num(sp, "filament_id"))), "model": id(m), "job": id(j), "print": id(pr)}
}

func TestPocketSearch(t *testing.T) {
	a := newApp(t, noPoll)
	ids := a.seedFarm(a.alice)
	res := a.ok(a.alice, "GET", "/api/v1/pocket/search?q=cube", nil)
	all := items(t, res)
	// the model of that name leads, then the job in the queue, then the print made
	if len(all) != 3 || all[0]["kind"] != "model" || all[1]["kind"] != "job" || all[2]["kind"] != "print" {
		t.Fatalf("cube: %v", all)
	}
	if m := all[0]; m["id"] != "model:"+ids["model"] || m["title"] != "Cube" || m["url"] != "/models/"+ids["model"] || m["snippet"] != "printed once" || m["date"] == nil {
		t.Fatalf("model: %v", m)
	}
	if j := all[1]; j["id"] != "job:"+ids["job"] || j["url"] != "/queue" || j["snippet"] != "Waiting in the queue · for Ana" {
		t.Fatalf("job: %v", j)
	}
	if p := all[2]; p["id"] != "print:"+ids["print"] || p["url"] != "/prints/"+ids["print"] || p["snippet"] != "Mk4 · Done" || !strings.HasPrefix(str(p, "date"), "2026-03-") {
		t.Fatalf("print: %v", p)
	}
	if p := item(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=mk4", nil), "Mk4"); p["kind"] != "printer" || p["id"] != "printer:"+ids["printer"] ||
		p["url"] != "/printers/"+ids["printer"] || p["snippet"] != "Prusa MK4 · Workshop" {
		t.Fatalf("printer: %v", p)
	}
	if f := item(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=galaxy", nil), "Prusament PETG Galaxy Black"); f["kind"] != "filament" || f["id"] != "filament:"+ids["filament"] ||
		f["url"] != "/filament" || f["snippet"] != "1 spool, 1000 g left" {
		t.Fatalf("filament: %v", f)
	}
	// found by who it is for: after anything named so
	if got := items(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=ana", nil)); len(got) != 1 || got[0]["kind"] != "job" || got[0]["title"] != "Cube" {
		t.Fatalf("for Ana: %v", got)
	}
	// a failed print says why, a printer put away says so
	a.ok(a.alice, "POST", "/api/v1/prints", map[string]any{"record": true, "title": "Vase", "printer_id": n(ids["printer"]), "status": "failed", "fail_reason": "adhesion",
		"started_at": "2026-03-11T09:00", "hours": 1})
	if p := item(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=vase", nil), "Vase"); p["snippet"] != "Mk4 · Failed · came off the bed" {
		t.Fatalf("failed print: %v", p)
	}
	a.ok(a.alice, "PUT", "/api/v1/printers/"+ids["printer"], map[string]any{"archived": true})
	if p := item(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=mk4", nil), "Mk4"); p["snippet"] != "Prusa MK4 · Workshop · put away" {
		t.Fatalf("printer put away: %v", p)
	}
	if got := items(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q=cube&limit=1", nil)); len(got) != 1 || got[0]["kind"] != "model" {
		t.Fatalf("limit 1: %v", got)
	}
	// nothing to look for: no words, or only signs (a % alone would match everything)
	for _, q := range []string{"", "%3F%3F", "*", "%25", "_", "+"} {
		if got := items(t, a.ok(a.alice, "GET", "/api/v1/pocket/search?q="+q, nil)); len(got) != 0 {
			t.Fatalf("q=%q finds %v", q, got)
		}
	}
	// another person's models, printers, filament, prints and jobs never appear
	for _, q := range []string{"cube", "mk4", "galaxy", "ana", "vase", "e"} {
		if got := items(t, a.ok(a.bob, "GET", "/api/v1/pocket/search?q="+q, nil)); len(got) != 0 {
			t.Fatalf("bob finds alice's %q: %v", q, got)
		}
	}
	a.expect(401, "", "GET", "/api/v1/pocket/search?q=cube", nil)
	// the old route still answers as before
	hits := a.ok(a.alice, "GET", "/api/v1/search?q=cube", nil)["hits"].([]any)
	if len(hits) != 3 {
		t.Fatalf("/search changed: %v", hits)
	}
	for i, want := range []struct{ kind, sub, path string }{{"model", "1 printed", "/models/" + ids["model"]}, {"print", "2026-03-10 · Mk4 · done", "/prints/" + ids["print"]},
		{"job", "in the queue, 0 of 1", "/queue"}} {
		h := hits[i].(map[string]any)
		if h["kind"] != want.kind || h["subtitle"] != want.sub || h["path"] != want.path || h["url"] != "http://printfarm.test"+want.path || h["title"] != "Cube" {
			t.Fatalf("/search hit %d changed: %v", i, h)
		}
	}
	if hits := a.ok(a.alice, "GET", "/api/v1/search?q=galaxy", nil)["hits"].([]any); len(hits) != 1 || hits[0].(map[string]any)["subtitle"] != "1 spools, 1000 g left" ||
		num(hits[0].(map[string]any), "id") != float64(n(ids["filament"])) {
		t.Fatalf("/search filament changed: %v", hits)
	}
}

func TestPocketDay(t *testing.T) {
	a := newApp(t, noPoll)
	ids := a.seedFarm(a.alice)
	rec := func(title, status, started string, more map[string]any) string {
		body := map[string]any{"record": true, "title": title, "printer_id": n(ids["printer"]), "status": status, "started_at": started}
		for k, v := range more {
			body[k] = v
		}
		return id(a.ok(a.alice, "POST", "/api/v1/prints", body))
	}
	vase := rec("Vase", "failed", "2026-03-10T12:00", map[string]any{"seconds": 1800, "fail_reason": "adhesion", "fail_note": "corner lifted", "grams": 5})
	rec("Hook", "cancelled", "2026-03-10T15:00", map[string]any{"seconds": 600})
	rec("Lid", "done", "2026-03-10T16:00", map[string]any{"hours": 0.5, "grams": 10})
	// upkeep done and a model added are in /day, not in the day that counts prints
	a.ok(a.alice, "POST", "/api/v1/upkeep/done", map[string]any{"printer_id": n(ids["printer"]), "title": "Oiled the rods", "done_on": "2026-03-10"})

	day := a.ok(a.alice, "GET", "/api/v1/pocket/day/2026-03-10", nil)
	if day["date"] != "2026-03-10" || day["summary"] != "2 prints finished, 1 failed, 1 stopped" || len(items(t, day)) != 4 {
		t.Fatalf("day: %v", day)
	}
	cube := item(t, day, "Cube")
	if cube["id"] != ids["print"] || cube["kind"] != "printed" || cube["done"] != true || cube["sub"] != "Mk4" || cube["time"] != "09:00" || cube["end"] != "11:00" ||
		num(cube, "minutes") != 120 || cube["url"] != "/prints/"+ids["print"] || cube["note"] != "for the shelf" {
		t.Fatalf("finished: %v", cube)
	}
	if it := item(t, day, "Vase"); it["id"] != vase || it["kind"] != "failed" || it["done"] != nil || it["sub"] != "Mk4 · came off the bed" || it["note"] != "corner lifted" ||
		it["time"] != "12:00" || it["end"] != "12:30" || num(it, "minutes") != 30 {
		t.Fatalf("failed: %v", it)
	}
	if it := item(t, day, "Hook"); it["kind"] != "stopped" || it["done"] != nil || it["sub"] != "Mk4" || num(it, "minutes") != 10 {
		t.Fatalf("stopped: %v", it)
	}
	if ms := metrics(day); len(ms) != 2 || ms["Print time"] != "3 h 10 min" || ms["Filament"] != "60 g" {
		t.Fatalf("metrics: %v", day["metrics"])
	}
	// one print, one that only failed
	rec("Clip", "done", "2026-03-12T08:00", map[string]any{"seconds": 30})
	if d := a.ok(a.alice, "GET", "/api/v1/pocket/day/2026-03-12", nil); d["summary"] != "1 print finished" || d["metrics"] != nil || item(t, d, "Clip")["minutes"] != 0.5 {
		t.Fatalf("one print: %v", d)
	}
	rec("Gear", "failed", "2026-03-13T08:00", map[string]any{"seconds": 60})
	if d := a.ok(a.alice, "GET", "/api/v1/pocket/day/2026-03-13", nil); d["summary"] != "1 print failed" || item(t, d, "Gear")["sub"] != "Mk4" {
		t.Fatalf("one failed: %v", d)
	}
	// a print that is going is on the day it started
	a.ok(a.alice, "POST", "/api/v1/prints", map[string]any{"printer_id": n(ids["printer"]), "model_id": n(ids["model"])})
	now := a.ok(a.alice, "GET", "/api/v1/pocket/day/"+today(), nil)
	if it := item(t, now, "Cube"); now["summary"] != "1 print still printing" || it["kind"] != "printing" || it["done"] != nil || it["sub"] != "Mk4" || it["time"] == nil || it["end"] != nil {
		t.Fatalf("going: %v", now)
	}
	// the day is the person's when the caller says their time zone: 23:30–00:30 in
	// London is the 15th there, at the time it ended, and 08:30–09:30 on the 15th in Tokyo
	rec("Night owl", "done", "2026-03-14T23:30:00Z", map[string]any{"hours": 1})
	if it := item(t, a.ok(a.alice, "GET", "/api/v1/pocket/day/2026-03-15?tz=Europe/London", nil), "Night owl"); it["time"] != "00:30" || it["end"] != nil {
		t.Fatalf("over midnight: %v", it)
	}
	if it := item(t, a.ok(a.alice, "GET", "/api/v1/pocket/day/2026-03-15?tz=Asia/Tokyo", nil), "Night owl"); it["time"] != "08:30" || it["end"] != "09:30" {
		t.Fatalf("in Tokyo: %v", it)
	}
	if d := a.ok(a.alice, "GET", "/api/v1/pocket/day/2026-03-14?tz=Europe/London", nil); len(items(t, d)) != 0 {
		t.Fatalf("it ended the next day: %v", d)
	}
	// a day with nothing, and somebody else's day
	if d := a.ok(a.alice, "GET", "/api/v1/pocket/day/2019-03-04", nil); d["summary"] != nil || d["metrics"] != nil || len(items(t, d)) != 0 {
		t.Fatalf("an empty day: %v", d)
	}
	if d := a.ok(a.bob, "GET", "/api/v1/pocket/day/2026-03-10", nil); d["summary"] != nil || len(items(t, d)) != 0 {
		t.Fatalf("bob sees alice's day: %v", d)
	}
	a.expect(400, a.alice, "GET", "/api/v1/pocket/day/yesterday", nil)
	a.expect(401, "", "GET", "/api/v1/pocket/day/2026-03-10", nil)
	// the old route still answers as before, upkeep included
	old := a.ok(a.alice, "GET", "/api/v1/day/2026-03-10", nil)
	kinds := map[string]int{}
	for _, v := range old["items"].([]any) {
		kinds[str(v.(map[string]any), "kind")]++
	}
	if kinds["print"] != 4 || kinds["upkeep"] != 1 || num(old, "done") != 2 || num(old, "failed") != 1 {
		t.Fatalf("/day changed: %v", old)
	}
	for _, v := range old["items"].([]any) {
		if x := v.(map[string]any); x["title"] == "Cube" && (x["text"] != "Printed Cube on Mk4 (2 h 00 min)" || x["time"] != "11:00" || x["url"] != "http://printfarm.test/prints/"+ids["print"]) {
			t.Fatalf("/day item changed: %v", x)
		}
	}
}

func TestPocketWidget(t *testing.T) {
	// a printer on the network that counts how often it is asked
	var asked atomic.Int64
	fp := &fakePrinter{state: "standby"}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		fp.moonraker().ServeHTTP(w, r)
	}))
	defer ts.Close()
	a := newApp(t, noPoll)
	ctx := context.Background()
	// the widget of a person; it never goes to a printer
	widget := func(auth string) map[string]any {
		t.Helper()
		before := asked.Load()
		w := a.ok(auth, "GET", "/api/v1/pocket/widget", nil)
		if asked.Load() != before {
			t.Fatal("the widget went to a printer")
		}
		return w
	}
	// no printer, nothing waiting: nothing to show
	if w := widget(a.bob); w["line"] != nil || w["metrics"] != nil || len(items(t, w)) != 0 {
		t.Fatalf("without a farm: %v", w)
	}
	// a printer by hand, one that was never heard from, and one that prints
	desk := obj2(a.ok(a.alice, "POST", "/api/v1/printers", map[string]any{"name": "Desk"}), "printer")
	a.ok(a.alice, "POST", "/api/v1/printers", map[string]any{"name": "Shed", "connection": "octoprint", "url": "http://127.0.0.1:1", "api_key": "k"})
	voron := obj2(a.ok(a.alice, "POST", "/api/v1/printers", map[string]any{"name": "Voron", "connection": "moonraker", "url": ts.URL}), "printer")
	if w := widget(a.alice); w["line"] != nil || len(items(t, w)) != 3 || metrics(w)["Printing"] != "0 of 3" || metrics(w)["In the queue"] != "0" {
		t.Fatalf("all quiet: %v", w)
	}
	m, _ := a.uploadModel(a.alice, "cube.gcode", []byte(testGcode), nil)
	first := a.ok(a.alice, "POST", "/api/v1/jobs", map[string]any{"model_id": num(m, "id")})
	a.ok(a.alice, "POST", "/api/v1/jobs/"+id(first)+"/start", map[string]any{"printer_id": num(voron, "id")})
	printer, _ := a.st.GetPrinter(ctx, a.ids["Alice"], int64(num(voron, "id")))
	fp.set("printing", "cube.gcode", 0.5)
	a.s.pollOne(ctx, printer)
	stand := a.ok(a.alice, "POST", "/api/v1/jobs", map[string]any{"model_id": num(m, "id"), "title": "Phone stand", "quantity": 2, "for_whom": "Ana", "due_on": "2026-12-04"})
	a.ok(a.alice, "POST", "/api/v1/jobs", map[string]any{"model_id": num(m, "id"), "title": "Hook"})
	sp := obj2(a.ok(a.alice, "POST", "/api/v1/spools", map[string]any{"filament": map[string]any{"material": "PLA", "name": "Red", "net_weight": 1000}}), "spool")
	a.ok(a.alice, "POST", "/api/v1/spools", map[string]any{"filament": map[string]any{"material": "PLA", "name": "Blue", "net_weight": 1000}})
	a.ok(a.alice, "POST", "/api/v1/spools/"+id(sp)+"/weigh", map[string]any{"left_g": 100})

	w := widget(a.alice)
	if w["line"] != "1 printing, 2 waiting in the queue" || w["url"] != "/" {
		t.Fatalf("widget: %v", w)
	}
	if ms := metrics(w); len(ms) != 3 || ms["Printing"] != "1 of 3" || ms["In the queue"] != "2" || ms["Spools running low"] != "1" {
		t.Fatalf("metrics: %v", w["metrics"])
	}
	rows := items(t, w)
	if len(rows) != 5 {
		t.Fatalf("rows: %v", rows)
	}
	for i, want := range []struct{ id, title, detail, url string }{
		{"printer:" + id(voron), "Voron", "Printing Cube · 50% · 10 min left", "/printers/" + id(voron)},
		{"printer:" + id(desk), "Desk", "Ready", "/printers/" + id(desk)},
		{"", "Shed", "Not answering", ""},
		{"job:" + id(stand), "Phone stand", "In the queue · 2 to print · for Ana · by Fri 4 Dec", "/queue"},
		{"", "Hook", "In the queue", "/queue"},
	} {
		r := rows[i]
		if r["title"] != want.title || r["detail"] != want.detail || want.id != "" && r["id"] != want.id || want.url != "" && r["url"] != want.url || r["done"] != nil {
			t.Fatalf("row %d: %v, want %v", i, r, want)
		}
	}
	// paused on the printer
	fp.set("paused", "cube.gcode", 0.5)
	printer, _ = a.st.GetPrinter(ctx, a.ids["Alice"], printer.ID)
	a.s.pollOne(ctx, printer)
	if w := widget(a.alice); w["line"] != "1 paused, 2 waiting in the queue" || items(t, w)[0]["detail"] != "Paused · Cube · 50%" ||
		metrics(w)["Printing"] != "1 of 3" {
		t.Fatalf("paused: %v", w)
	}
	// finished: the bed has to be cleared, and nothing prints
	fp.set("complete", "cube.gcode", 1)
	printer, _ = a.st.GetPrinter(ctx, a.ids["Alice"], printer.ID)
	a.s.pollOne(ctx, printer)
	w = widget(a.alice)
	if w["line"] != "1 needs you, 2 waiting in the queue" || items(t, w)[0]["title"] != "Voron" || items(t, w)[0]["detail"] != "Done, clear the bed" || metrics(w)["Printing"] != "0 of 3" {
		t.Fatalf("done: %v", w)
	}
	// many printers leave room for the queue: eight rows, three of them jobs
	for _, name := range []string{"P4", "P5", "P6", "P7", "P8", "P9"} {
		a.ok(a.alice, "POST", "/api/v1/printers", map[string]any{"name": name})
	}
	a.ok(a.alice, "POST", "/api/v1/jobs", map[string]any{"model_id": num(m, "id"), "title": "Lid"})
	a.ok(a.alice, "POST", "/api/v1/jobs", map[string]any{"model_id": num(m, "id"), "title": "Clip"})
	rows = items(t, widget(a.alice))
	if len(rows) != 8 || rows[4]["title"] != "P6" || rows[5]["title"] != "Phone stand" || rows[7]["title"] != "Lid" {
		t.Fatalf("a full widget: %v", rows)
	}
	// only a queue, no printer yet
	a.uploadModel(a.bob, "hook.gcode", []byte(testGcode), nil)
	bm := a.ok(a.bob, "GET", "/api/v1/models", nil)["models"].([]any)[0].(map[string]any)
	a.ok(a.bob, "POST", "/api/v1/jobs", map[string]any{"model_id": num(bm, "id")})
	if w := widget(a.bob); w["line"] != "1 waiting in the queue" || len(items(t, w)) != 1 || items(t, w)[0]["title"] != "Hook" ||
		len(metrics(w)) != 1 || metrics(w)["In the queue"] != "1" {
		t.Fatalf("bob's widget: %v", w)
	}
	a.expect(401, "", "GET", "/api/v1/pocket/widget", nil)
	// the widget changed nothing and started nothing
	if jobs := a.ok(a.alice, "GET", "/api/v1/jobs", nil)["jobs"].([]any); len(jobs) != 4 {
		t.Fatalf("the queue changed: %v", jobs)
	}
}

func TestPocketLinkTokens(t *testing.T) {
	a, _ := newCapApp(t)
	ids := a.seedFarm(a.alice)
	const day = "2026-03-10"
	for _, c := range []struct {
		tok, method, path string
		body              any
		want              int
	}{
		{"pcx_alice_search", "GET", "/api/v1/pocket/search?q=cube", nil, 200},
		{"pcx_alice_day", "GET", "/api/v1/pocket/day/" + day, nil, 200},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/widget", nil, 200},
		{"pcx_alice_widget", "GET", "/api/v1/auth/me", nil, 200},
		// another capability's route
		{"pcx_alice_search", "GET", "/api/v1/pocket/day/" + day, nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/pocket/search?q=cube", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/search?q=cube", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/day/" + day, nil, 403},
		// nothing can be kept here: there is no such route, and the capture scope opens nothing else
		{"pcx_alice_capture", "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-0000000000000001", "text": "Cube"}, 404},
		{"pcx_alice_capture", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_capture", "POST", "/api/v1/jobs", map[string]any{"model_id": n(ids["model"])}, 403},
		// the rest of the app
		{"pcx_alice_widget", "GET", "/api/v1/farm", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/home", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/summary", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/printers", nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/search?q=cube", nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/models", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/day/" + day, nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/prints", nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/settings", nil, 403},
		// no capability starts, pauses or stops a print, or adds to the queue
		{"pcx_alice_widget", "POST", "/api/v1/printers/" + ids["printer"] + "/control", map[string]any{"action": "pause"}, 403},
		{"pcx_alice_widget", "POST", "/api/v1/jobs/" + ids["job"] + "/start", map[string]any{"printer_id": n(ids["printer"])}, 403},
		{"pcx_alice_widget", "POST", "/api/v1/prints", map[string]any{"printer_id": n(ids["printer"]), "model_id": n(ids["model"])}, 403},
		{"pcx_alice_search", "POST", "/api/v1/jobs", map[string]any{"model_id": n(ids["model"])}, 403},
		{"pcx_alice_day", "DELETE", "/api/v1/prints/" + ids["print"], nil, 403},
		{"pcx_alice_widget", "POST", "/api/v1/pocket/widget", nil, 405},
		// the app's own scopes do not open the new routes
		{"pcx_alice_read", "GET", "/api/v1/pocket/search?q=cube", nil, 403},
		{"pcx_alice_read", "GET", "/api/v1/pocket/day/" + day, nil, 403},
		{"pcx_alice_read", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_write", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_write", "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-0000000000000002", "text": "Cube"}, 404},
		{"pcx_alice_read", "GET", "/api/v1/search?q=cube", nil, 200},
		{"pcx_alice_read", "GET", "/api/v1/day/" + day, nil, 200},
		{"pcx_nobody", "GET", "/api/v1/pocket/widget", nil, 401},
	} {
		if code, m, _ := a.call(c.tok, c.method, c.path, c.body); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%v)", c.tok, c.method, c.path, code, c.want, m)
		}
	}
	// a person signed in finds no capture route either
	a.expect(404, a.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-0000000000000003", "text": "Cube"})
	// the token acts for its person only
	if got := items(t, a.ok("pcx_alice_search", "GET", "/api/v1/pocket/search?q=cube", nil)); len(got) != 3 || got[0]["title"] != "Cube" {
		t.Fatalf("alice through the link: %v", got)
	}
	if got := items(t, a.ok("pcx_bob_search", "GET", "/api/v1/pocket/search?q=cube", nil)); len(got) != 0 {
		t.Fatalf("bob through the link finds alice's model: %v", got)
	}
	if w := a.ok("pcx_alice_widget", "GET", "/api/v1/pocket/widget", nil); len(items(t, w)) != 2 || w["line"] != "1 waiting in the queue" {
		t.Fatalf("alice's widget through the link: %v", w)
	}
	if w := a.ok("pcx_bob_widget", "GET", "/api/v1/pocket/widget", nil); len(items(t, w)) != 0 {
		t.Fatalf("bob's widget through the link: %v", w)
	}
	if d := a.ok("pcx_alice_day", "GET", "/api/v1/pocket/day/"+day, nil); d["summary"] != "1 print finished" {
		t.Fatalf("alice's day through the link: %v", d)
	}
	if d := a.ok("pcx_bob_day", "GET", "/api/v1/pocket/day/"+day, nil); len(items(t, d)) != 0 {
		t.Fatalf("bob's day through the link: %v", d)
	}
	// nothing was added, started or removed on the way
	if jobs := a.ok(a.alice, "GET", "/api/v1/jobs", nil)["jobs"].([]any); len(jobs) != 1 {
		t.Fatalf("the queue changed: %v", jobs)
	}
	if prints := a.ok(a.alice, "GET", "/api/v1/prints", nil); num(prints, "total") != 1 {
		t.Fatalf("the prints changed: %v", prints)
	}
}

func TestPocketLinkScopes(t *testing.T) {
	req := func(method, path string) *http.Request { return httptest.NewRequest(method, path, nil) }
	for _, c := range []struct {
		scope, method, path string
		want                bool
	}{
		{core.ScopeSearch, "GET", "/api/v1/pocket/search?q=cube", true},
		{core.ScopeDay, "GET", "/api/v1/pocket/day/2026-03-10", true},
		{core.ScopeWidget, "GET", "/api/v1/pocket/widget", true},
		{core.ScopeWidget, "GET", "/api/v1/auth/me", true},
		{core.ScopeDay, "GET", "/api/v1/pocket/day/2026-03-10/more", false},
		{core.ScopeDay, "GET", "/api/v1/day/2026-03-10", false},
		{core.ScopeSearch, "POST", "/api/v1/pocket/search", false},
		{core.ScopeWidget, "DELETE", "/api/v1/pocket/widget", false},
		{core.ScopeCapture, "POST", "/api/v1/pocket/capture", false},
		{"printfarm:read", "GET", "/api/v1/pocket/widget", false},
		{"printfarm:write", "POST", "/api/v1/pocket/capture", false},
		{"printfarm:read", "GET", "/api/v1/farm", true},
	} {
		if got := linkAllows([]string{c.scope}, req(c.method, c.path)); got != c.want {
			t.Errorf("%s %s %s = %v, want %v", c.scope, c.method, c.path, got, c.want)
		}
	}
}

func TestManifestCapabilities(t *testing.T) {
	a, fc := newCapApp(t)
	if err := a.s.registerWithCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc.mu.Lock()
	m := fc.manifest
	fc.mu.Unlock()
	if m["slug"] != "printfarm" || m["category"] != "Tools" {
		t.Fatalf("manifest: %v %v", m["slug"], m["category"])
	}
	search, _ := m["search"].(map[string]any)
	day, _ := m["day"].(map[string]any)
	widget, _ := m["widget"].(map[string]any)
	if search == nil || day == nil || day["label"] != "Print Farm" || day["one"] != "print" || day["many"] != "prints" || widget == nil || widget["title"] != "Printers" || m["capture"] != nil {
		t.Fatalf("capabilities: %v %v %v %v", search, day, widget, m["capture"])
	}
	bangs := map[string]bool{}
	for _, b := range search["bangs"].([]any) {
		bangs[b.(string)] = true
	}
	if len(bangs) != 3 || !bangs["printfarm"] || !bangs["3d"] || !bangs["print3d"] || len(search["kinds"].([]any)) != 5 {
		t.Fatalf("search: %v", search)
	}
	offered := map[string]bool{}
	for _, o := range m["offers"].([]any) {
		offered[o.(map[string]any)["scope"].(string)] = true
	}
	for _, sc := range []string{"printfarm:read", "printfarm:write", core.ScopeSearch, core.ScopeDay, core.ScopeWidget, core.ScopeMoveExport, core.ScopeMoveImport} {
		if !offered[sc] {
			t.Errorf("%s is not offered", sc)
		}
	}
	if offered[core.ScopeCapture] {
		t.Error("a quick line is offered, but nothing can be kept here")
	}
}
