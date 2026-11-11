package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/store"
)

// The export is everything a person made, without the model files and
// photos themselves (those are in DATA_DIR/media and in the box's backup)
// and without the printers' API keys. Importing twice changes nothing.

type exportDoc struct {
	App        string            `json:"app"`
	Version    string            `json:"version"`
	ExportedAt string            `json:"exported_at"`
	Printers   []store.Printer   `json:"printers"`
	Filaments  []store.Filament  `json:"filaments"`
	Spools     []store.Spool     `json:"spools"`
	Models     []store.Model     `json:"models"`
	Files      []store.ModelFile `json:"files"`
	Jobs       []store.Job       `json:"jobs"`
	Prints     []store.Print     `json:"prints"`
	Upkeep     []store.Upkeep    `json:"upkeep"`
	UpkeepLog  []store.UpkeepLog `json:"upkeep_log"`
}

func (s *Server) export(ctx context.Context, uid int64) (*exportDoc, error) {
	doc := &exportDoc{App: "pocket-printfarm", Version: Version, ExportedAt: store.Now()}
	var err error
	if doc.Printers, err = s.store.ListPrinters(ctx, uid, true); err != nil {
		return nil, err
	}
	for i := range doc.Printers {
		doc.Printers[i].APIKey = ""
	}
	if doc.Filaments, err = s.store.ListFilaments(ctx, uid); err != nil {
		return nil, err
	}
	if doc.Spools, err = s.store.ListSpools(ctx, uid, store.SpoolFilter{Archived: true}); err != nil {
		return nil, err
	}
	if doc.Models, _, err = s.store.ListModels(ctx, uid, store.ModelFilter{Archived: true, Limit: 1000000}); err != nil {
		return nil, err
	}
	if doc.Files, err = s.store.AllFiles(ctx, uid); err != nil {
		return nil, err
	}
	if doc.Jobs, err = s.store.ListJobs(ctx, uid); err != nil {
		return nil, err
	}
	if doc.Prints, _, err = s.store.ListPrints(ctx, uid, store.PrintFilter{Limit: 1000000}); err != nil {
		return nil, err
	}
	if doc.Upkeep, err = s.store.ListUpkeep(ctx, uid, 0); err != nil {
		return nil, err
	}
	if doc.UpkeepLog, err = s.store.ListUpkeepLog(ctx, uid, 0, 1000000); err != nil {
		return nil, err
	}
	return doc, nil
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	doc, err := s.export(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pocket-printfarm-%s.json"`, time.Now().Format("2006-01-02")))
	_ = json.NewEncoder(w).Encode(doc)
}

// handleExportCSV is one row per print.
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pocket-printfarm-prints-%s.csv"`, time.Now().Format("2006-01-02")))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"started", "ended", "title", "printer", "filament", "status", "fail_reason", "hours", "grams", "copies", "filament_cost", "power_cost", "wear_cost", "rating", "notes"})
	list, _, _ := s.store.ListPrints(ctx, actorID(ctx), store.PrintFilter{Limit: 1000000})
	cents := func(v int) string { return strconv.FormatFloat(float64(v)/100, 'f', 2, 64) }
	for _, p := range list {
		end := ""
		if p.EndedAt != nil {
			end = parseTime(*p.EndedAt).Local().Format("2006-01-02 15:04")
		}
		_ = cw.Write([]string{parseTime(p.StartedAt).Local().Format("2006-01-02 15:04"), end, p.Title, p.Printer, p.Spool, p.Status, p.FailReason,
			strconv.FormatFloat(float64(p.Seconds)/3600, 'f', 2, 64), strconv.FormatFloat(p.Grams, 'f', 1, 64), strconv.Itoa(p.Copies),
			cents(p.FilamentCost), cents(p.PowerCost), cents(p.WearCost), strconv.Itoa(p.Rating), p.Notes})
	}
	cw.Flush()
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var doc exportDoc
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<20)).Decode(&doc); err != nil || doc.App != "pocket-printfarm" {
		writeErr(w, http.StatusBadRequest, "that is not a Pocket Print Farm export")
		return
	}
	res, err := s.importDoc(ctx, actorID(ctx), &doc)
	if err != nil {
		s.userFail(w, err, "import")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func low(v ...string) string { return strings.ToLower(strings.TrimSpace(strings.Join(v, "|"))) }

func mapID(m map[int64]int64, p *int64) *int64 {
	if p == nil {
		return nil
	}
	if v, ok := m[*p]; ok {
		return &v
	}
	return nil
}

// importDoc adds what is not here yet: printers by name, filaments by
// brand, material and colour, spools by where they came from or when they
// were added, models by name, files by their contents, jobs and prints by
// their title and time, upkeep by printer and title.
func (s *Server) importDoc(ctx context.Context, uid int64, doc *exportDoc) (map[string]any, error) {
	added := map[string]int{}
	// printers
	have, _ := s.store.ListPrinters(ctx, uid, true)
	byName := map[string]int64{}
	for _, p := range have {
		byName[low(p.Name)] = p.ID
	}
	pm := map[int64]int64{}
	for _, p := range doc.Printers {
		old := p.ID
		if id, ok := byName[low(p.Name)]; ok {
			pm[old] = id
			continue
		}
		p.UserID, p.SpoolID, p.APIKey, p.Photo = uid, nil, "", ""
		if p.Connection == "" {
			p.Connection = "manual"
		}
		if err := s.store.CreatePrinter(ctx, &p); err != nil {
			return nil, err
		}
		pm[old], byName[low(p.Name)] = p.ID, p.ID
		added["printers"]++
	}
	// filaments
	fhave, _ := s.store.ListFilaments(ctx, uid)
	fkey := map[string]int64{}
	for _, f := range fhave {
		fkey[low(f.Brand, f.Material, f.Name, f.Color)] = f.ID
	}
	fm := map[int64]int64{}
	for _, f := range doc.Filaments {
		old := f.ID
		k := low(f.Brand, f.Material, f.Name, f.Color)
		if id, ok := fkey[k]; ok {
			fm[old] = id
			continue
		}
		f.UserID = uid
		if err := s.store.CreateFilament(ctx, &f); err != nil {
			return nil, err
		}
		fm[old], fkey[k] = f.ID, f.ID
		added["filaments"]++
	}
	// spools
	shave, _ := s.store.ListSpools(ctx, uid, store.SpoolFilter{Archived: true})
	skey := map[string]int64{}
	for _, x := range shave {
		skey[low(fmt.Sprint(x.FilamentID), x.CreatedAt)] = x.ID
		if x.ExternalID != "" {
			skey["ext|"+x.ExternalID] = x.ID
		}
	}
	sm := map[int64]int64{}
	for _, x := range doc.Spools {
		old := x.ID
		fid, ok := fm[x.FilamentID]
		if !ok {
			continue
		}
		if x.ExternalID != "" {
			if id, ok := skey["ext|"+x.ExternalID]; ok {
				sm[old] = id
				continue
			}
		}
		k := low(fmt.Sprint(fid), x.CreatedAt)
		if id, ok := skey[k]; ok {
			sm[old] = id
			continue
		}
		x.UserID, x.FilamentID = uid, fid
		if err := s.store.CreateSpool(ctx, &x); err != nil {
			return nil, err
		}
		_, _ = s.store.DB.ExecContext(ctx, `UPDATE spools SET created_at = ? WHERE id = ?`, x.CreatedAt, x.ID)
		sm[old], skey[k] = x.ID, x.ID
		added["spools"]++
	}
	// loaded spools
	for _, p := range doc.Printers {
		if p.SpoolID == nil {
			continue
		}
		if np, ok := pm[p.ID]; ok {
			if ns := mapID(sm, p.SpoolID); ns != nil {
				if cur, err := s.store.GetPrinter(ctx, uid, np); err == nil && cur.SpoolID == nil {
					cur.SpoolID = ns
					_ = s.store.UpdatePrinter(ctx, cur)
				}
			}
		}
	}
	// models and files (rows only: the files themselves come with the box's backup)
	mhave, _, _ := s.store.ListModels(ctx, uid, store.ModelFilter{Archived: true, Limit: 1000000})
	mkey := map[string]int64{}
	for _, m := range mhave {
		mkey[low(m.Name)] = m.ID
	}
	mm := map[int64]int64{}
	for _, m := range doc.Models {
		old := m.ID
		if id, ok := mkey[low(m.Name)]; ok {
			mm[old] = id
			continue
		}
		m.UserID, m.Cover = uid, ""
		if err := s.store.CreateModel(ctx, &m); err != nil {
			return nil, err
		}
		mm[old], mkey[low(m.Name)] = m.ID, m.ID
		added["models"]++
	}
	fhaveFiles, _ := s.store.AllFiles(ctx, uid)
	shaKey := map[string]int64{}
	for _, f := range fhaveFiles {
		shaKey[low(fmt.Sprint(f.ModelID), f.SHA256, f.Name)] = f.ID
	}
	filem := map[int64]int64{}
	for _, f := range doc.Files {
		old := f.ID
		mid, ok := mm[f.ModelID]
		if !ok {
			continue
		}
		k := low(fmt.Sprint(mid), f.SHA256, f.Name)
		if id, ok := shaKey[k]; ok {
			filem[old] = id
			continue
		}
		f.UserID, f.ModelID, f.Path, f.Thumb = uid, mid, "", ""
		if err := s.store.CreateFile(ctx, &f); err != nil {
			return nil, err
		}
		filem[old], shaKey[k] = f.ID, f.ID
		added["files"]++
	}
	// jobs
	jhave, _ := s.store.ListJobs(ctx, uid)
	jkey := map[string]int64{}
	for _, j := range jhave {
		jkey[low(j.Title, j.CreatedAt)] = j.ID
	}
	jm := map[int64]int64{}
	for _, j := range doc.Jobs {
		old := j.ID
		k := low(j.Title, j.CreatedAt)
		if id, ok := jkey[k]; ok {
			jm[old] = id
			continue
		}
		j.UserID = uid
		j.ModelID, j.FileID, j.PrinterID, j.FilamentID = mapID(mm, j.ModelID), mapID(filem, j.FileID), mapID(pm, j.PrinterID), mapID(fm, j.FilamentID)
		if j.Status == "printing" {
			j.Status = "queued"
		}
		if err := s.store.CreateJob(ctx, &j); err != nil {
			return nil, err
		}
		_, _ = s.store.DB.ExecContext(ctx, `UPDATE jobs SET created_at = ? WHERE id = ?`, j.CreatedAt, j.ID)
		jm[old], jkey[k] = j.ID, j.ID
		added["jobs"]++
	}
	// prints
	phave, _, _ := s.store.ListPrints(ctx, uid, store.PrintFilter{Limit: 1000000})
	pkey := map[string]bool{}
	for _, p := range phave {
		pkey[low(p.Title, p.StartedAt)] = true
	}
	for _, p := range doc.Prints {
		k := low(p.Title, p.StartedAt)
		if pkey[k] {
			continue
		}
		p.UserID, p.Photo = uid, ""
		p.PrinterID, p.JobID, p.ModelID, p.FileID, p.SpoolID = mapID(pm, p.PrinterID), mapID(jm, p.JobID), mapID(mm, p.ModelID), mapID(filem, p.FileID), mapID(sm, p.SpoolID)
		if p.Status == "printing" || p.Status == "paused" {
			p.Status = "cancelled"
		}
		if err := s.store.CreatePrint(ctx, &p); err != nil {
			return nil, err
		}
		pkey[k] = true
		added["prints"]++
	}
	// upkeep and its log
	uhave, _ := s.store.ListUpkeep(ctx, uid, 0)
	ukey := map[string]int64{}
	for _, u := range uhave {
		ukey[low(fmt.Sprint(u.PrinterID), u.Title)] = u.ID
	}
	um := map[int64]int64{}
	for _, u := range doc.Upkeep {
		old := u.ID
		pid, ok := pm[u.PrinterID]
		if !ok {
			continue
		}
		k := low(fmt.Sprint(pid), u.Title)
		if id, ok := ukey[k]; ok {
			um[old] = id
			continue
		}
		u.UserID, u.PrinterID, u.TaskRef = uid, pid, ""
		if err := s.store.CreateUpkeep(ctx, &u); err != nil {
			return nil, err
		}
		um[old], ukey[k] = u.ID, u.ID
		added["upkeep"]++
	}
	lhave, _ := s.store.ListUpkeepLog(ctx, uid, 0, 1000000)
	lkey := map[string]bool{}
	for _, l := range lhave {
		lkey[low(fmt.Sprint(l.PrinterID), l.Title, l.DoneOn)] = true
	}
	for _, l := range doc.UpkeepLog {
		pid, ok := pm[l.PrinterID]
		if !ok {
			continue
		}
		k := low(fmt.Sprint(pid), l.Title, l.DoneOn)
		if lkey[k] {
			continue
		}
		l.PrinterID, l.UpkeepID = pid, mapID(um, l.UpkeepID)
		if err := s.store.CreateUpkeepLog(ctx, uid, &l); err != nil {
			return nil, err
		}
		lkey[k] = true
		added["upkeep_log"]++
	}
	total := 0
	for _, v := range added {
		total += v
	}
	return map[string]any{"added": added, "text": fmt.Sprintf("%d %s brought in (the model files themselves come with a backup of the box)", total, plural(total, "thing", "things"))}, nil
}
