package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/store"
)

// Pocket AI helps in two places: what probably went wrong with a failed
// print (and what to change next time), and which settings to start from
// for a model, from the person's own prints that worked. It never changes
// anything by itself: the person reads it and decides.

func (s *Server) aiOf(w http.ResponseWriter, r *http.Request) (*ai.Client, bool) {
	ctx := r.Context()
	st := s.settingsOf(ctx, actorID(ctx))
	if !st.AIHelp {
		writeErr(w, http.StatusBadRequest, "AI help is switched off in Settings → AI")
		return nil, false
	}
	cl, _, why := s.aiFor(ctx, actorID(ctx), st)
	if cl == nil {
		writeErr(w, http.StatusBadRequest, firstNonEmpty(why, "no AI is set up (Settings → AI)"))
		return nil, false
	}
	select {
	case s.aiSem <- struct{}{}:
		return cl, true
	default:
		writeErr(w, http.StatusTooManyRequests, "the AI is busy: try again in a moment")
		return nil, false
	}
}

func (s *Server) aiDone() { <-s.aiSem }

const diagnoseSystem = `You help someone with a home 3D printer (FDM, sometimes resin) understand why a print failed.
You get what they wrote down: the printer, the filament, the slicer settings, how far it got, the reason they picked and their own words, and how their earlier prints of the same thing and on the same printer went.
Give the two to four most likely causes, most likely first, each with what to check and what to change next time — concrete (a temperature, a speed, a setting name as the slicers call it, cleaning the bed with dish soap, drying the filament at a temperature for some hours).
Use plain words for someone who is not an expert. Do not invent facts about their printer that are not given. If the information is too thin, say what to look at.
Answer JSON: {"summary": "one sentence", "causes": [{"cause": "…", "check": "…", "change": "…"}], "next_try": ["short setting changes for the next try"]}.`

// handleAIDiagnose: why did this print fail?
func (s *Server) handleAIDiagnose(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := s.printOf(w, r)
	if !ok {
		return
	}
	cl, ok := s.aiOf(w, r)
	if !ok {
		return
	}
	defer s.aiDone()
	var b strings.Builder
	fmt.Fprintf(&b, "Print: %s\nStatus: %s at %d%%\n", p.Title, p.Status, int(p.Progress*100))
	if p.FailReason != "" {
		fmt.Fprintf(&b, "Reason picked: %s\n", strings.ReplaceAll(p.FailReason, "_", " "))
	}
	if p.FailNote != "" || p.Notes != "" {
		fmt.Fprintf(&b, "Their words: %s %s\n", p.FailNote, p.Notes)
	}
	if p.PrinterID != nil {
		if pr, err := s.store.GetPrinter(ctx, p.UserID, *p.PrinterID); err == nil {
			fmt.Fprintf(&b, "Printer: %s %s, nozzle %.2f mm, %.0f print hours\n", pr.Name, pr.Model, pr.Nozzle, pr.Hours)
			ups, _ := s.store.ListUpkeepLog(ctx, p.UserID, pr.ID, 5)
			for _, u := range ups {
				fmt.Fprintf(&b, "  upkeep done %s: %s\n", u.DoneOn, u.Title)
			}
			recent, _, _ := s.store.ListPrints(ctx, p.UserID, store.PrintFilter{PrinterID: pr.ID, Limit: 12})
			for _, x := range recent {
				if x.ID != p.ID {
					fmt.Fprintf(&b, "  earlier on this printer: %s — %s %s %s\n", x.Title, x.Status, x.FailReason, x.Material)
				}
			}
		}
	}
	if p.SpoolID != nil {
		if sp, err := s.store.GetSpool(ctx, p.UserID, *p.SpoolID); err == nil {
			fmt.Fprintf(&b, "Filament: %s, opened %s, last dried %s\n", sp.Label(), firstNonEmpty(sp.OpenedOn, "unknown"), firstNonEmpty(sp.DriedOn, "never written down"))
		}
	}
	if len(p.Settings) > 2 {
		fmt.Fprintf(&b, "Slicer settings: %s\n", string(p.Settings))
	}
	if p.ModelID != nil {
		others, _, _ := s.store.ListPrints(ctx, p.UserID, store.PrintFilter{ModelID: *p.ModelID, Limit: 10})
		for _, x := range others {
			if x.ID != p.ID {
				fmt.Fprintf(&b, "  this model before: %s on %s — %s %s settings %s\n", localDay(x.StartedAt), x.Printer, x.Status, x.FailReason, string(x.Settings))
			}
		}
	}
	var out struct {
		Summary string `json:"summary"`
		Causes  []struct {
			Cause  string `json:"cause"`
			Check  string `json:"check"`
			Change string `json:"change"`
		} `json:"causes"`
		NextTry []string `json:"next_try"`
	}
	if err := cl.JSON(ctx, diagnoseSystem, clip(b.String(), 20000), &out); err != nil {
		writeErr(w, http.StatusBadGateway, "the AI did not answer: "+err.Error())
		return
	}
	if len(out.Causes) > 4 {
		out.Causes = out.Causes[:4]
	}
	writeJSON(w, http.StatusOK, out)
}

const settingsSystem = `You help someone choose slicer settings for a 3D print, starting from THEIR OWN prints: the ones that came out well, the ones that failed, the printer and the filament.
Suggest a starting point: layer height, walls, infill, supports, brim, nozzle and bed temperature, and anything the failures teach. Say which earlier print each choice comes from when it does.
Use plain words. Do not invent facts about their printer. Answer JSON: {"summary": "one sentence", "settings": {"layer_height": "0.2 mm", "…": "…"}, "why": ["short reasons"]}.`

// handleAISettings: what settings to start from for a model.
func (s *Server) handleAISettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	m, ok := s.modelOf(w, r)
	if !ok {
		return
	}
	var in struct {
		PrinterID int64  `json:"printer_id"`
		Material  string `json:"material"`
		Ask       string `json:"ask"`
	}
	_ = decode(r, &in)
	cl, ok := s.aiOf(w, r)
	if !ok {
		return
	}
	defer s.aiDone()
	var b strings.Builder
	fmt.Fprintf(&b, "Model: %s\n%s\nTags: %s\n", m.Name, clip(m.Description, 1500), m.Tags)
	if m.SizeX > 0 {
		fmt.Fprintf(&b, "Size: %.0f × %.0f × %.0f mm\n", m.SizeX, m.SizeY, m.SizeZ)
	}
	files, _ := s.store.ListFiles(ctx, m.UserID, m.ID)
	for _, f := range files {
		fmt.Fprintf(&b, "File %s (%s): layer %.2f, nozzle %.2f, %s, %s\n", f.Name, f.Kind, f.LayerHeight, f.Nozzle, f.Material, string(f.Settings))
	}
	prints, _, _ := s.store.ListPrints(ctx, m.UserID, store.PrintFilter{ModelID: m.ID, Limit: 20})
	for _, x := range prints {
		fmt.Fprintf(&b, "Printed %s on %s with %s: %s %s rating %d settings %s\n", localDay(x.StartedAt), x.Printer, x.Spool, x.Status, x.FailReason, x.Rating, string(x.Settings))
	}
	// what worked on other models with the same material
	kept, _, _ := s.store.ListPrints(ctx, m.UserID, store.PrintFilter{Keep: true, Limit: 15})
	for _, x := range kept {
		if in.Material == "" || strings.EqualFold(x.Material, in.Material) {
			fmt.Fprintf(&b, "Settings that worked on %s (%s, %s): %s\n", x.Title, x.Material, x.Printer, string(x.Settings))
		}
	}
	if in.PrinterID > 0 {
		if p, err := s.store.GetPrinter(ctx, m.UserID, in.PrinterID); err == nil {
			fmt.Fprintf(&b, "Printing on: %s %s, nozzle %.2f mm\n", p.Name, p.Model, p.Nozzle)
		}
	}
	if in.Material != "" {
		fmt.Fprintf(&b, "Material: %s\n", in.Material)
	}
	if t := strings.TrimSpace(in.Ask); t != "" {
		fmt.Fprintf(&b, "They say: %s\n", clip(t, 1000))
	}
	var out struct {
		Summary  string          `json:"summary"`
		Settings json.RawMessage `json:"settings"`
		Why      []string        `json:"why"`
	}
	if err := cl.JSON(ctx, settingsSystem, clip(b.String(), 20000), &out); err != nil {
		writeErr(w, http.StatusBadGateway, "the AI did not answer: "+err.Error())
		return
	}
	set := map[string]string{}
	var raw map[string]any
	if json.Unmarshal(out.Settings, &raw) == nil {
		for k, v := range raw {
			if len(set) < 20 {
				set[clip(k, 40)] = clip(fmt.Sprint(v), 120)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"summary": out.Summary, "settings": set, "why": out.Why})
}
