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
	from, to := t.UTC().Format(time.RFC3339Nano), t.AddDate(0, 0, 1).UTC().Format(time.RFC3339Nano)
	items := []map[string]any{}
	// prints that ended (or started and are still going) that day
	recent, _, _ := s.store.ListPrints(ctx, uid, store.PrintFilter{From: t.AddDate(0, 0, -3).UTC().Format(time.RFC3339Nano), To: to, Limit: 500})
	done, failed, secs, grams := 0, 0, 0, 0.0
	for _, p := range recent {
		ended := p.EndedAt != nil && *p.EndedAt >= from && *p.EndedAt < to
		started := p.StartedAt >= from && p.StartedAt < to
		if !ended && !(started && p.EndedAt == nil) {
			continue
		}
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
	low := strings.ToLower(q)
	hit := func(kind string, id int64, title, sub, path string) {
		if len(hits) < lim {
			hits = append(hits, map[string]any{"kind": kind, "id": id, "title": title, "subtitle": sub, "url": s.link(path), "path": path})
		}
	}
	models, _, _ := s.store.ListModels(ctx, uid, store.ModelFilter{Q: q, Limit: lim})
	for _, m := range models {
		hit("model", m.ID, m.Name, joinNonEmpty(" · ", m.Tags, m.Designer, fmt.Sprintf("%d printed", m.Prints)), fmt.Sprintf("/models/%d", m.ID))
	}
	printers, _ := s.store.ListPrinters(ctx, uid, true)
	for _, p := range printers {
		if strings.Contains(strings.ToLower(p.Name+" "+p.Model+" "+p.Location), low) {
			hit("printer", p.ID, p.Name, joinNonEmpty(" · ", p.Model, p.Location), fmt.Sprintf("/printers/%d", p.ID))
		}
	}
	fils, _ := s.store.ListFilaments(ctx, uid)
	for _, f := range fils {
		if strings.Contains(strings.ToLower(f.Brand+" "+f.Material+" "+f.Name), low) {
			hit("filament", f.ID, strings.TrimSpace(f.Brand+" "+f.Material+" "+f.Name), fmt.Sprintf("%d spools, %d g left", f.Spools, int(f.LeftGrams)), "/filament")
		}
	}
	prints, _, _ := s.store.ListPrints(ctx, uid, store.PrintFilter{Q: q, Limit: lim})
	for _, p := range prints {
		hit("print", p.ID, p.Title, joinNonEmpty(" · ", localDay(p.StartedAt), p.Printer, p.Status), fmt.Sprintf("/prints/%d", p.ID))
	}
	jobs, _ := s.store.ListJobs(ctx, uid, "queued", "printing")
	for _, j := range jobs {
		if strings.Contains(strings.ToLower(j.Title+" "+j.ForWhom+" "+j.Notes), low) {
			hit("job", j.ID, j.Title, fmt.Sprintf("in the queue, %d of %d", j.DoneCount, j.Quantity), "/queue")
		}
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
