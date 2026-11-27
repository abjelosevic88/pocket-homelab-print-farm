package server

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/store"
)

// What Pocket Search, Pocket Day and Pocket Hub ask Print Farm (the
// capabilities value in links.go): one route each, in the shape every Pocket
// app answers in. They read what /search, /day and /farm read, as the
// request's person, and change nothing. None of them goes to a printer: what
// a printer is doing is what the poller last heard and what the prints say.

// The words the pages use (web/src/lib/farm.ts).
var (
	printerStateWords = map[string]string{"idle": "Ready", "manual": "Ready", "printing": "Printing", "paused": "Paused", "done": "Done, clear the bed",
		"stopped": "Stopped", "error": "Needs you", "busy": "Busy", "offline": "Not answering"}
	printStatusWords = map[string]string{"printing": "Printing", "paused": "Paused", "done": "Done", "failed": "Failed", "cancelled": "Stopped"}
	failReasonWords  = map[string]string{"adhesion": "came off the bed", "spaghetti": "spaghetti", "warping": "warping", "clog": "clogged nozzle",
		"under_extrusion": "under-extrusion", "layer_shift": "layer shift", "stringing": "stringing", "supports": "supports failed",
		"filament_out": "ran out of filament", "power": "power cut", "cancelled_by_me": "stopped by hand", "other": ""}
)

// failReason is why a print failed, in words; "other" says nothing.
func failReason(v string) string {
	if w, ok := failReasonWords[v]; ok {
		return w
	}
	return strings.ReplaceAll(v, "_", " ")
}

// dots joins the parts of a line that are there: "Mk4 · Done".
func dots(parts ...string) string { return joinNonEmpty(" · ", parts...) }

// percent is how far a print is: "45%".
func percent(progress float64) string {
	return strconv.Itoa(int(math.Round(clamp01(progress)*100))) + "%"
}

// hasWord says whether there is a letter or a digit to look for: "??" and
// "%" are not words, and would match everything or nothing by accident.
func hasWord(q string) bool {
	for _, r := range q {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// ---------- search ----------

// pocketSearch answers Pocket Search: the models (and their files),
// printers, filaments, prints and open jobs /search finds, as one list with
// the closest matches first: a model, printer or filament of that name, then
// a job, then a print, then what only has the words in its tags, files,
// place or notes. Jobs and filaments have no page of their own: they open
// the queue and the filament shelf.
func (s *Server) pocketSearch(r *http.Request, q string, limit int) ([]core.SearchItem, error) {
	if !hasWord(q) {
		return nil, nil
	}
	ctx := r.Context()
	f, err := s.find(ctx, actorID(ctx), q, limit)
	if err != nil {
		return nil, err
	}
	low := strings.ToLower(q)
	score := func(named float64, name string) float64 {
		if strings.Contains(strings.ToLower(name), low) {
			return named
		}
		return 1
	}
	out := []core.SearchItem{}
	for _, m := range f.models {
		printed := "not printed yet"
		switch {
		case m.Prints == 1:
			printed = "printed once"
		case m.Prints > 1:
			printed = fmt.Sprintf("printed %d times", m.Prints)
		}
		date := m.CreatedAt
		if m.UpdatedAt != nil {
			date = *m.UpdatedAt
		}
		out = append(out, core.SearchItem{ID: "model:" + itoa(m.ID), Title: m.Name, Kind: "model", Date: date, URL: "/models/" + itoa(m.ID),
			Snippet: dots(m.Tags, m.Designer, printed), Score: score(4, m.Name)})
	}
	for _, p := range f.printers {
		away := ""
		if p.Archived {
			away = "put away"
		}
		out = append(out, core.SearchItem{ID: "printer:" + itoa(p.ID), Title: p.Name, Kind: "printer", URL: "/printers/" + itoa(p.ID),
			Snippet: dots(p.Model, p.Location, away), Score: score(4, p.Name)})
	}
	for _, x := range f.filaments {
		left := "no spools left"
		if x.Spools > 0 {
			left = fmt.Sprintf("%d %s, %d g left", x.Spools, plural(x.Spools, "spool", "spools"), int(x.LeftGrams))
		}
		out = append(out, core.SearchItem{ID: "filament:" + itoa(x.ID), Title: filamentName(x), Kind: "filament", URL: "/filament",
			Snippet: left, Score: score(4, filamentName(x))})
	}
	for _, p := range f.prints {
		why := ""
		if p.Status == "failed" {
			why = failReason(p.FailReason)
		}
		out = append(out, core.SearchItem{ID: "print:" + itoa(p.ID), Title: p.Title, Kind: "print", Date: p.StartedAt, URL: "/prints/" + itoa(p.ID),
			Snippet: dots(p.Printer, printStatusWords[p.Status], why), Score: score(2, p.Title)})
	}
	for _, j := range f.jobs {
		out = append(out, core.SearchItem{ID: "job:" + itoa(j.ID), Title: j.Title, Kind: "job", Date: firstNonEmpty(j.DueOn, j.CreatedAt), URL: "/queue",
			Snippet: jobWords(j), Score: score(3, j.Title)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// jobWords is where a job stands in the queue: "Waiting in the queue · 1 of
// 3 done · for Ana · by Fri 4 Dec".
func jobWords(j store.Job) string {
	where := "Waiting in the queue"
	if j.Status == "printing" || j.Printing > 0 {
		where = "Printing now"
	}
	count, whom, by := "", "", ""
	if j.Quantity > 1 {
		count = fmt.Sprintf("%d of %d done", j.DoneCount, j.Quantity)
	}
	if j.ForWhom != "" {
		whom = "for " + j.ForWhom
	}
	if j.DueOn != "" {
		by = "by " + niceDate(j.DueOn)
	}
	return dots(where, count, whom, by)
}

// ---------- day ----------

// pocketDay answers Pocket Day: the prints /day has for the day, the ones
// that ended then (finished, failed or stopped) and the ones started then
// that are still going. Upkeep done and models added, which /day also has,
// are left out: the day here counts prints. The day is the person's when the
// caller says their time zone, else the box's.
func (s *Server) pocketDay(r *http.Request, date string, tz *time.Location) (*core.Day, error) {
	ctx := r.Context()
	if tz == nil {
		tz = time.Local
	}
	t, err := time.ParseInLocation("2006-01-02", date, tz)
	if err != nil {
		return nil, err
	}
	prints, err := s.printsOn(ctx, actorID(ctx), t)
	if err != nil {
		return nil, err
	}
	if len(prints) == 0 {
		return nil, nil
	}
	// a moment as a clock in the day's zone, and whether it was on the day
	local := func(iso string) (clock string, onDay bool) {
		at, err := time.Parse(time.RFC3339Nano, iso)
		if err != nil {
			return "", false
		}
		at = at.In(tz)
		return at.Format("15:04"), at.Format("2006-01-02") == date
	}
	day := &core.Day{Date: date}
	finished, failed, stopped, going, secs, grams := 0, 0, 0, 0, 0, 0.0
	for _, p := range prints {
		it := core.DayItem{ID: itoa(p.ID), Title: p.Title, Sub: p.Printer, Note: p.Notes, URL: "/prints/" + itoa(p.ID)}
		at := ""
		if p.Progress > 0 && p.Progress < 1 {
			at = "at " + percent(p.Progress)
		}
		switch p.Status {
		case "done":
			finished++
			it.Kind, it.Done = "printed", true
		case "failed":
			failed++
			it.Kind, it.Sub, it.Note = "failed", dots(p.Printer, at, failReason(p.FailReason)), firstNonEmpty(p.FailNote, p.Notes)
		case "cancelled":
			stopped++
			it.Kind, it.Sub = "stopped", dots(p.Printer, at)
		default:
			going++
			it.Kind = "printing"
			if p.Status == "paused" {
				it.Sub = dots(p.Printer, "paused")
			}
		}
		// from when it started to when it ended; a print that ran over
		// midnight is on the day it ended, at that time
		start, sameDay := local(p.StartedAt)
		end := ""
		if p.ended {
			end, _ = local(*p.EndedAt)
		}
		switch {
		case !p.ended:
			it.Time = start
		case sameDay:
			it.Time, it.End = start, end
		default:
			it.Time = end
		}
		if p.Seconds > 0 {
			it.Minutes = math.Round(float64(p.Seconds)/6) / 10
		}
		secs += p.Seconds
		grams += p.Grams
		day.Items = append(day.Items, it)
	}
	var parts []string
	for _, c := range []struct {
		n    int
		word string
	}{{finished, "finished"}, {failed, "failed"}, {stopped, "stopped"}, {going, "still printing"}} {
		switch {
		case c.n == 0:
		case len(parts) == 0:
			parts = append(parts, fmt.Sprintf("%d %s %s", c.n, plural(c.n, "print", "prints"), c.word))
		default:
			parts = append(parts, fmt.Sprintf("%d %s", c.n, c.word))
		}
	}
	day.Summary = strings.Join(parts, ", ")
	if secs >= 60 {
		day.Metrics = append(day.Metrics, core.Metric{Label: "Print time", Value: hm(secs)})
	}
	if g := int(math.Round(grams)); g > 0 {
		day.Metrics = append(day.Metrics, core.Metric{Label: "Filament", Value: fmt.Sprintf("%d g", g)})
	}
	return day, nil
}

// ---------- widget ----------

// widgetQueueRows is how many rows the queue keeps when the printers alone
// would fill the widget (it shows eight).
const widgetQueueRows = 3

// pocketWidget answers Pocket Hub: what each printer is doing now (printing
// with how far it is and the time left, ready, not answering), then what
// waits in the queue. Printers that are printing come first, then the ones
// that want the person, the ready ones, and last the ones that do not
// answer. Someone without a printer and with an empty queue gets nothing.
func (s *Server) pocketWidget(r *http.Request) (*core.WidgetData, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	list, err := s.store.ListPrinters(ctx, uid, false)
	if err != nil {
		return nil, err
	}
	jobs, err := s.store.ListJobs(ctx, uid, "queued")
	if err != nil {
		return nil, err
	}
	if len(list) == 0 && len(jobs) == 0 {
		return nil, nil
	}
	views := make([]printerView, 0, len(list))
	for i := range list {
		views = append(views, s.printerNow(ctx, &list[i]))
	}
	rank := func(v printerView) int {
		switch v.State {
		case "printing", "paused":
			return 0
		case "error", "stopped", "done":
			return 1
		case "offline":
			return 3
		}
		return 2
	}
	sort.SliceStable(views, func(i, j int) bool { return rank(views[i]) < rank(views[j]) })

	w := &core.WidgetData{URL: "/"}
	printing, paused, needs := 0, 0, 0
	rows := 8 - min(len(jobs), widgetQueueRows)
	for _, v := range views {
		switch v.State {
		case "printing":
			printing++
		case "paused":
			paused++
		case "error", "stopped", "done":
			needs++
		}
		if len(w.Items) < rows {
			w.Items = append(w.Items, core.WidgetItem{ID: "printer:" + itoa(v.ID), Title: v.Name, Detail: printerWords(v), URL: "/printers/" + itoa(v.ID)})
		}
	}
	for _, j := range jobs {
		if len(w.Items) == 8 {
			break
		}
		w.Items = append(w.Items, core.WidgetItem{ID: "job:" + itoa(j.ID), Title: j.Title, Detail: queueWords(j), URL: "/queue"})
	}

	var parts []string
	for _, c := range []struct {
		n         int
		one, many string
	}{{printing, "printing", "printing"}, {paused, "paused", "paused"}, {needs, "needs you", "need you"}, {len(jobs), "waiting in the queue", "waiting in the queue"}} {
		if c.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c.n, plural(c.n, c.one, c.many)))
		}
	}
	w.Line = strings.Join(parts, ", ")
	if len(views) > 0 {
		w.Metrics = append(w.Metrics, core.Metric{Label: "Printing", Value: fmt.Sprintf("%d of %d", printing+paused, len(views))})
	}
	w.Metrics = append(w.Metrics, core.Metric{Label: "In the queue", Value: strconv.Itoa(len(jobs))})
	// spools with less on them than the person's "running low" mark, as the sidebar counts them
	spools, err := s.store.ListSpools(ctx, uid, store.SpoolFilter{})
	if err != nil {
		return nil, err
	}
	low, mark := 0, float64(s.settingsOf(ctx, uid).LowGrams)
	for _, x := range spools {
		if x.LeftG < mark {
			low++
		}
	}
	if low > 0 {
		w.Metrics = append(w.Metrics, core.Metric{Label: "Spools running low", Value: strconv.Itoa(low)})
	}
	return w, nil
}

// printerWords is a printer's row: "Printing Phone stand · 45% · 1 h 20 min
// left", "Ready", "Not answering".
func printerWords(v printerView) string {
	state := firstNonEmpty(printerStateWords[v.State], printerStateWords["idle"])
	if v.Print == nil {
		return state
	}
	far := "" // nothing while there is nothing to tell (a print by hand without an estimate)
	if v.Progress >= 0.005 {
		far = percent(v.Progress)
	}
	if v.State != "printing" {
		return dots(state, v.Print.Title, far)
	}
	left := ""
	switch {
	case v.Left >= 60:
		left = hm(v.Left) + " left"
	case v.Left > 0:
		left = "almost done"
	case v.Overdue:
		left = "past its time"
	}
	return dots(strings.TrimSpace(state+" "+v.Print.Title), far, left)
}

// queueWords is a waiting job's row: "In the queue · 2 to print · for Ana ·
// by Fri 4 Dec".
func queueWords(j store.Job) string {
	count, whom, by := "", "", ""
	if n := j.Quantity - j.DoneCount; n > 1 {
		count = fmt.Sprintf("%d to print", n)
	}
	if j.ForWhom != "" {
		whom = "for " + j.ForWhom
	}
	if j.DueOn != "" {
		by = "by " + niceDate(j.DueOn)
	}
	return dots("In the queue", count, whom, by)
}
