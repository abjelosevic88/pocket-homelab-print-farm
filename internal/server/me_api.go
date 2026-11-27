package server

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/store"
)

func ctxDetached(ctx context.Context) context.Context { return context.WithoutCancel(ctx) }

func localDay(iso string) string {
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		return ""
	}
	return t.In(time.Local).Format("2006-01-02")
}

func localClock(iso string) string {
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		return ""
	}
	return t.In(time.Local).Format("15:04")
}

// handleHome is what the sidebar needs: counts and what wants the person.
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	views := s.farmView(ctx, uid)
	printing, attention := 0, 0
	for _, v := range views {
		switch v.State {
		case "printing", "paused":
			printing++
		case "error", "done", "stopped":
			attention++
		}
		if v.Overdue {
			attention++
		}
	}
	jobs, _ := s.store.ListJobs(ctx, uid, "queued", "printing")
	_, models, _ := s.store.ListModels(ctx, uid, store.ModelFilter{Limit: 1})
	spools, _ := s.store.ListSpools(ctx, uid, store.SpoolFilter{})
	st := s.settingsOf(ctx, uid)
	low := 0
	for _, x := range spools {
		if x.LeftG < float64(st.LowGrams) {
			low++
		}
	}
	list, _ := s.store.ListPrinters(ctx, uid, false)
	due := 0
	for _, u := range s.upkeepWithDue(ctx, uid, 0, list) {
		if u.Due {
			due++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"printers": len(views), "printing": printing, "attention": attention, "queued": len(jobs), "models": models,
		"spools": len(spools), "low": low, "upkeep_due": due, "farm": views})
}

// handleSummary is a short overview for other apps (Pocket Hub, AI, Day).
func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	views := s.farmView(ctx, uid)
	now := []map[string]any{}
	for _, v := range views {
		item := map[string]any{"printer": v.Name, "state": v.State, "url": s.link(fmt.Sprintf("/printers/%d", v.ID))}
		if v.Print != nil {
			item["print"], item["progress"], item["left_seconds"], item["ends_at"] = v.Print.Title, math.Round(v.Progress*100), v.Left, v.EndsAt
		}
		if v.Spool != nil {
			item["filament"], item["filament_left_g"] = v.Spool.Label(), math.Round(v.Spool.LeftG)
		}
		now = append(now, item)
	}
	jobs, _ := s.store.ListJobs(ctx, uid, "queued")
	next := []map[string]any{}
	for i, j := range jobs {
		if i >= 5 {
			break
		}
		next = append(next, map[string]any{"title": j.Title, "quantity": j.Quantity - j.DoneCount, "for": j.ForWhom, "due_on": j.DueOn, "material": j.Material})
	}
	week := time.Now().AddDate(0, 0, -7).UTC().Format(time.RFC3339Nano)
	prints, _, _ := s.store.ListPrints(ctx, uid, store.PrintFilter{From: week, Limit: 1000})
	done, failed, hours, grams := 0, 0, 0.0, 0.0
	for _, p := range prints {
		switch p.Status {
		case "done":
			done++
		case "failed":
			failed++
		}
		hours += float64(p.Seconds) / 3600
		grams += p.Grams
	}
	st := s.settingsOf(ctx, uid)
	spools, _ := s.store.ListSpools(ctx, uid, store.SpoolFilter{})
	low := []string{}
	for _, x := range spools {
		if x.LeftG < float64(st.LowGrams) {
			low = append(low, fmt.Sprintf("%s (%d g)", x.Label(), int(x.LeftG)))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"printers": now, "queue": len(jobs), "next": next, "week": map[string]any{"done": done, "failed": failed, "hours": round1(hours), "grams": math.Round(grams)},
		"low_filament": low, "url": s.link("/")})
}

// dayPrint is a print that belongs to a day: it ended then, or it started
// then and is still going.
type dayPrint struct {
	store.Print
	ended bool
}

// printsOn are a person's prints of the day that starts at t, newest first.
func (s *Server) printsOn(ctx context.Context, uid int64, t time.Time) ([]dayPrint, error) {
	from, to := t.UTC().Format(time.RFC3339Nano), t.AddDate(0, 0, 1).UTC().Format(time.RFC3339Nano)
	recent, _, err := s.store.ListPrints(ctx, uid, store.PrintFilter{From: t.AddDate(0, 0, -3).UTC().Format(time.RFC3339Nano), To: to, Limit: 500})
	out := []dayPrint{}
	for _, p := range recent {
		ended := p.EndedAt != nil && *p.EndedAt >= from && *p.EndedAt < to
		started := p.StartedAt >= from && p.StartedAt < to
		if !ended && !(started && p.EndedAt == nil) {
			continue
		}
		out = append(out, dayPrint{Print: p, ended: ended})
	}
	return out, err
}

// handleDay is what was printed on one day, for Pocket Day's journal.
func (s *Server) handleDay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	day := chiParam(r, "date")
	t, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "a day is YYYY-MM-DD")
		return
	}
	items := []map[string]any{}
	// prints that ended (or started and are still going) that day
	done, failed, secs, grams := 0, 0, 0, 0.0
	prints, _ := s.printsOn(ctx, uid, t)
	for _, dp := range prints {
		p, ended := dp.Print, dp.ended
		var txt string
		switch p.Status {
		case "done":
			done++
			txt = fmt.Sprintf("Printed %s", p.Title)
		case "failed":
			failed++
			txt = fmt.Sprintf("%s failed at %d%%", p.Title, int(p.Progress*100))
		case "cancelled":
			txt = fmt.Sprintf("Stopped %s", p.Title)
		default:
			txt = fmt.Sprintf("Started %s", p.Title)
		}
		if p.Printer != "" {
			txt += " on " + p.Printer
		}
		if p.Seconds > 0 && p.Status == "done" {
			txt += " (" + hm(p.Seconds) + ")"
		}
		secs += p.Seconds
		grams += p.Grams
		when := p.StartedAt
		if p.EndedAt != nil && ended {
			when = *p.EndedAt
		}
		item := map[string]any{"kind": "print", "id": p.ID, "title": p.Title, "text": txt, "status": p.Status, "time": localClock(when), "printer": p.Printer,
			"grams": math.Round(p.Grams), "seconds": p.Seconds, "url": s.link(fmt.Sprintf("/prints/%d", p.ID))}
		if p.FailReason != "" {
			item["reason"] = p.FailReason
		}
		items = append(items, item)
	}
	logs, _ := s.store.ListUpkeepLog(ctx, uid, 0, 200)
	for _, l := range logs {
		if l.DoneOn == day {
			items = append(items, map[string]any{"kind": "upkeep", "id": l.ID, "title": l.Title, "text": fmt.Sprintf("%s: %s", l.Printer, l.Title), "url": s.link(fmt.Sprintf("/printers/%d", l.PrinterID))})
		}
	}
	models, _, _ := s.store.ListModels(ctx, uid, store.ModelFilter{Limit: 200})
	for _, m := range models {
		if localDay(m.CreatedAt) == day {
			items = append(items, map[string]any{"kind": "model", "id": m.ID, "title": m.Name, "text": "Added the model " + m.Name, "time": localClock(m.CreatedAt), "url": s.link(fmt.Sprintf("/models/%d", m.ID))})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"date": day, "items": items, "done": done, "failed": failed, "hours": round1(float64(secs) / 3600), "grams": math.Round(grams)})
}

// found is what a person's words match: models (and their files), printers
// (the put-away ones too), filaments, prints and the jobs still open. Each
// list is at most as long as the limit asked for.
type found struct {
	models    []store.Model
	printers  []store.Printer
	filaments []store.Filament
	prints    []store.Print
	jobs      []store.Job
}

// find looks a person's words up in everything of theirs. It keeps going
// when one list cannot be read and returns the first error with the rest.
func (s *Server) find(ctx context.Context, uid int64, q string, lim int) (found, error) {
	var f found
	var first error
	keep := func(err error) {
		if first == nil {
			first = err
		}
	}
	low := strings.ToLower(q)
	var err error
	f.models, _, err = s.store.ListModels(ctx, uid, store.ModelFilter{Q: q, Limit: lim})
	keep(err)
	printers, err := s.store.ListPrinters(ctx, uid, true)
	keep(err)
	for _, p := range printers {
		if len(f.printers) < lim && strings.Contains(strings.ToLower(p.Name+" "+p.Model+" "+p.Location), low) {
			f.printers = append(f.printers, p)
		}
	}
	fils, err := s.store.ListFilaments(ctx, uid)
	keep(err)
	for _, x := range fils {
		if len(f.filaments) < lim && strings.Contains(strings.ToLower(x.Brand+" "+x.Material+" "+x.Name), low) {
			f.filaments = append(f.filaments, x)
		}
	}
	f.prints, _, err = s.store.ListPrints(ctx, uid, store.PrintFilter{Q: q, Limit: lim})
	keep(err)
	jobs, err := s.store.ListJobs(ctx, uid, "queued", "printing")
	keep(err)
	for _, j := range jobs {
		if len(f.jobs) < lim && strings.Contains(strings.ToLower(j.Title+" "+j.ForWhom+" "+j.Notes), low) {
			f.jobs = append(f.jobs, j)
		}
	}
	return f, first
}

// filamentName is a filament as lists call it: "Prusament PLA Galaxy Black".
func filamentName(f store.Filament) string {
	return strings.TrimSpace(f.Brand + " " + f.Material + " " + f.Name)
}

// handleSearch finds models (and their files), printers, filaments and prints.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	hits := []map[string]any{}
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
		return
	}
	lim := clampInt(firstPos(queryInt(r, "limit"), 20), 1, 50)
	hit := func(kind string, id int64, title, sub, path string) {
		if len(hits) < lim {
			hits = append(hits, map[string]any{"kind": kind, "id": id, "title": title, "subtitle": sub, "url": s.link(path), "path": path})
		}
	}
	f, _ := s.find(ctx, uid, q, lim)
	for _, m := range f.models {
		hit("model", m.ID, m.Name, joinNonEmpty(" · ", m.Tags, m.Designer, fmt.Sprintf("%d printed", m.Prints)), fmt.Sprintf("/models/%d", m.ID))
	}
	for _, p := range f.printers {
		hit("printer", p.ID, p.Name, joinNonEmpty(" · ", p.Model, p.Location), fmt.Sprintf("/printers/%d", p.ID))
	}
	for _, x := range f.filaments {
		hit("filament", x.ID, filamentName(x), fmt.Sprintf("%d spools, %d g left", x.Spools, int(x.LeftGrams)), "/filament")
	}
	for _, p := range f.prints {
		hit("print", p.ID, p.Title, joinNonEmpty(" · ", localDay(p.StartedAt), p.Printer, p.Status), fmt.Sprintf("/prints/%d", p.ID))
	}
	for _, j := range f.jobs {
		hit("job", j.ID, j.Title, fmt.Sprintf("in the queue, %d of %d", j.DoneCount, j.Quantity), "/queue")
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
}

// handleStats: a year of printing in numbers.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	months := clampInt(firstPos(queryInt(r, "months"), 12), 1, 36)
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local).AddDate(0, -(months - 1), 0)
	prints, _, _ := s.store.ListPrints(ctx, uid, store.PrintFilter{From: start.UTC().Format(time.RFC3339Nano), Limit: 100000})
	type month struct {
		Month  string  `json:"month"`
		Done   int     `json:"done"`
		Failed int     `json:"failed"`
		Hours  float64 `json:"hours"`
		Grams  float64 `json:"grams"`
		Cost   int     `json:"cost"`
	}
	ms := make([]month, months)
	idx := map[string]int{}
	for i := 0; i < months; i++ {
		k := start.AddDate(0, i, 0).Format("2006-01")
		ms[i].Month, idx[k] = k, i
	}
	type agg struct {
		Name   string  `json:"name"`
		Done   int     `json:"done"`
		Failed int     `json:"failed"`
		Hours  float64 `json:"hours"`
		Grams  float64 `json:"grams"`
		Color  string  `json:"color,omitempty"`
	}
	byPrinter, byMaterial, byModel := map[string]*agg{}, map[string]*agg{}, map[string]*agg{}
	reasons := map[string]int{}
	tot := agg{Name: "all"}
	cost := 0
	add := func(m map[string]*agg, k string, p store.Print) {
		if k == "" {
			k = "—"
		}
		a := m[k]
		if a == nil {
			a = &agg{Name: k}
			m[k] = a
		}
		switch p.Status {
		case "done":
			a.Done++
		case "failed":
			a.Failed++
		}
		a.Hours += float64(p.Seconds) / 3600
		a.Grams += p.Grams
	}
	for _, p := range prints {
		k := localDay(p.StartedAt)
		if len(k) < 7 {
			continue
		}
		i, ok := idx[k[:7]]
		if !ok {
			continue
		}
		switch p.Status {
		case "done":
			ms[i].Done++
		case "failed":
			ms[i].Failed++
			reasons[firstNonEmpty(p.FailReason, "other")]++
		}
		ms[i].Hours += float64(p.Seconds) / 3600
		ms[i].Grams += p.Grams
		ms[i].Cost += p.Cost
		add(byPrinter, p.Printer, p)
		add(byMaterial, p.Material, p)
		add(byModel, p.Title, p)
		add(map[string]*agg{"all": &tot}, "all", p)
		cost += p.Cost
	}
	for i := range ms {
		ms[i].Hours, ms[i].Grams = round1(ms[i].Hours), math.Round(ms[i].Grams)
	}
	list := func(m map[string]*agg, by func(a, b *agg) bool, n int) []agg {
		out := []*agg{}
		for _, a := range m {
			a.Hours, a.Grams = round1(a.Hours), math.Round(a.Grams)
			out = append(out, a)
		}
		sort.Slice(out, func(i, j int) bool { return by(out[i], out[j]) })
		res := []agg{}
		for i, a := range out {
			if i >= n {
				break
			}
			res = append(res, *a)
		}
		return res
	}
	type rc struct {
		Reason string `json:"reason"`
		Count  int    `json:"count"`
	}
	rs := []rc{}
	for k, v := range reasons {
		rs = append(rs, rc{k, v})
	}
	sort.Slice(rs, func(i, j int) bool {
		return rs[i].Count > rs[j].Count || rs[i].Count == rs[j].Count && rs[i].Reason < rs[j].Reason
	})
	// filament colours for the materials chart
	fils, _ := s.store.ListFilaments(ctx, uid)
	shelf := map[string]float64{}
	for _, f := range fils {
		shelf[f.Material] += f.LeftGrams
	}
	st := s.settingsOf(ctx, uid)
	success := 0.0
	if tot.Done+tot.Failed > 0 {
		success = math.Round(float64(tot.Done) / float64(tot.Done+tot.Failed) * 100)
	}
	writeJSON(w, http.StatusOK, map[string]any{"months": ms, "printers": list(byPrinter, func(a, b *agg) bool { return a.Hours > b.Hours }, 20),
		"materials": list(byMaterial, func(a, b *agg) bool { return a.Grams > b.Grams }, 12), "models": list(byModel, func(a, b *agg) bool { return a.Done+a.Failed > b.Done+b.Failed }, 10),
		"reasons": rs, "total": map[string]any{"done": tot.Done, "failed": tot.Failed, "hours": round1(tot.Hours), "grams": math.Round(tot.Grams), "cost": cost, "success": success},
		"shelf": shelf, "currency": st.Currency})
}
