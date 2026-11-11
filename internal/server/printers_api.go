package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/printers"
	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/store"
)

// ---- the farm ----

// handleFarm is the farm at a glance: every printer as it is now, what is
// next in the queue, and what needs the person.
func (s *Server) handleFarm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	views := s.farmView(ctx, uid)
	queued, _ := s.store.ListJobs(ctx, uid, "queued", "printing")
	counts := map[string]int{}
	for _, v := range views {
		counts[v.State]++
	}
	writeJSON(w, http.StatusOK, map[string]any{"printers": views, "queue": firstN(queued, 12), "queued": len(queued), "counts": counts, "at": store.Now(), "poll": s.cfg.PollSeconds})
}

func firstN[T any](v []T, n int) []T {
	if len(v) > n {
		return v[:n]
	}
	return v
}

// ---- printers ----

type printerIn struct {
	Name        *string  `json:"name"`
	Model       *string  `json:"model"`
	Kind        *string  `json:"kind"`
	Connection  *string  `json:"connection"`
	URL         *string  `json:"url"`
	APIKey      *string  `json:"api_key"`
	WebcamURL   *string  `json:"webcam_url"`
	BedX        *float64 `json:"bed_x"`
	BedY        *float64 `json:"bed_y"`
	BedZ        *float64 `json:"bed_z"`
	Nozzle      *float64 `json:"nozzle"`
	Materials   *string  `json:"materials"`
	Watts       *int     `json:"watts"`
	WearPerHour *int     `json:"wear_per_hour"`
	BoughtOn    *string  `json:"bought_on"`
	Price       *int     `json:"price"`
	StartHours  *float64 `json:"start_hours"`
	Location    *string  `json:"location"`
	Color       *string  `json:"color"`
	Notes       *string  `json:"notes"`
	Archived    *bool    `json:"archived"`
	HomeRef     *string  `json:"home_ref"`
}

func (in printerIn) apply(p *store.Printer) error {
	trimP(&p.Name, in.Name)
	trimP(&p.Model, in.Model)
	trimP(&p.Location, in.Location)
	trimP(&p.Materials, in.Materials)
	trimP(&p.HomeRef, in.HomeRef)
	if in.Notes != nil {
		p.Notes = cleanText(*in.Notes, maxNotes)
	}
	if in.Kind != nil {
		if !oneOf(*in.Kind, []string{"fdm", "resin"}) {
			return &userErr{"a printer is FDM (filament) or resin"}
		}
		p.Kind = *in.Kind
	}
	if in.Connection != nil {
		if !oneOf(*in.Connection, append([]string{"manual"}, printers.Kinds...)) {
			return &userErr{"connect a printer through Moonraker (Klipper), OctoPrint or PrusaLink — or by hand"}
		}
		p.Connection = *in.Connection
	}
	if in.URL != nil {
		v := strings.TrimRight(strings.TrimSpace(*in.URL), "/")
		if v != "" && !strings.Contains(v, "://") {
			v = "http://" + v
		}
		if _, ok := cleanURL(v); !ok {
			return &userErr{"the printer's address is like http://192.168.1.50 or http://voron.local:7125"}
		}
		p.URL = v
	}
	if in.APIKey != nil && !strings.Contains(*in.APIKey, "••") {
		p.APIKey = strings.TrimSpace(*in.APIKey)
	}
	if in.WebcamURL != nil {
		v, ok := cleanURL(*in.WebcamURL)
		if !ok {
			return &userErr{"the camera address starts with http:// or https://"}
		}
		p.WebcamURL = v
	}
	if in.Color != nil {
		if *in.Color != "" && !reColor.MatchString(*in.Color) {
			return &userErr{"a colour is #rrggbb"}
		}
		p.Color = *in.Color
	}
	if in.BoughtOn != nil {
		if !validDate(*in.BoughtOn) {
			return &userErr{"a day is YYYY-MM-DD"}
		}
		p.BoughtOn = *in.BoughtOn
	}
	for _, f := range []struct {
		dst *float64
		v   *float64
		max float64
	}{{&p.BedX, in.BedX, 5000}, {&p.BedY, in.BedY, 5000}, {&p.BedZ, in.BedZ, 5000}, {&p.Nozzle, in.Nozzle, 5}, {&p.StartHours, in.StartHours, 1e6}} {
		if f.v != nil {
			if *f.v < 0 || *f.v > f.max {
				return &userErr{"a size, nozzle or number of hours is out of range"}
			}
			*f.dst = *f.v
		}
	}
	for _, f := range []struct {
		dst *int
		v   *int
	}{{&p.Watts, in.Watts}, {&p.WearPerHour, in.WearPerHour}, {&p.Price, in.Price}} {
		if f.v != nil {
			if *f.v < 0 {
				return &userErr{"watts, wear and price are not negative"}
			}
			*f.dst = *f.v
		}
	}
	if in.Archived != nil {
		p.Archived = *in.Archived
	}
	p.Name = clip(p.Name, maxName)
	if p.Name == "" {
		return &userErr{"give the printer a name"}
	}
	if p.Connection != "manual" && p.URL == "" {
		return &userErr{"add the printer's address, or connect it by hand"}
	}
	return nil
}

func (s *Server) printerOf(w http.ResponseWriter, r *http.Request) (*store.Printer, bool) {
	p, err := s.store.GetPrinter(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "printer")
		return nil, false
	}
	return p, true
}

func (s *Server) handlePrinters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.store.ListPrinters(ctx, actorID(ctx), queryBool(r, "archived"))
	if err != nil {
		s.fail(w, err, "printers")
		return
	}
	for i := range list {
		list[i].APIKey = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"printers": list})
}

func (s *Server) handleCreatePrinter(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in printerIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p := &store.Printer{UserID: actorID(ctx), Kind: "fdm", Connection: "manual", Nozzle: 0.4, Source: sourceOf(ctx)}
	if err := in.apply(p); err != nil {
		s.userFail(w, err, "printer")
		return
	}
	if err := s.store.CreatePrinter(ctx, p); err != nil {
		s.fail(w, err, "add printer")
		return
	}
	if connected(p) {
		go s.pollOne(ctxDetached(ctx), p)
	}
	s.printerResponse(w, r, p.ID, http.StatusCreated)
}

func (s *Server) printerResponse(w http.ResponseWriter, r *http.Request, id int64, code int) {
	ctx := r.Context()
	uid := actorID(ctx)
	p, err := s.store.GetPrinter(ctx, uid, id)
	if err != nil {
		s.fail(w, err, "printer")
		return
	}
	all, _ := s.store.ListPrinters(ctx, uid, true)
	upkeep := s.upkeepWithDue(ctx, uid, p.ID, all)
	v := s.viewPrinter(ctx, p, upkeep)
	recent, _, _ := s.store.ListPrints(ctx, uid, store.PrintFilter{PrinterID: p.ID, Limit: 20})
	log, _ := s.store.ListUpkeepLog(ctx, uid, p.ID, 50)
	month := time.Now().AddDate(0, 0, -30).UTC().Format(time.RFC3339Nano)
	last30, _, _ := s.store.ListPrints(ctx, uid, store.PrintFilter{PrinterID: p.ID, From: month, Limit: 1000})
	hours, good, bad := 0.0, 0, 0
	for _, x := range last30 {
		hours += float64(x.Seconds) / 3600
		switch x.Status {
		case "done":
			good++
		case "failed":
			bad++
		}
	}
	writeJSON(w, code, map[string]any{"printer": v, "upkeep": upkeep, "log": log, "prints": recent,
		"month": map[string]any{"hours": round1(hours), "done": good, "failed": bad, "busy": round1(hours / (30 * 24) * 100)}})
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

func (s *Server) handleGetPrinter(w http.ResponseWriter, r *http.Request) {
	s.printerResponse(w, r, idParam(r, "id"), http.StatusOK)
}

func (s *Server) handleUpdatePrinter(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := s.printerOf(w, r)
	if !ok {
		return
	}
	var in printerIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := in.apply(p); err != nil {
		s.userFail(w, err, "printer")
		return
	}
	if err := s.store.UpdatePrinter(ctx, p); err != nil {
		s.fail(w, err, "save printer")
		return
	}
	if connected(p) {
		s.pollOne(ctx, p)
	}
	s.printerResponse(w, r, p.ID, http.StatusOK)
}

func (s *Server) handleDeletePrinter(w http.ResponseWriter, r *http.Request) {
	p, ok := s.printerOf(w, r)
	if !ok {
		return
	}
	if err := s.store.DeletePrinter(r.Context(), p.UserID, p.ID); err != nil {
		s.fail(w, err, "delete printer")
		return
	}
	s.dropMedia(p.Photo)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOrderPrinters(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.store.ReorderPrinters(r.Context(), actorID(r.Context()), in.IDs); err != nil {
		s.fail(w, err, "order")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleTestPrinter asks a printer how it is, with the address typed in the
// form (or the saved one), before or after it is saved.
func (s *Server) handleTestPrinter(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		ID         int64  `json:"id"`
		Connection string `json:"connection"`
		URL        string `json:"url"`
		APIKey     string `json:"api_key"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.ID > 0 {
		p, err := s.store.GetPrinter(ctx, actorID(ctx), in.ID)
		if err != nil {
			s.fail(w, err, "printer")
			return
		}
		in.Connection = firstNonEmpty(in.Connection, p.Connection)
		in.URL = firstNonEmpty(in.URL, p.URL)
		if in.APIKey == "" || strings.Contains(in.APIKey, "••") {
			in.APIKey = p.APIKey
		}
	}
	d, err := printers.New(printers.Config{Kind: in.Connection, URL: in.URL, APIKey: in.APIKey, HTTP: s.farm.http})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	st, err := d.Status(cctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	msg := fmt.Sprintf("%s answered: %s", firstNonEmpty(st.Software, "The printer"), stateWords[st.State])
	if st.Nozzle > 0 {
		msg += fmt.Sprintf(", nozzle %.0f °C, bed %.0f °C", st.Nozzle, st.Bed)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg, "status": st, "camera": st.Webcam != ""})
}

var stateWords = map[string]string{printers.Idle: "ready", printers.Printing: "printing", printers.Paused: "paused", printers.Done: "done",
	printers.Stopped: "stopped", printers.Error: "a problem", printers.Busy: "busy", printers.Offline: "not answering"}

// handleControl: pause, resume or stop the print, say the bed is clear, or ask again.
func (s *Server) handleControl(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := s.printerOf(w, r)
	if !ok {
		return
	}
	var in struct {
		Action string `json:"action"` // pause | resume | cancel | clear | refresh
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	switch in.Action {
	case "clear":
		s.clearBed(ctx, p.ID, false)
		go s.maybeAutoStart(ctxDetached(ctx), p)
	case "refresh":
		if connected(p) {
			s.pollOne(ctx, p)
		}
	case "pause", "resume", "cancel":
		open, err := s.store.OpenPrint(ctx, p.UserID, p.ID)
		if !connected(p) {
			if err != nil {
				writeErr(w, http.StatusBadRequest, "nothing is printing on "+p.Name)
				return
			}
			switch in.Action {
			case "pause":
				open.Status = "paused"
			case "resume":
				open.Status = "printing"
			case "cancel":
				if err := s.finishPrint(ctx, open, "cancelled", finishIn{}); err != nil {
					s.userFail(w, err, "stop")
					return
				}
			}
			if in.Action != "cancel" {
				_ = s.store.UpdatePrint(ctx, open)
			}
			break
		}
		d, err := s.driverOf(p)
		if err != nil {
			s.userFail(w, err, "printer")
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		switch in.Action {
		case "pause":
			err = d.Pause(cctx)
		case "resume":
			err = d.Resume(cctx)
		case "cancel":
			err = d.Cancel(cctx)
		}
		if err != nil {
			writeErr(w, http.StatusBadGateway, p.Name+": "+err.Error())
			return
		}
		time.Sleep(700 * time.Millisecond)
		s.pollOne(ctx, p)
	default:
		writeErr(w, http.StatusBadRequest, "unknown action")
		return
	}
	s.printerResponse(w, r, p.ID, http.StatusOK)
}

// handleLoadSpool puts a spool in a printer (or takes it out).
func (s *Server) handleLoadSpool(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := s.printerOf(w, r)
	if !ok {
		return
	}
	var in struct {
		SpoolID *int64 `json:"spool_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.SpoolID != nil && *in.SpoolID > 0 {
		sp, err := s.store.GetSpool(ctx, p.UserID, *in.SpoolID)
		if err != nil || sp.Archived {
			writeErr(w, http.StatusBadRequest, "that spool is not on the shelf")
			return
		}
		// a spool is in one printer at a time
		all, _ := s.store.ListPrinters(ctx, p.UserID, true)
		for i := range all {
			o := all[i]
			if o.ID != p.ID && o.SpoolID != nil && *o.SpoolID == sp.ID {
				o.SpoolID = nil
				_ = s.store.UpdatePrinter(ctx, &o)
			}
		}
		p.SpoolID = &sp.ID
		if open, err := s.store.OpenPrint(ctx, p.UserID, p.ID); err == nil && open.SpoolID == nil {
			open.SpoolID = &sp.ID
			_ = s.store.UpdatePrint(ctx, open)
		}
	} else {
		p.SpoolID = nil
	}
	if err := s.store.UpdatePrinter(ctx, p); err != nil {
		s.fail(w, err, "load spool")
		return
	}
	s.printerResponse(w, r, p.ID, http.StatusOK)
}

// handleSnapshot is a picture from the printer's camera (cached two seconds).
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := s.printerOf(w, r)
	if !ok {
		return
	}
	s.farm.mu.Lock()
	sn, have := s.farm.snaps[p.ID]
	s.farm.mu.Unlock()
	if !have || time.Since(sn.at) > 2*time.Second {
		addr := p.WebcamURL
		if addr == "" {
			if l, ok := s.farm.get(p.ID); ok {
				addr = l.st.Webcam
			}
		}
		if addr == "" {
			writeErr(w, http.StatusNotFound, "this printer has no camera")
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		key := ""
		if strings.HasPrefix(addr, p.URL) {
			key = p.APIKey
		}
		b, ct, err := printers.Snapshot(cctx, s.farm.http, addr, key)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		sn = snap{b: b, ctype: ct, at: time.Now()}
		s.farm.mu.Lock()
		s.farm.snaps[p.ID] = sn
		s.farm.mu.Unlock()
	}
	w.Header().Set("Content-Type", sn.ctype)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(sn.b)
}

func (s *Server) handlePrinterPhoto(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := s.printerOf(w, r)
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
		_ = s.store.UpdatePrinter(ctx, p)
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
		if err := s.store.UpdatePrinter(ctx, p); err != nil {
			s.fail(w, err, "photo")
			return
		}
		s.dropMedia(old)
		s.printerResponse(w, r, p.ID, http.StatusOK)
	}
}
