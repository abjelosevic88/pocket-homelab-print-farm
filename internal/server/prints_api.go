package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/store"
)

// ---- prints: the history ----

func (s *Server) printOf(w http.ResponseWriter, r *http.Request) (*store.Print, bool) {
	p, err := s.store.GetPrint(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "print")
		return nil, false
	}
	return p, true
}

func (s *Server) handlePrints(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	f := store.PrintFilter{PrinterID: int64(queryInt(r, "printer")), ModelID: int64(queryInt(r, "model")), JobID: int64(queryInt(r, "job")), SpoolID: int64(queryInt(r, "spool")),
		Q: q.Get("q"), Keep: queryBool(r, "keep"), Limit: clampInt(firstPos(queryInt(r, "limit"), 100), 1, 1000), Offset: max(0, queryInt(r, "offset"))}
	if st := q.Get("status"); st != "" {
		f.Status = strings.Split(st, ",")
	}
	if m := q.Get("month"); len(m) == 7 {
		if t, err := time.ParseInLocation("2006-01", m, time.Local); err == nil {
			f.From, f.To = t.UTC().Format(time.RFC3339Nano), t.AddDate(0, 1, 0).UTC().Format(time.RFC3339Nano)
		}
	}
	list, total, err := s.store.ListPrints(ctx, actorID(ctx), f)
	if err != nil {
		s.fail(w, err, "prints")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"prints": list, "total": total})
}

// handleCreatePrint starts a print on a printer, or writes down one made
// earlier ("record": true, with how it went).
func (s *Server) handleCreatePrint(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	b, err := s.readBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	var head struct {
		Record bool `json:"record"`
	}
	_ = json.Unmarshal(b, &head)
	if !head.Record {
		if linkApp(ctx) != "" {
			writeErr(w, http.StatusForbidden, "other apps may write down a print made, but not start a printer")
			return
		}
		var in startIn
		if err := json.Unmarshal(b, &in); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		pr, err := s.startPrint(ctx, uid, in)
		if err != nil {
			s.userFail(w, err, "start print")
			return
		}
		writeJSON(w, http.StatusCreated, pr)
		return
	}
	var in struct {
		printEdit
		PrinterID int64   `json:"printer_id"`
		ModelID   int64   `json:"model_id"`
		FileID    int64   `json:"file_id"`
		JobID     int64   `json:"job_id"`
		Status    string  `json:"status"`
		StartedAt string  `json:"started_at"` // local "2026-11-10T14:30" or RFC 3339
		Hours     float64 `json:"hours"`
	}
	if err := json.Unmarshal(b, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	pr := &store.Print{UserID: uid, Status: firstNonEmpty(in.Status, "done"), Source: sourceOf(ctx), Copies: 1}
	if !oneOf(pr.Status, []string{"done", "failed", "cancelled"}) {
		writeErr(w, http.StatusBadRequest, "a print made is done, failed or cancelled")
		return
	}
	start, err := parseWhen(in.StartedAt)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "when it started is YYYY-MM-DDTHH:MM")
		return
	}
	if in.PrinterID > 0 {
		if _, err := s.store.GetPrinter(ctx, uid, in.PrinterID); err != nil {
			writeErr(w, http.StatusBadRequest, "choose one of your printers")
			return
		}
		pr.PrinterID = &in.PrinterID
	}
	if in.FileID > 0 {
		f, err := s.store.GetFile(ctx, uid, in.FileID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "that file is not in the library")
			return
		}
		pr.FileID, pr.FileName, pr.EstSeconds, pr.EstGrams, pr.Settings, in.ModelID = &f.ID, f.Name, f.EstSeconds, f.EstGrams, f.Settings, f.ModelID
	}
	if in.ModelID > 0 {
		m, err := s.store.GetModel(ctx, uid, in.ModelID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "that model is not in the library")
			return
		}
		pr.ModelID, pr.Title = &m.ID, m.Name
	}
	if in.JobID > 0 {
		if j, err := s.store.GetJob(ctx, uid, in.JobID); err == nil {
			pr.JobID, pr.Title = &j.ID, j.Title
		}
	}
	if in.Title != nil {
		pr.Title = cleanText(*in.Title, maxName)
	}
	if pr.Title == "" {
		writeErr(w, http.StatusBadRequest, "what was printed?")
		return
	}
	pr.Seconds = int(in.Hours * 3600)
	if in.Seconds != nil {
		pr.Seconds = *in.Seconds
	}
	if pr.Seconds <= 0 {
		pr.Seconds = pr.EstSeconds
	}
	pr.StartedAt = start.UTC().Format(time.RFC3339Nano)
	end := start.Add(time.Duration(pr.Seconds) * time.Second).UTC().Format(time.RFC3339Nano)
	pr.EndedAt = &end
	pr.Progress = 1
	if err := in.printEdit.apply(ctx, s, pr); err != nil {
		s.userFail(w, err, "print")
		return
	}
	if pr.Status == "failed" && pr.FailReason == "" {
		pr.FailReason = "other"
	}
	if pr.Grams == 0 && pr.Status == "done" {
		pr.Grams = pr.EstGrams
	}
	st := s.settingsOf(ctx, uid)
	var printer *store.Printer
	if pr.PrinterID != nil {
		printer, _ = s.store.GetPrinter(ctx, uid, *pr.PrinterID)
	}
	var spool *store.Spool
	if pr.SpoolID != nil {
		spool, _ = s.store.GetSpool(ctx, uid, *pr.SpoolID)
	}
	s.workOutCost(pr, printer, spool, st)
	if err := s.store.CreatePrint(ctx, pr); err != nil {
		s.fail(w, err, "print")
		return
	}
	if spool != nil && pr.Grams > 0 && st.AutoDeduct {
		_ = s.store.UseSpool(ctx, uid, spool.ID, pr.Grams, "print", pr.Title, &pr.ID)
		s.checkLow(ctx, uid, spool.ID, st)
	}
	if pr.JobID != nil && pr.Status == "done" {
		if j, err := s.store.GetJob(ctx, uid, *pr.JobID); err == nil && j.Status != "cancelled" {
			j.DoneCount += pr.Copies
			if j.DoneCount >= j.Quantity && j.Status != "done" {
				j.Status, j.FinishedAt = "done", &end
			}
			_ = s.store.UpdateJob(ctx, j)
		}
	}
	out, _ := s.store.GetPrint(ctx, uid, pr.ID)
	writeJSON(w, http.StatusCreated, out)
}

// parseWhen reads a local "YYYY-MM-DDTHH:MM", a day, or RFC 3339 ("" = now).
func parseWhen(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Now(), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t, nil
	}
	for _, f := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(f, v, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad time")
}

// printEdit is what a person can change on a print.
type printEdit struct {
	Title      *string          `json:"title"`
	Notes      *string          `json:"notes"`
	Rating     *int             `json:"rating"`
	FailReason *string          `json:"fail_reason"`
	FailNote   *string          `json:"fail_note"`
	Settings   *json.RawMessage `json:"settings"`
	Keep       *bool            `json:"keep"`
	Grams      *float64         `json:"grams"`
	Seconds    *int             `json:"seconds"`
	Copies     *int             `json:"copies"`
	SpoolID    *int64           `json:"spool_id"`
}

func (in printEdit) apply(ctx context.Context, s *Server, p *store.Print) error {
	if in.Title != nil {
		if t := cleanText(*in.Title, maxName); t != "" {
			p.Title = t
		}
	}
	if in.Notes != nil {
		p.Notes = cleanText(*in.Notes, maxNotes)
	}
	if in.Rating != nil {
		p.Rating = clampInt(*in.Rating, 0, 5)
	}
	if in.FailReason != nil {
		if *in.FailReason != "" && !oneOf(*in.FailReason, failReasons) {
			return &userErr{"unknown reason"}
		}
		p.FailReason = *in.FailReason
	}
	if in.FailNote != nil {
		p.FailNote = cleanText(*in.FailNote, maxLine)
	}
	if in.Settings != nil {
		var m map[string]any
		if err := json.Unmarshal(*in.Settings, &m); err != nil {
			return &userErr{"the settings are a list of names and values"}
		}
		clean := map[string]string{}
		for k, v := range m {
			k = clip(strings.TrimSpace(k), 40)
			sv := clip(strings.TrimSpace(fmt.Sprint(v)), 120)
			if k != "" && sv != "" && v != nil {
				clean[k] = sv
			}
			if len(clean) >= 40 {
				break
			}
		}
		p.Settings = settingsJSON(clean)
	}
	if in.Keep != nil {
		p.Keep = *in.Keep
	}
	if in.Grams != nil {
		if *in.Grams < 0 || *in.Grams > 100000 {
			return &userErr{"grams are 0 to 100 000"}
		}
		p.Grams = *in.Grams
	}
	if in.Seconds != nil {
		if *in.Seconds < 0 {
			return &userErr{"a time is not negative"}
		}
		p.Seconds = *in.Seconds
	}
	if in.Copies != nil {
		p.Copies = clampInt(*in.Copies, 1, 1000)
	}
	if in.SpoolID != nil {
		if *in.SpoolID == 0 {
			p.SpoolID = nil
		} else if _, err := s.store.GetSpool(ctx, p.UserID, *in.SpoolID); err != nil {
			return &userErr{"that spool is not yours"}
		} else {
			id := *in.SpoolID
			p.SpoolID = &id
		}
	}
	return nil
}

func (s *Server) handleGetPrint(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := s.printOf(w, r)
	if !ok {
		return
	}
	out := map[string]any{"print": p}
	if p.ModelID != nil {
		if m, err := s.store.GetModel(ctx, p.UserID, *p.ModelID); err == nil {
			out["model"] = m
		}
	}
	if p.JobID != nil {
		if j, err := s.store.GetJob(ctx, p.UserID, *p.JobID); err == nil {
			out["job"] = j
		}
	}
	st := s.settingsOf(ctx, p.UserID)
	out["price"] = suggestPrice(p.Cost, p.Copies, st.Markup)
	out["currency"] = st.Currency
	writeJSON(w, http.StatusOK, out)
}

// suggestPrice is a price for one piece: the cost with the markup on top.
func suggestPrice(cost, copies, markup int) int {
	if copies <= 0 {
		copies = 1
	}
	return (cost*(100+markup) + 50*copies) / (100 * copies)
}

func (s *Server) handleUpdatePrint(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := s.printOf(w, r)
	if !ok {
		return
	}
	if !mayChange(ctx, p.Source) {
		writeErr(w, http.StatusForbidden, "this print was not added by that app")
		return
	}
	var in printEdit
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	oldGrams, oldSpool := p.Grams, p.SpoolID
	if err := in.apply(ctx, s, p); err != nil {
		s.userFail(w, err, "print")
		return
	}
	ended := p.Status != "printing" && p.Status != "paused"
	st := s.settingsOf(ctx, p.UserID)
	if ended && (in.Grams != nil || in.Seconds != nil || in.SpoolID != nil) {
		var printer *store.Printer
		if p.PrinterID != nil {
			printer, _ = s.store.GetPrinter(ctx, p.UserID, *p.PrinterID)
		}
		var spool *store.Spool
		if p.SpoolID != nil {
			spool, _ = s.store.GetSpool(ctx, p.UserID, *p.SpoolID)
		}
		s.workOutCost(p, printer, spool, st)
	}
	if err := s.store.UpdatePrint(ctx, p); err != nil {
		s.fail(w, err, "save print")
		return
	}
	// the grams moved: put the old ones back, take the new ones off
	if ended && st.AutoDeduct && (p.Grams != oldGrams || !sameID(oldSpool, p.SpoolID)) {
		if oldSpool != nil {
			_ = s.store.UseSpool(ctx, p.UserID, *oldSpool, 0, "print", p.Title, &p.ID)
		}
		if p.SpoolID != nil && p.Grams > 0 {
			_ = s.store.UseSpool(ctx, p.UserID, *p.SpoolID, p.Grams, "print", p.Title, &p.ID)
		}
	}
	out, _ := s.store.GetPrint(ctx, p.UserID, p.ID)
	writeJSON(w, http.StatusOK, out)
}

func sameID(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// handleFinishPrint ends a print by hand: done, failed or cancelled.
func (s *Server) handleFinishPrint(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := s.printOf(w, r)
	if !ok {
		return
	}
	var in struct {
		finishIn
		Status string `json:"status"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.finishPrint(ctx, p, firstNonEmpty(in.Status, "done"), in.finishIn); err != nil {
		s.userFail(w, err, "finish print")
		return
	}
	out, _ := s.store.GetPrint(ctx, p.UserID, p.ID)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeletePrint(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := s.printOf(w, r)
	if !ok {
		return
	}
	if !mayChange(ctx, p.Source) {
		writeErr(w, http.StatusForbidden, "this print was not added by that app")
		return
	}
	if err := s.store.DeletePrint(ctx, p.UserID, p.ID); err != nil {
		s.fail(w, err, "delete print")
		return
	}
	s.dropMedia(p.Photo)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePrintPhoto(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := s.printOf(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		if p.Photo == "" {
			writeErr(w, http.StatusNotFound, "no photo")
			return
		}
		s.serveMediaFile(w, r, p.Photo, "", "")
	case http.MethodDelete:
		s.dropMedia(p.Photo)
		p.Photo = ""
		_ = s.store.UpdatePrint(ctx, p)
		w.WriteHeader(http.StatusNoContent)
	default:
		b, err := s.readPicture(r)
		if err != nil {
			s.userFail(w, err, "photo")
			return
		}
		rel, err := s.saveBytes(p.UserID, imageExt(b), b)
		if err != nil {
			s.fail(w, err, "photo")
			return
		}
		old := p.Photo
		p.Photo = rel
		if err := s.store.UpdatePrint(ctx, p); err != nil {
			s.fail(w, err, "photo")
			return
		}
		s.dropMedia(old)
		out, _ := s.store.GetPrint(ctx, p.UserID, p.ID)
		writeJSON(w, http.StatusOK, out)
	}
}

// ---- the queue ----

type jobIn struct {
	Title      *string `json:"title"`
	ModelID    *int64  `json:"model_id"`
	FileID     *int64  `json:"file_id"`
	PrinterID  *int64  `json:"printer_id"`
	FilamentID *int64  `json:"filament_id"`
	Material   *string `json:"material"`
	Quantity   *int    `json:"quantity"`
	DoneCount  *int    `json:"done_count"`
	Status     *string `json:"status"`
	DueOn      *string `json:"due_on"`
	ForWhom    *string `json:"for_whom"`
	ContactRef *string `json:"contact_ref"`
	Price      *int    `json:"price"`
	Notes      *string `json:"notes"`
}

func (s *Server) applyJob(ctx context.Context, j *store.Job, in jobIn) error {
	uid := j.UserID
	ref := func(v *int64, check func(int64) error, dst **int64) error {
		if v == nil {
			return nil
		}
		if *v == 0 {
			*dst = nil
			return nil
		}
		if err := check(*v); err != nil {
			return err
		}
		id := *v
		*dst = &id
		return nil
	}
	if err := ref(in.ModelID, func(id int64) error {
		m, err := s.store.GetModel(ctx, uid, id)
		if err != nil {
			return &userErr{"that model is not in the library"}
		}
		if j.Title == "" && in.Title == nil {
			j.Title = m.Name
		}
		return nil
	}, &j.ModelID); err != nil {
		return err
	}
	if err := ref(in.FileID, func(id int64) error {
		f, err := s.store.GetFile(ctx, uid, id)
		if err != nil {
			return &userErr{"that file is not in the library"}
		}
		mid := f.ModelID
		j.ModelID = &mid
		if j.Material == "" && in.Material == nil {
			j.Material = f.Material
		}
		if j.Title == "" && in.Title == nil {
			if m, err := s.store.GetModel(ctx, uid, mid); err == nil {
				j.Title = m.Name
			}
		}
		return nil
	}, &j.FileID); err != nil {
		return err
	}
	if err := ref(in.PrinterID, func(id int64) error {
		_, err := s.store.GetPrinter(ctx, uid, id)
		if err != nil {
			return &userErr{"choose one of your printers"}
		}
		return nil
	}, &j.PrinterID); err != nil {
		return err
	}
	if err := ref(in.FilamentID, func(id int64) error {
		f, err := s.store.GetFilament(ctx, uid, id)
		if err != nil {
			return &userErr{"choose one of your filaments"}
		}
		j.Material = f.Material
		return nil
	}, &j.FilamentID); err != nil {
		return err
	}
	// a model without a file chosen: its newest G-code
	if j.ModelID != nil && j.FileID == nil && in.FileID == nil {
		if files, err := s.store.ListFiles(ctx, uid, *j.ModelID); err == nil {
			for i := len(files) - 1; i >= 0; i-- {
				if files[i].Printable {
					fid := files[i].ID
					j.FileID = &fid
					if j.Material == "" && in.Material == nil {
						j.Material = files[i].Material
					}
					break
				}
			}
		}
	}
	if in.Title != nil {
		j.Title = cleanText(*in.Title, maxName)
	}
	if in.Material != nil {
		j.Material = clip(strings.TrimSpace(*in.Material), 20)
	}
	if in.Quantity != nil {
		j.Quantity = clampInt(*in.Quantity, 1, 10000)
	}
	if in.DoneCount != nil {
		j.DoneCount = clampInt(*in.DoneCount, 0, 10000)
	}
	if in.Status != nil {
		if !oneOf(*in.Status, []string{"queued", "done", "cancelled"}) {
			return &userErr{"a job is queued, done or cancelled"}
		}
		if *in.Status != j.Status {
			j.Status = *in.Status
			if j.Status == "queued" {
				j.FinishedAt = nil
			} else {
				now := store.Now()
				j.FinishedAt = &now
			}
		}
	}
	if in.DueOn != nil {
		if !validDate(*in.DueOn) {
			return &userErr{"a day is YYYY-MM-DD"}
		}
		j.DueOn = *in.DueOn
	}
	trimP(&j.ForWhom, in.ForWhom)
	trimP(&j.ContactRef, in.ContactRef)
	j.ForWhom = clip(j.ForWhom, maxName)
	if in.Price != nil {
		if *in.Price < 0 {
			return &userErr{"a price is not negative"}
		}
		j.Price = *in.Price
	}
	if in.Notes != nil {
		j.Notes = cleanText(*in.Notes, maxNotes)
	}
	if j.Title == "" {
		return &userErr{"what is to be printed? Choose a model or give the job a name"}
	}
	return nil
}

func (s *Server) jobOf(w http.ResponseWriter, r *http.Request) (*store.Job, bool) {
	j, err := s.store.GetJob(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "job")
		return nil, false
	}
	return j, true
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var statuses []string
	switch v := r.URL.Query().Get("status"); v {
	case "", "open":
		statuses = []string{"queued", "printing"}
	case "all":
	default:
		statuses = strings.Split(v, ",")
	}
	list, err := s.store.ListJobs(ctx, actorID(ctx), statuses...)
	if err != nil {
		s.fail(w, err, "queue")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": list})
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in jobIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	j := &store.Job{UserID: actorID(ctx), Quantity: 1, Status: "queued", Source: sourceOf(ctx)}
	if in.Status != nil && *in.Status != "queued" {
		writeErr(w, http.StatusBadRequest, "a new job is queued")
		return
	}
	if err := s.applyJob(ctx, j, in); err != nil {
		s.userFail(w, err, "job")
		return
	}
	if err := s.store.CreateJob(ctx, j); err != nil {
		s.fail(w, err, "add job")
		return
	}
	s.publish(ctx, j.UserID, "printfarm.job_added", fmt.Sprint(j.ID), map[string]any{"job_id": j.ID, "title": j.Title, "quantity": j.Quantity, "for": j.ForWhom, "due_on": j.DueOn, "url": s.link("/queue")})
	out, _ := s.store.GetJob(ctx, j.UserID, j.ID)
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	j, ok := s.jobOf(w, r)
	if !ok {
		return
	}
	prints, _, _ := s.store.ListPrints(ctx, j.UserID, store.PrintFilter{JobID: j.ID, Limit: 200})
	writeJSON(w, http.StatusOK, map[string]any{"job": j, "prints": prints})
}

func (s *Server) handleUpdateJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	j, ok := s.jobOf(w, r)
	if !ok {
		return
	}
	if !mayChange(ctx, j.Source) {
		writeErr(w, http.StatusForbidden, "this job was not added by that app")
		return
	}
	var in jobIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.applyJob(ctx, j, in); err != nil {
		s.userFail(w, err, "job")
		return
	}
	if err := s.store.UpdateJob(ctx, j); err != nil {
		s.fail(w, err, "save job")
		return
	}
	out, _ := s.store.GetJob(ctx, j.UserID, j.ID)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	j, ok := s.jobOf(w, r)
	if !ok {
		return
	}
	if !mayChange(ctx, j.Source) {
		writeErr(w, http.StatusForbidden, "this job was not added by that app")
		return
	}
	if err := s.store.DeleteJob(ctx, j.UserID, j.ID); err != nil {
		s.fail(w, err, "delete job")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOrderJobs(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.store.ReorderJobs(r.Context(), actorID(r.Context()), in.IDs); err != nil {
		s.fail(w, err, "order")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleStartJob starts a job on a printer.
func (s *Server) handleStartJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	j, ok := s.jobOf(w, r)
	if !ok {
		return
	}
	var in startIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.JobID = j.ID
	if in.PrinterID == 0 && j.PrinterID != nil {
		in.PrinterID = *j.PrinterID
	}
	if in.Copies == 0 {
		in.Copies = 1
	}
	pr, err := s.startPrint(ctx, j.UserID, in)
	if err != nil {
		s.userFail(w, err, "start")
		return
	}
	writeJSON(w, http.StatusCreated, pr)
}
