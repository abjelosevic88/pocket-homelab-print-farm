package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/printers"
	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/store"
)

// The farm: what each connected printer said last (kept in memory, asked
// every PRINTER_POLL seconds), and the life of a print — started here or
// seen on the printer, followed while it prints, finished or failed — with
// the filament taken off the loaded spool and the cost worked out.

type live struct {
	st   printers.Status
	at   time.Time
	err  string
	seen bool // it has answered at least once
}

type snap struct {
	b     []byte
	ctype string
	at    time.Time
}

type farm struct {
	mu    sync.Mutex
	live  map[int64]live
	snaps map[int64]snap
	busy  map[int64]bool // a printer being started (no second start meanwhile)
	http  *http.Client
}

func newFarm() *farm {
	return &farm{live: map[int64]live{}, snaps: map[int64]snap{}, busy: map[int64]bool{}, http: &http.Client{Timeout: 8 * time.Second}}
}

func (f *farm) get(id int64) (live, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.live[id]
	return l, ok
}

func (f *farm) set(id int64, l live) {
	f.mu.Lock()
	f.live[id] = l
	f.mu.Unlock()
}

func (s *Server) driverOf(p *store.Printer) (printers.Driver, error) {
	if p.Connection == "manual" || p.Connection == "" {
		return nil, printers.ErrNoDriver
	}
	return printers.New(printers.Config{Kind: p.Connection, URL: p.URL, APIKey: p.APIKey, HTTP: s.farm.http})
}

// connected says whether Print Farm talks to this printer itself.
func connected(p *store.Printer) bool {
	return p.Connection != "" && p.Connection != "manual" && p.URL != ""
}

// ---------- the poller ----------

func (s *Server) poller(ctx context.Context) {
	every := time.Duration(s.cfg.PollSeconds) * time.Second
	tick := time.NewTicker(every)
	defer tick.Stop()
	s.pollAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.pollAll(ctx)
		}
	}
}

func (s *Server) pollAll(ctx context.Context) {
	list, err := s.store.ConnectedPrinters(ctx)
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range list {
		p := list[i]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			s.pollOne(ctx, &p)
		}()
	}
	wg.Wait()
}

// pollOne asks one printer how it is and follows its print.
func (s *Server) pollOne(ctx context.Context, p *store.Printer) live {
	d, err := s.driverOf(p)
	if err != nil {
		return live{}
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	st, err := d.Status(cctx)
	cancel()
	prev, had := s.farm.get(p.ID)
	l := live{st: st, at: time.Now(), seen: prev.seen || err == nil}
	if err != nil {
		l.err = err.Error()
		l.st.State = printers.Offline
		if had && prev.st.State == printers.Printing {
			l.st.File, l.st.Progress = prev.st.File, prev.st.Progress
		}
	}
	s.farm.set(p.ID, l)
	if p.LastState != l.st.State {
		_ = s.store.SetPrinterState(ctx, p.ID, l.st.State, err == nil)
		s.stateChanged(ctx, p, prev, l)
	} else if err == nil && (p.LastSeenAt == nil || time.Since(parseTime(*p.LastSeenAt)) > 5*time.Minute) {
		_ = s.store.SetPrinterState(ctx, p.ID, l.st.State, true)
	}
	if err == nil {
		s.follow(ctx, p, l.st)
	}
	return l
}

// stateChanged tells the person about a printer that stopped answering or
// shows an error (once per change).
func (s *Server) stateChanged(ctx context.Context, p *store.Printer, prev, now live) {
	st := s.settingsOf(ctx, p.UserID)
	if !st.NotifyOffline {
		return
	}
	path := fmt.Sprintf("/printers/%d", p.ID)
	switch now.st.State {
	case printers.Error:
		msg := firstNonEmpty(now.st.Message, "the printer says something is wrong")
		s.tell(ctx, p.UserID, st, "⚠️ "+p.Name+" has a problem", msg, path, "high")
	case printers.Offline:
		if prev.seen && prev.st.State == printers.Printing {
			s.tell(ctx, p.UserID, st, "📡 "+p.Name+" stopped answering", "It was printing. Print Farm keeps asking; the print is kept open until the printer answers again.", path, "high")
		}
	}
}

// follow keeps the print on a printer in step with what the printer says.
func (s *Server) follow(ctx context.Context, p *store.Printer, st printers.Status) {
	open, err := s.store.OpenPrint(ctx, p.UserID, p.ID)
	if err != nil {
		open = nil
	}
	switch st.State {
	case printers.Printing, printers.Paused:
		if open != nil && st.File != "" && open.FileName != "" && !sameFile(open.FileName, st.File) {
			// a new file is printing: the last one ended between two looks
			_ = s.finishPrint(ctx, open, "done", finishIn{Progress: 1, auto: true})
			open = nil
		}
		if open == nil {
			open, err = s.printSeen(ctx, p, st)
			if err != nil {
				s.log.Debug("print seen on printer", "printer", p.ID, "err", err)
				return
			}
		}
		open.Progress, open.Status = clamp01(st.Progress), st.State
		if st.Elapsed > 0 {
			open.Seconds = st.Elapsed
		}
		if st.File != "" && open.FileName == "" {
			open.FileName = st.File
		}
		_ = s.store.UpdatePrint(ctx, open)
	case printers.Done:
		if open != nil {
			_ = s.finishPrint(ctx, open, "done", finishIn{Progress: 1, Seconds: st.Elapsed, FilamentMM: st.FilamentMM, auto: true})
		}
		s.maybeAutoStart(ctx, p)
	case printers.Stopped:
		if open != nil {
			how := firstNonEmpty(s.settingsOf(ctx, p.UserID).DefaultFail, "failed")
			_ = s.finishPrint(ctx, open, how, finishIn{Progress: st.Progress, Seconds: st.Elapsed, FilamentMM: st.FilamentMM, Note: "Stopped on the printer", auto: true})
		}
	case printers.Error:
		if open != nil && open.Status == "printing" && st.File == "" {
			_ = s.finishPrint(ctx, open, "failed", finishIn{Progress: open.Progress, Reason: "other", Note: firstNonEmpty(st.Message, "The printer showed an error"), auto: true})
		}
	case printers.Idle:
		if open != nil && open.Source == "printer" && time.Since(parseTime(open.StartedAt)) > 2*time.Minute {
			// the printer forgot it (OctoPrint after a reconnect): it is not printing any more
			_ = s.finishPrint(ctx, open, firstNonEmpty(s.settingsOf(ctx, p.UserID).DefaultFail, "failed"), finishIn{Progress: open.Progress, Note: "The printer stopped printing", auto: true})
		} else if open == nil {
			s.maybeAutoStart(ctx, p)
		}
	}
}

func sameFile(a, b string) bool {
	base := func(v string) string {
		if i := strings.LastIndex(v, "/"); i >= 0 {
			v = v[i+1:]
		}
		return strings.ToLower(v)
	}
	return base(a) == base(b)
}

// printSeen records a print that started on the printer itself (from its
// screen, or another app): it is matched to a file in the library and to a
// queued job with that file.
func (s *Server) printSeen(ctx context.Context, p *store.Printer, st printers.Status) (*store.Print, error) {
	name := st.File
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	pr := &store.Print{UserID: p.UserID, PrinterID: &p.ID, Title: cleanTitle(name), FileName: st.File, Status: st.State, Source: "printer",
		StartedAt: time.Now().Add(-time.Duration(st.Elapsed) * time.Second).UTC().Format(time.RFC3339Nano), ExternalID: st.JobID, SpoolID: p.SpoolID, Copies: 1}
	if f, err := s.store.FileByName(ctx, p.UserID, name); err == nil {
		pr.FileID, pr.ModelID, pr.EstSeconds, pr.EstGrams, pr.Settings = &f.ID, &f.ModelID, f.EstSeconds, f.EstGrams, f.Settings
		if m, err := s.store.GetModel(ctx, p.UserID, f.ModelID); err == nil {
			pr.Title = m.Name
		}
		jobs, _ := s.store.ListJobs(ctx, p.UserID, "queued", "printing")
		for _, j := range jobs {
			if j.FileID != nil && *j.FileID == f.ID && (j.PrinterID == nil || *j.PrinterID == p.ID) {
				jj := j
				pr.JobID, pr.Title = &jj.ID, j.Title
				jj.Status = "printing"
				_ = s.store.UpdateJob(ctx, &jj)
				break
			}
		}
	}
	if st.Left > 0 && pr.EstSeconds == 0 {
		pr.EstSeconds = st.Elapsed + st.Left
	}
	if err := s.store.CreatePrint(ctx, pr); err != nil {
		return nil, err
	}
	s.clearBed(ctx, p.ID, false)
	s.afterStart(ctx, p, pr)
	return pr, nil
}

func cleanTitle(file string) string {
	n := file
	for _, ext := range []string{".gcode", ".bgcode", ".gco", ".3mf"} {
		n = strings.TrimSuffix(strings.TrimSuffix(n, ext), strings.ToUpper(ext))
	}
	n = strings.Join(strings.Fields(strings.NewReplacer("_", " ").Replace(n)), " ")
	if n == "" {
		return "A print"
	}
	return n
}

// ---------- the bed ----------

func clearKey(id int64) string { return fmt.Sprintf("clear:%d", id) }

// needsClear says a finished print is still on the bed.
func (s *Server) needsClear(ctx context.Context, id int64) bool {
	v, _, ok := s.store.CacheGet(ctx, clearKey(id))
	return ok && v == "1"
}

func (s *Server) clearBed(ctx context.Context, id int64, waiting bool) {
	v := "0"
	if waiting {
		v = "1"
	}
	_ = s.store.CachePut(ctx, clearKey(id), v)
}

// ---------- starting a print ----------

// startIn is how to start a print.
type startIn struct {
	PrinterID int64  `json:"printer_id"`
	JobID     int64  `json:"job_id"`
	FileID    int64  `json:"file_id"`
	ModelID   int64  `json:"model_id"`
	SpoolID   int64  `json:"spool_id"` // load this spool first
	Title     string `json:"title"`
	Copies    int    `json:"copies"`
	Send      *bool  `json:"send"`  // send the G-code to the printer (default: the setting)
	Force     bool   `json:"force"` // even when the bed has not been cleared
}

// startPrint starts a print on a printer: the G-code goes to a connected
// printer (which starts it), a manual printer is just marked as printing.
func (s *Server) startPrint(ctx context.Context, uid int64, in startIn) (*store.Print, error) {
	p, err := s.store.GetPrinter(ctx, uid, in.PrinterID)
	if err != nil {
		return nil, &userErr{"choose a printer"}
	}
	s.farm.mu.Lock()
	if s.farm.busy[p.ID] {
		s.farm.mu.Unlock()
		return nil, &userErr{p.Name + " is being started already"}
	}
	s.farm.busy[p.ID] = true
	s.farm.mu.Unlock()
	defer func() { s.farm.mu.Lock(); delete(s.farm.busy, p.ID); s.farm.mu.Unlock() }()

	if open, err := s.store.OpenPrint(ctx, uid, p.ID); err == nil {
		return nil, &userErr{fmt.Sprintf("%s is printing “%s”: finish that first", p.Name, open.Title)}
	}
	st := s.settingsOf(ctx, uid)
	if st.NeedClear && !in.Force && s.needsClear(ctx, p.ID) {
		return nil, &userErr{"the last print is still on " + p.Name + "'s bed: say the bed is clear first"}
	}
	var job *store.Job
	if in.JobID > 0 {
		if job, err = s.store.GetJob(ctx, uid, in.JobID); err != nil {
			return nil, &userErr{"that job is not in the queue"}
		}
		if job.Status == "done" || job.Status == "cancelled" {
			return nil, &userErr{"that job is " + job.Status}
		}
		if in.FileID == 0 && job.FileID != nil {
			in.FileID = *job.FileID
		}
		if in.ModelID == 0 && job.ModelID != nil {
			in.ModelID = *job.ModelID
		}
	}
	var file *store.ModelFile
	if in.FileID > 0 {
		if file, err = s.store.GetFile(ctx, uid, in.FileID); err != nil {
			return nil, &userErr{"that file is not in the library"}
		}
		in.ModelID = file.ModelID
	}
	var model *store.Model
	if in.ModelID > 0 {
		if model, err = s.store.GetModel(ctx, uid, in.ModelID); err != nil {
			return nil, &userErr{"that model is not in the library"}
		}
		if file == nil {
			// the model's G-code (the newest), when it has one
			files, _ := s.store.ListFiles(ctx, uid, model.ID)
			for i := len(files) - 1; i >= 0; i-- {
				if files[i].Printable {
					f := files[i]
					file = &f
					break
				}
			}
		}
	}
	if in.SpoolID > 0 {
		if sp, err := s.store.GetSpool(ctx, uid, in.SpoolID); err != nil || sp.Archived {
			return nil, &userErr{"that spool is not on the shelf"}
		}
		p.SpoolID = &in.SpoolID
		_ = s.store.UpdatePrinter(ctx, p)
	}
	send := st.SendGcode
	if in.Send != nil {
		send = *in.Send
	}
	pr := &store.Print{UserID: uid, PrinterID: &p.ID, Status: "printing", StartedAt: store.Now(), SpoolID: p.SpoolID, Copies: max(1, in.Copies), Source: sourceOf(ctx)}
	switch {
	case job != nil:
		pr.Title = job.Title
		pr.JobID = &job.ID
	case model != nil:
		pr.Title = model.Name
	default:
		pr.Title = firstNonEmpty(cleanText(in.Title, maxName), "A print")
	}
	if model != nil {
		pr.ModelID = &model.ID
	}
	if file != nil {
		pr.FileID, pr.FileName, pr.EstSeconds, pr.EstGrams, pr.Settings = &file.ID, file.Name, file.EstSeconds, file.EstGrams, file.Settings
	}
	if connected(p) && send {
		if file == nil || !file.Printable {
			return nil, &userErr{"there is no G-code for this yet: slice the model and add the G-code to it, or start it on the printer (Print Farm sees it)"}
		}
		d, err := s.driverOf(p)
		if err != nil {
			return nil, err
		}
		if l, ok := s.farm.get(p.ID); ok && (l.st.State == printers.Printing || l.st.State == printers.Paused) {
			return nil, &userErr{p.Name + " is printing something already"}
		}
		f, err := os.Open(s.mediaPath(file.Path))
		if err != nil {
			return nil, &userErr{"the G-code file is gone from this server"}
		}
		uctx, cancel := context.WithTimeout(ctxDetached(ctx), 30*time.Minute)
		err = d.Upload(uctx, file.Name, f, file.Size, true)
		cancel()
		f.Close()
		if err != nil {
			return nil, &userErr{p.Name + ": " + err.Error()}
		}
		pr.Source = firstNonEmpty(pr.Source, "")
	}
	if err := s.store.CreatePrint(ctx, pr); err != nil {
		return nil, err
	}
	if job != nil {
		job.Status = "printing"
		_ = s.store.UpdateJob(ctx, job)
	}
	s.clearBed(ctx, p.ID, false)
	s.afterStart(ctx, p, pr)
	return s.store.GetPrint(ctx, uid, pr.ID)
}

// afterStart: the event, and a long print's end in the calendar.
func (s *Server) afterStart(ctx context.Context, p *store.Printer, pr *store.Print) {
	s.publish(ctx, pr.UserID, "printfarm.print_started", fmt.Sprint(pr.ID), map[string]any{"print_id": pr.ID, "title": pr.Title, "printer": p.Name,
		"est_seconds": pr.EstSeconds, "url": s.link(fmt.Sprintf("/prints/%d", pr.ID))})
	st := s.settingsOf(ctx, pr.UserID)
	if st.CalendarLong > 0 && pr.EstSeconds >= st.CalendarLong*3600 {
		bg := ctxDetached(ctx)
		go func() {
			if link, err := s.printToCalendar(bg, pr.UserID, pr, p); err == nil {
				pr.CalendarRef = link
				if cur, err := s.store.GetPrint(bg, pr.UserID, pr.ID); err == nil {
					cur.CalendarRef = link
					_ = s.store.UpdatePrint(bg, cur)
				}
			}
		}()
	}
}

// ---------- finishing a print ----------

type finishIn struct {
	Seconds    int     `json:"seconds"`
	Grams      float64 `json:"grams"`
	Progress   float64 `json:"progress"` // how far it got (failed)
	Reason     string  `json:"fail_reason"`
	Note       string  `json:"fail_note"`
	Rating     int     `json:"rating"`
	FilamentMM float64 `json:"-"`
	auto       bool    // the printer said so (not the person)
}

var failReasons = []string{"adhesion", "spaghetti", "warping", "clog", "layer_shift", "stringing", "supports", "under_extrusion", "power", "filament_out", "cancelled_by_me", "other"}

// finishPrint ends a print: done, failed or cancelled. The grams go off the
// spool, the job counts it, the cost is worked out, and the person is told.
func (s *Server) finishPrint(ctx context.Context, pr *store.Print, status string, in finishIn) error {
	if pr.Status != "printing" && pr.Status != "paused" {
		return &userErr{"that print has ended already"}
	}
	if !oneOf(status, []string{"done", "failed", "cancelled"}) {
		return &userErr{"a print ends done, failed or cancelled"}
	}
	uid := pr.UserID
	st := s.settingsOf(ctx, uid)
	now := time.Now()
	pr.Status = status
	end := now.UTC().Format(time.RFC3339Nano)
	pr.EndedAt = &end
	if in.Seconds > 0 {
		pr.Seconds = in.Seconds
	} else if pr.Seconds == 0 || !in.auto {
		if el := int(now.Sub(parseTime(pr.StartedAt)).Seconds()); el > pr.Seconds && (!in.auto || pr.Seconds == 0) {
			pr.Seconds = el
		}
	}
	switch status {
	case "done":
		pr.Progress = 1
	default:
		if in.Progress > 0 {
			pr.Progress = clamp01(in.Progress)
		} else if pr.Progress == 0 && pr.EstSeconds > 0 {
			pr.Progress = clamp01(float64(pr.Seconds) / float64(pr.EstSeconds))
		}
		pr.FailReason = firstNonEmpty(in.Reason, pr.FailReason)
		if status == "failed" && pr.FailReason == "" {
			pr.FailReason = "other"
		}
		if !oneOf(pr.FailReason, failReasons) {
			pr.FailReason = "other"
		}
		if status == "cancelled" && pr.FailReason == "other" && in.Reason == "" {
			pr.FailReason = "cancelled_by_me"
		}
		pr.FailNote = firstNonEmpty(cleanText(in.Note, maxLine), pr.FailNote)
	}
	if in.Rating > 0 {
		pr.Rating = clampInt(in.Rating, 1, 5)
	}
	var spool *store.Spool
	if pr.SpoolID != nil {
		spool, _ = s.store.GetSpool(ctx, uid, *pr.SpoolID)
	}
	switch {
	case in.Grams > 0:
		pr.Grams = in.Grams
	case in.FilamentMM > 0 && spool != nil:
		d := firstPosF(spool.Diameter, 1.75)
		pr.Grams = in.FilamentMM * math.Pi * (d / 2) * (d / 2) / 1000 * firstPosF(spool.Density, 1.24)
	case pr.EstGrams > 0:
		pr.Grams = pr.EstGrams * pr.Progress
	}
	pr.Grams = math.Round(pr.Grams*10) / 10
	var printer *store.Printer
	if pr.PrinterID != nil {
		printer, _ = s.store.GetPrinter(ctx, uid, *pr.PrinterID)
	}
	s.workOutCost(pr, printer, spool, st)
	if err := s.store.UpdatePrint(ctx, pr); err != nil {
		return err
	}
	if spool != nil && st.AutoDeduct && pr.Grams > 0 {
		_ = s.store.UseSpool(ctx, uid, spool.ID, pr.Grams, "print", pr.Title, &pr.ID)
		s.checkLow(ctx, uid, spool.ID, st)
	}
	if pr.JobID != nil {
		if j, err := s.store.GetJob(ctx, uid, *pr.JobID); err == nil && j.Status != "cancelled" {
			if status == "done" {
				j.DoneCount += max(1, pr.Copies)
			}
			j.Status = "queued"
			if j.DoneCount >= j.Quantity {
				j.Status = "done"
				fin := end
				j.FinishedAt = &fin
			}
			_ = s.store.UpdateJob(ctx, j)
			if j.Status == "done" {
				s.publish(ctx, uid, "printfarm.job_done", fmt.Sprint(j.ID), map[string]any{"job_id": j.ID, "title": j.Title, "quantity": j.Quantity, "for": j.ForWhom, "url": s.link("/queue")})
			}
		}
	}
	if printer != nil && st.NeedClear && status != "cancelled" {
		s.clearBed(ctx, printer.ID, true)
	}
	printerName := ""
	if printer != nil {
		printerName = printer.Name
	}
	path := fmt.Sprintf("/prints/%d", pr.ID)
	payload := map[string]any{"print_id": pr.ID, "title": pr.Title, "printer": printerName, "seconds": pr.Seconds, "grams": pr.Grams, "cost": pr.FilamentCost + pr.PowerCost + pr.WearCost,
		"currency": st.Currency, "url": s.link(path)}
	if status == "done" {
		s.publish(ctx, uid, "printfarm.print_finished", fmt.Sprint(pr.ID), payload)
		if st.NotifyDone {
			body := fmt.Sprintf("“%s” is done after %s", pr.Title, hm(pr.Seconds))
			if printerName != "" {
				body = printerName + ": " + body
			}
			if st.NeedClear {
				body += ". Take it off the bed and say the bed is clear for the next one."
			}
			s.tell(ctx, uid, st, "✅ Print done", body, path, "default")
		}
	} else {
		payload["reason"], payload["status"] = pr.FailReason, status
		s.publish(ctx, uid, "printfarm.print_failed", fmt.Sprint(pr.ID), payload)
		if st.NotifyFailed && (status == "failed" || in.auto) {
			body := fmt.Sprintf("“%s” stopped at %d%%", pr.Title, int(pr.Progress*100))
			if printerName != "" {
				body = printerName + ": " + body
			}
			if pr.FailNote != "" {
				body += " — " + pr.FailNote
			}
			s.tell(ctx, uid, st, "❌ Print "+map[string]string{"failed": "failed", "cancelled": "stopped"}[status], body, path, "high")
		}
	}
	return nil
}

// workOutCost fills a print's three costs, in minor units.
func (s *Server) workOutCost(pr *store.Print, p *store.Printer, sp *store.Spool, st store.Settings) {
	hours := float64(pr.Seconds) / 3600
	pr.FilamentCost, pr.PowerCost, pr.WearCost = 0, 0, 0
	if sp != nil && pr.Grams > 0 {
		price, grams := float64(firstPos(sp.Price, sp.FilamentPrice)), sp.InitialG
		if price > 0 && grams > 0 {
			pr.FilamentCost = int(math.Round(pr.Grams * price / grams))
		}
	}
	if p != nil {
		pr.PowerCost = int(math.Round(float64(p.Watts) / 1000 * hours * st.KWhPrice * 100))
		pr.WearCost = int(math.Round(float64(p.WearPerHour) * hours))
	}
}

// checkLow tells the person (once per spool) that it is running low.
func (s *Server) checkLow(ctx context.Context, uid, spoolID int64, st store.Settings) {
	sp, err := s.store.GetSpool(ctx, uid, spoolID)
	if err != nil || st.LowGrams <= 0 || sp.LeftG >= float64(st.LowGrams) {
		return
	}
	key := fmt.Sprintf("low:%d", sp.ID)
	if _, _, done := s.store.CacheGet(ctx, key); done {
		return
	}
	_ = s.store.CachePut(ctx, key, "1")
	body := fmt.Sprintf("%s has about %d g left", sp.Label(), int(sp.LeftG))
	if sp.Printer != "" {
		body += " (on " + sp.Printer + ")"
	}
	s.publish(ctx, uid, "printfarm.spool_low", fmt.Sprint(sp.ID), map[string]any{"spool_id": sp.ID, "label": sp.Label(), "left_g": sp.LeftG, "url": s.link("/filament")})
	if st.NotifyLow {
		s.tell(ctx, uid, st, "🧵 Filament running low", body+".", "/filament", "default")
	}
}

// ---------- auto start ----------

// maybeAutoStart starts the next job on a free, clear printer when the
// person asked for that.
func (s *Server) maybeAutoStart(ctx context.Context, p *store.Printer) {
	st := s.settingsOf(ctx, p.UserID)
	if !st.AutoStart || !st.SendGcode || (st.NeedClear && s.needsClear(ctx, p.ID)) {
		return
	}
	j := s.nextJobFor(ctx, p)
	if j == nil || j.FileID == nil {
		return
	}
	if _, err := s.startPrint(ctx, p.UserID, startIn{PrinterID: p.ID, JobID: j.ID}); err != nil {
		s.log.Debug("auto start", "printer", p.ID, "err", err)
	}
}

// nextJobFor is the first queued job this printer can take.
func (s *Server) nextJobFor(ctx context.Context, p *store.Printer) *store.Job {
	jobs, _ := s.store.ListJobs(ctx, p.UserID, "queued")
	var loaded *store.Spool
	if p.SpoolID != nil {
		loaded, _ = s.store.GetSpool(ctx, p.UserID, *p.SpoolID)
	}
	for _, j := range jobs {
		if j.PrinterID != nil && *j.PrinterID != p.ID {
			continue
		}
		if j.Printing > 0 && j.DoneCount+j.Printing >= j.Quantity {
			continue
		}
		if j.Material != "" && !canPrint(p, loaded, j.Material) {
			continue
		}
		if j.FilamentID != nil && loaded != nil && loaded.FilamentID != *j.FilamentID {
			continue
		}
		jj := j
		return &jj
	}
	return nil
}

// canPrint says whether a printer can take a material (by its list, else by what is loaded).
func canPrint(p *store.Printer, loaded *store.Spool, material string) bool {
	m := strings.ToLower(strings.TrimSpace(material))
	if m == "" {
		return true
	}
	if loaded != nil && strings.EqualFold(loaded.Material, m) {
		return true
	}
	if strings.TrimSpace(p.Materials) == "" {
		return loaded == nil
	}
	for _, x := range strings.Split(p.Materials, ",") {
		if strings.EqualFold(strings.TrimSpace(x), m) {
			return true
		}
	}
	return false
}

// ---------- the farm's view ----------

// printerView is a printer as the pages show it: its fields, what it says
// now, the print on it, the spool in it and what could go next.
type printerView struct {
	store.Printer
	State      string           `json:"state"` // idle | printing | paused | done | stopped | error | busy | offline | manual
	Live       *printers.Status `json:"live"`
	LiveAt     string           `json:"live_at"`
	LiveError  string           `json:"live_error"`
	Print      *store.Print     `json:"print"`
	Progress   float64          `json:"progress"`
	Left       int              `json:"left"`    // seconds
	EndsAt     string           `json:"ends_at"` // UTC
	Overdue    bool             `json:"overdue"` // a manual print past its time
	NeedsClear bool             `json:"needs_clear"`
	Spool      *store.Spool     `json:"spool"`
	Next       *store.Job       `json:"next"`
	Camera     bool             `json:"camera"`
	UpkeepDue  int              `json:"upkeep_due"`
}

// printerNow is what a printer is doing: what it said when it was last asked
// (the poller keeps that; nothing here goes to the printer), the print on it
// with how far it is and how long it has left, and whether the bed still has
// to be cleared.
func (s *Server) printerNow(ctx context.Context, p *store.Printer) printerView {
	v := printerView{Printer: *p, State: "manual"}
	v.APIKey = ""
	if connected(p) {
		v.State = firstNonEmpty(p.LastState, "offline")
		if l, ok := s.farm.get(p.ID); ok {
			st := l.st
			v.Live, v.LiveAt, v.LiveError, v.State = &st, l.at.UTC().Format(time.RFC3339), l.err, st.State
			v.Camera = p.WebcamURL != "" || st.Webcam != ""
		} else {
			v.Camera = p.WebcamURL != ""
		}
	} else {
		v.Camera = p.WebcamURL != ""
	}
	if pr, err := s.store.OpenPrint(ctx, p.UserID, p.ID); err == nil {
		v.Print = pr
		if v.State == "manual" {
			v.State = pr.Status
		}
		v.Progress = pr.Progress
		el := int(time.Since(parseTime(pr.StartedAt)).Seconds())
		if v.Live != nil && v.Live.State == printers.Printing || v.Live != nil && v.Live.State == printers.Paused {
			v.Progress = clamp01(v.Live.Progress)
			v.Left = v.Live.Left
		} else if pr.EstSeconds > 0 && pr.Status == "printing" {
			v.Progress = clamp01(float64(el) / float64(pr.EstSeconds))
			v.Left = max(0, pr.EstSeconds-el)
			v.Overdue = el > pr.EstSeconds+300
		}
		if v.Left == 0 && pr.EstSeconds > 0 && v.Progress < 1 && pr.Status == "printing" {
			v.Left = int(float64(pr.EstSeconds) * (1 - v.Progress))
		}
		if v.Left > 0 {
			v.EndsAt = time.Now().Add(time.Duration(v.Left) * time.Second).UTC().Format(time.RFC3339)
		}
	} else if v.State == "manual" {
		v.State = "idle"
	}
	v.NeedsClear = s.needsClear(ctx, p.ID) && v.Print == nil
	if v.NeedsClear && v.State == "idle" {
		v.State = "done"
	}
	return v
}

func (s *Server) viewPrinter(ctx context.Context, p *store.Printer, upkeep []store.Upkeep) printerView {
	v := s.printerNow(ctx, p)
	if p.SpoolID != nil {
		if sp, err := s.store.GetSpool(ctx, p.UserID, *p.SpoolID); err == nil {
			v.Spool = sp
		}
	}
	if v.Print == nil {
		v.Next = s.nextJobFor(ctx, p)
	}
	for _, u := range upkeep {
		if u.PrinterID == p.ID && u.Due {
			v.UpkeepDue++
		}
	}
	return v
}

func (s *Server) farmView(ctx context.Context, uid int64) []printerView {
	list, _ := s.store.ListPrinters(ctx, uid, false)
	upkeep := s.upkeepWithDue(ctx, uid, 0, list)
	out := make([]printerView, 0, len(list))
	for i := range list {
		out = append(out, s.viewPrinter(ctx, &list[i], upkeep))
	}
	return out
}

// ---------- helpers ----------

func parseTime(v string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Now()
	}
	return t
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func firstPosF(v ...float64) float64 {
	for _, x := range v {
		if x > 0 {
			return x
		}
	}
	return 0
}

// hm is a duration in words: 2 h 05 min, 45 min.
func hm(sec int) string {
	if sec < 60 {
		return fmt.Sprintf("%d s", sec)
	}
	h, m := sec/3600, (sec/60)%60
	if h == 0 {
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%d h %02d min", h, m)
}

// tell sends a message to the person's phone and to Pocket Alerts.
func (s *Server) tell(ctx context.Context, uid int64, st store.Settings, title, body, path, priority string) {
	s.push(ctx, uid, st, title, body, path)
	bg := ctxDetached(ctx)
	go s.alert(bg, uid, title, body, path, priority)
}

func settingsJSON(m map[string]string) json.RawMessage {
	if len(m) == 0 {
		return json.RawMessage("{}")
	}
	b, _ := json.Marshal(m)
	return b
}
