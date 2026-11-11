package server

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/store"
)

// ---- upkeep: what printers need every so many print hours or days ----

// upkeepWithDue works out when each item is due against its printer's hours.
func (s *Server) upkeepWithDue(ctx context.Context, uid, printerID int64, list []store.Printer) []store.Upkeep {
	items, _ := s.store.ListUpkeep(ctx, uid, printerID)
	hours := map[int64]float64{}
	for _, p := range list {
		hours[p.ID] = p.Hours
	}
	today := time.Now().Format("2006-01-02")
	soon := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	for i := range items {
		u := &items[i]
		if u.EveryHours > 0 {
			left := math.Round((u.LastHours+u.EveryHours-hours[u.PrinterID])*10) / 10
			u.DueHours = &left
			if left <= 0 {
				u.Due = true
			} else if left <= math.Max(5, u.EveryHours*0.1) {
				u.Soon = true
			}
		}
		if u.EveryDays > 0 {
			from := u.LastDoneOn
			if from == "" {
				from = localDay(u.CreatedAt)
			}
			u.DueOn = store.AddDays(from, u.EveryDays)
			if u.DueOn <= today {
				u.Due = true
			} else if u.DueOn <= soon {
				u.Soon = true
			}
		}
		if u.Due {
			u.Soon = false
		}
	}
	return items
}

type upkeepIn struct {
	PrinterID  *int64   `json:"printer_id"`
	Title      *string  `json:"title"`
	EveryHours *float64 `json:"every_hours"`
	EveryDays  *int     `json:"every_days"`
	LastDoneOn *string  `json:"last_done_on"`
	LastHours  *float64 `json:"last_hours"`
	Notes      *string  `json:"notes"`
}

func (s *Server) applyUpkeep(ctx context.Context, u *store.Upkeep, in upkeepIn) error {
	if in.PrinterID != nil {
		p, err := s.store.GetPrinter(ctx, u.UserID, *in.PrinterID)
		if err != nil {
			return &userErr{"choose one of your printers"}
		}
		u.PrinterID = p.ID
		if u.ID == 0 && in.LastHours == nil {
			u.LastHours = math.Round(p.Hours*10) / 10
		}
	}
	if in.Title != nil {
		u.Title = cleanText(*in.Title, maxName)
	}
	if in.EveryHours != nil {
		u.EveryHours = math.Max(0, *in.EveryHours)
	}
	if in.EveryDays != nil {
		u.EveryDays = clampInt(*in.EveryDays, 0, 3650)
	}
	if in.LastDoneOn != nil {
		if !validDate(*in.LastDoneOn) {
			return &userErr{"a day is YYYY-MM-DD"}
		}
		u.LastDoneOn = *in.LastDoneOn
	}
	if in.LastHours != nil {
		u.LastHours = math.Max(0, *in.LastHours)
	}
	if in.Notes != nil {
		u.Notes = cleanText(*in.Notes, maxNotes)
	}
	if u.Title == "" {
		return &userErr{"what needs doing? (\"Clean the nozzle\")"}
	}
	if u.PrinterID == 0 {
		return &userErr{"which printer?"}
	}
	if u.EveryHours == 0 && u.EveryDays == 0 {
		return &userErr{"say how often: every so many print hours, or days"}
	}
	return nil
}

func (s *Server) handleUpkeep(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	list, _ := s.store.ListPrinters(ctx, uid, false)
	items := s.upkeepWithDue(ctx, uid, int64(queryInt(r, "printer")), list)
	log, _ := s.store.ListUpkeepLog(ctx, uid, int64(queryInt(r, "printer")), 100)
	writeJSON(w, http.StatusOK, map[string]any{"upkeep": items, "log": log})
}

func (s *Server) handleCreateUpkeep(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in upkeepIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	u := &store.Upkeep{UserID: actorID(ctx)}
	if err := s.applyUpkeep(ctx, u, in); err != nil {
		s.userFail(w, err, "upkeep")
		return
	}
	if u.LastDoneOn == "" {
		u.LastDoneOn = store.Today()
	}
	if err := s.store.CreateUpkeep(ctx, u); err != nil {
		s.fail(w, err, "add upkeep")
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) handleUpdateUpkeep(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, err := s.store.GetUpkeep(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "upkeep")
		return
	}
	var in upkeepIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.applyUpkeep(ctx, u, in); err != nil {
		s.userFail(w, err, "upkeep")
		return
	}
	if err := s.store.UpdateUpkeep(ctx, u); err != nil {
		s.fail(w, err, "save upkeep")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleDeleteUpkeep(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.DeleteUpkeep(ctx, actorID(ctx), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete upkeep")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUpkeepDone writes upkeep done (or a repair, with no upkeep item)
// into the printer's log book; the item starts counting again.
func (s *Server) handleUpkeepDone(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		UpkeepID  int64  `json:"upkeep_id"`
		PrinterID int64  `json:"printer_id"`
		Title     string `json:"title"`
		DoneOn    string `json:"done_on"`
		Cost      int    `json:"cost"`
		Note      string `json:"note"`
		ToHome    bool   `json:"to_home"` // also in Pocket Home's log for the appliance
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !validDate(in.DoneOn) {
		writeErr(w, http.StatusBadRequest, "a day is YYYY-MM-DD")
		return
	}
	l := &store.UpkeepLog{PrinterID: in.PrinterID, Title: cleanText(in.Title, maxName), DoneOn: firstNonEmpty(in.DoneOn, store.Today()), Cost: max(0, in.Cost), Note: cleanText(in.Note, maxNotes)}
	var u *store.Upkeep
	if in.UpkeepID > 0 {
		var err error
		if u, err = s.store.GetUpkeep(ctx, uid, in.UpkeepID); err != nil {
			s.fail(w, err, "upkeep")
			return
		}
		l.UpkeepID, l.PrinterID = &u.ID, u.PrinterID
		l.Title = firstNonEmpty(l.Title, u.Title)
	}
	p, err := s.store.GetPrinter(ctx, uid, l.PrinterID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "which printer?")
		return
	}
	if l.Title == "" {
		writeErr(w, http.StatusBadRequest, "what was done?")
		return
	}
	l.Hours = math.Round(p.Hours*10) / 10
	if err := s.store.CreateUpkeepLog(ctx, uid, l); err != nil {
		s.fail(w, err, "log")
		return
	}
	out := map[string]any{"log": l}
	if u != nil {
		u.LastDoneOn, u.LastHours, u.ToldAt = l.DoneOn, l.Hours, ""
		taskRef := u.TaskRef
		u.TaskRef = ""
		_ = s.store.UpdateUpkeep(ctx, u)
		if taskRef != "" {
			bg := ctxDetached(ctx)
			go s.completeTask(bg, uid, taskRef)
		}
	}
	if in.ToHome && p.HomeRef != "" {
		if link, err := s.upkeepToHome(ctx, uid, p, l); err == nil {
			out["home_url"] = link
		} else {
			out["home_error"] = plainErr(appHome, err)
		}
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleDeleteUpkeepLog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.DeleteUpkeepLog(ctx, actorID(ctx), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// upkeepReminders tells each person, at their hour, about upkeep that is
// due (once per item until it is done), and makes a task for it.
func (s *Server) upkeepReminders(ctx context.Context) {
	users, err := s.store.UpkeepUsers(ctx)
	if err != nil {
		return
	}
	now := time.Now()
	for _, uid := range users {
		st := s.settingsOf(ctx, uid)
		if now.Hour() < st.NotifyHour {
			continue
		}
		list, _ := s.store.ListPrinters(ctx, uid, false)
		var due []string
		for _, u := range s.upkeepWithDue(ctx, uid, 0, list) {
			if !u.Due || u.ToldAt != "" {
				continue
			}
			u := u
			u.ToldAt = store.Now()
			if st.TasksUpkeep && u.TaskRef == "" {
				if ref, err := s.upkeepTask(ctx, uid, &u); err == nil {
					u.TaskRef = ref
				}
			}
			_ = s.store.UpdateUpkeep(ctx, &u)
			due = append(due, u.Printer+": "+u.Title)
			s.publish(ctx, uid, "printfarm.upkeep_due", fmt.Sprint(u.ID), map[string]any{"upkeep_id": u.ID, "title": u.Title, "printer": u.Printer, "url": s.link(fmt.Sprintf("/printers/%d", u.PrinterID))})
		}
		if len(due) > 0 && st.NotifyUpkeep {
			s.tell(ctx, uid, st, "🔧 Printer upkeep is due", strings.Join(due, "\n"), "/upkeep", "default")
		}
	}
}
