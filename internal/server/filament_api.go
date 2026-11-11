package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/store"
)

// ---- filaments ----

type filamentIn struct {
	Brand       *string  `json:"brand"`
	Material    *string  `json:"material"`
	Name        *string  `json:"name"`
	Color       *string  `json:"color"`
	Diameter    *float64 `json:"diameter"`
	Density     *float64 `json:"density"`
	NozzleTemp  *int     `json:"nozzle_temp"`
	BedTemp     *int     `json:"bed_temp"`
	NetWeight   *int     `json:"net_weight"`
	SpoolWeight *int     `json:"spool_weight"`
	Price       *int     `json:"price"`
	URL         *string  `json:"url"`
	Notes       *string  `json:"notes"`
}

// densities of the usual materials, g/cm³
var densities = map[string]float64{"PLA": 1.24, "PETG": 1.27, "ABS": 1.04, "ASA": 1.07, "TPU": 1.21, "PA": 1.14, "NYLON": 1.14, "PC": 1.20, "PVA": 1.23, "HIPS": 1.04, "PP": 0.90, "PLA-CF": 1.30, "PETG-CF": 1.30, "PA-CF": 1.20, "RESIN": 1.10}

func (in filamentIn) apply(f *store.Filament, isNew bool) error {
	trimP(&f.Brand, in.Brand)
	trimP(&f.Name, in.Name)
	if in.Material != nil {
		f.Material = strings.ToUpper(clip(strings.TrimSpace(*in.Material), 20))
		if d, ok := densities[f.Material]; ok && isNew && in.Density == nil {
			f.Density = d
		}
	}
	if in.Color != nil {
		if *in.Color != "" && !reColor.MatchString(*in.Color) {
			return &userErr{"a colour is #rrggbb"}
		}
		f.Color = strings.ToLower(*in.Color)
	}
	if in.Diameter != nil {
		if *in.Diameter <= 0 || *in.Diameter > 5 {
			return &userErr{"the diameter is 1.75 or 2.85 mm"}
		}
		f.Diameter = *in.Diameter
	}
	if in.Density != nil {
		if *in.Density <= 0 || *in.Density > 5 {
			return &userErr{"the density is about 1.0 to 1.4 g/cm³"}
		}
		f.Density = *in.Density
	}
	for _, x := range []struct {
		dst *int
		v   *int
		max int
	}{{&f.NozzleTemp, in.NozzleTemp, 500}, {&f.BedTemp, in.BedTemp, 200}, {&f.NetWeight, in.NetWeight, 100000}, {&f.SpoolWeight, in.SpoolWeight, 10000}, {&f.Price, in.Price, 1 << 30}} {
		if x.v != nil {
			if *x.v < 0 || *x.v > x.max {
				return &userErr{"a temperature, weight or price is out of range"}
			}
			*x.dst = *x.v
		}
	}
	if in.URL != nil {
		v, ok := cleanURL(*in.URL)
		if !ok {
			return &userErr{"where to buy it is a web address"}
		}
		f.URL = v
	}
	if in.Notes != nil {
		f.Notes = cleanText(*in.Notes, maxNotes)
	}
	f.Brand, f.Name = clip(f.Brand, 60), clip(f.Name, 60)
	if f.Material == "" {
		return &userErr{"what material is it? (PLA, PETG…)"}
	}
	return nil
}

func (s *Server) handleFilaments(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListFilaments(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "filaments")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"filaments": list})
}

func (s *Server) handleCreateFilament(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in filamentIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	f := &store.Filament{UserID: actorID(ctx), Material: s.settingsOf(ctx, actorID(ctx)).DefaultMaterial, Diameter: 1.75, Density: 1.24, NetWeight: 1000, Source: sourceOf(ctx)}
	if err := in.apply(f, true); err != nil {
		s.userFail(w, err, "filament")
		return
	}
	if err := s.store.CreateFilament(ctx, f); err != nil {
		s.fail(w, err, "add filament")
		return
	}
	out, _ := s.store.GetFilament(ctx, f.UserID, f.ID)
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleGetFilament(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f, err := s.store.GetFilament(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "filament")
		return
	}
	spools, _ := s.store.ListSpools(ctx, f.UserID, store.SpoolFilter{FilamentID: f.ID, Archived: true})
	writeJSON(w, http.StatusOK, map[string]any{"filament": f, "spools": spools})
}

func (s *Server) handleUpdateFilament(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f, err := s.store.GetFilament(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "filament")
		return
	}
	var in filamentIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := in.apply(f, false); err != nil {
		s.userFail(w, err, "filament")
		return
	}
	if err := s.store.UpdateFilament(ctx, f); err != nil {
		s.fail(w, err, "save filament")
		return
	}
	out, _ := s.store.GetFilament(ctx, f.UserID, f.ID)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteFilament(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.DeleteFilament(ctx, actorID(ctx), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete filament")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- spools ----

type spoolIn struct {
	FilamentID *int64   `json:"filament_id"`
	InitialG   *float64 `json:"initial_g"`
	UsedG      *float64 `json:"used_g"`
	LeftG      *float64 `json:"left_g"`
	Price      *int     `json:"price"`
	BoughtOn   *string  `json:"bought_on"`
	OpenedOn   *string  `json:"opened_on"`
	DriedOn    *string  `json:"dried_on"`
	Location   *string  `json:"location"`
	Lot        *string  `json:"lot"`
	Notes      *string  `json:"notes"`
	Archived   *bool    `json:"archived"`
	// a new spool's filament, when it is not on the list yet
	Filament *filamentIn `json:"filament"`
	Count    int         `json:"count"` // several of the same at once
}

func (s *Server) applySpool(ctx context.Context, x *store.Spool, in spoolIn) error {
	if in.FilamentID != nil {
		if _, err := s.store.GetFilament(ctx, x.UserID, *in.FilamentID); err != nil {
			return &userErr{"choose one of your filaments"}
		}
		x.FilamentID = *in.FilamentID
	}
	if in.InitialG != nil {
		if *in.InitialG <= 0 || *in.InitialG > 100000 {
			return &userErr{"a spool has 1 to 100 000 g"}
		}
		x.InitialG = *in.InitialG
	}
	if in.UsedG != nil {
		x.UsedG = math.Max(0, *in.UsedG)
	}
	if in.LeftG != nil {
		x.UsedG = math.Max(0, x.InitialG-math.Max(0, *in.LeftG))
	}
	if in.Price != nil {
		if *in.Price < 0 {
			return &userErr{"a price is not negative"}
		}
		x.Price = *in.Price
	}
	for _, d := range []struct {
		dst *string
		v   *string
	}{{&x.BoughtOn, in.BoughtOn}, {&x.OpenedOn, in.OpenedOn}, {&x.DriedOn, in.DriedOn}} {
		if d.v != nil {
			if !validDate(*d.v) {
				return &userErr{"a day is YYYY-MM-DD"}
			}
			*d.dst = *d.v
		}
	}
	trimP(&x.Location, in.Location)
	trimP(&x.Lot, in.Lot)
	x.Location, x.Lot = clip(x.Location, 80), clip(x.Lot, 60)
	if in.Notes != nil {
		x.Notes = cleanText(*in.Notes, maxNotes)
	}
	if in.Archived != nil {
		x.Archived = *in.Archived
	}
	if x.FilamentID == 0 {
		return &userErr{"which filament is on the spool?"}
	}
	return nil
}

func (s *Server) spoolOf(w http.ResponseWriter, r *http.Request) (*store.Spool, bool) {
	x, err := s.store.GetSpool(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "spool")
		return nil, false
	}
	return x, true
}

func (s *Server) handleSpools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	list, err := s.store.ListSpools(ctx, uid, store.SpoolFilter{Archived: queryBool(r, "archived"), FilamentID: int64(queryInt(r, "filament")), Material: r.URL.Query().Get("material")})
	if err != nil {
		s.fail(w, err, "spools")
		return
	}
	st := s.settingsOf(ctx, uid)
	low, left := 0, 0.0
	for _, x := range list {
		if !x.Archived {
			left += x.LeftG
			if x.LeftG < float64(st.LowGrams) {
				low++
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"spools": list, "low": low, "left_g": math.Round(left), "low_grams": st.LowGrams})
}

func (s *Server) handleCreateSpool(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in spoolIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st := s.settingsOf(ctx, uid)
	if (in.FilamentID == nil || *in.FilamentID == 0) && in.Filament != nil {
		f := &store.Filament{UserID: uid, Material: st.DefaultMaterial, Diameter: 1.75, Density: 1.24, NetWeight: 1000, Source: sourceOf(ctx)}
		if err := in.Filament.apply(f, true); err != nil {
			s.userFail(w, err, "filament")
			return
		}
		if err := s.store.CreateFilament(ctx, f); err != nil {
			s.fail(w, err, "add filament")
			return
		}
		in.FilamentID = &f.ID
	}
	x := &store.Spool{UserID: uid, BoughtOn: store.Today(), Source: sourceOf(ctx)}
	if in.FilamentID != nil {
		if f, err := s.store.GetFilament(ctx, uid, *in.FilamentID); err == nil {
			x.InitialG, x.Price = float64(firstPos(f.NetWeight, 1000)), f.Price
		}
	}
	if err := s.applySpool(ctx, x, in); err != nil {
		s.userFail(w, err, "spool")
		return
	}
	n := clampInt(firstPos(in.Count, 1), 1, 50)
	out := []store.Spool{}
	for i := 0; i < n; i++ {
		y := *x
		if err := s.store.CreateSpool(ctx, &y); err != nil {
			s.fail(w, err, "add spool")
			return
		}
		got, _ := s.store.GetSpool(ctx, uid, y.ID)
		out = append(out, *got)
	}
	// written down in Pocket Money when asked
	if st.MoneyOnSpool && x.Price > 0 && linkApp(ctx) == "" {
		first := out[0]
		bg := ctxDetached(ctx)
		go func() {
			if link, err := s.spoolExpense(bg, uid, &first, n); err == nil {
				first.MoneyRef = link
				_ = s.store.UpdateSpool(bg, &first)
			}
		}()
	}
	writeJSON(w, http.StatusCreated, map[string]any{"spool": out[0], "spools": out})
}

func (s *Server) handleGetSpool(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	x, ok := s.spoolOf(w, r)
	if !ok {
		return
	}
	log, _ := s.store.SpoolLog(ctx, x.UserID, x.ID, 200)
	prints, _, _ := s.store.ListPrints(ctx, x.UserID, store.PrintFilter{SpoolID: x.ID, Limit: 50})
	writeJSON(w, http.StatusOK, map[string]any{"spool": x, "log": log, "prints": prints})
}

func (s *Server) handleUpdateSpool(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	x, ok := s.spoolOf(w, r)
	if !ok {
		return
	}
	if !mayChange(ctx, x.Source) {
		writeErr(w, http.StatusForbidden, "this spool was not added by that app")
		return
	}
	var in spoolIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	before := x.UsedG
	if err := s.applySpool(ctx, x, in); err != nil {
		s.userFail(w, err, "spool")
		return
	}
	if err := s.store.UpdateSpool(ctx, x); err != nil {
		s.fail(w, err, "save spool")
		return
	}
	if d := x.UsedG - before; math.Abs(d) >= 0.1 {
		// a correction typed in: written in the spool's log (the weight is saved already)
		_ = s.store.LogSpool(ctx, x.UserID, x.ID, "fix", d, "Corrected by hand")
	}
	out, _ := s.store.GetSpool(ctx, x.UserID, x.ID)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteSpool(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	x, ok := s.spoolOf(w, r)
	if !ok {
		return
	}
	if !mayChange(ctx, x.Source) {
		writeErr(w, http.StatusForbidden, "this spool was not added by that app")
		return
	}
	if err := s.store.DeleteSpool(ctx, x.UserID, x.ID); err != nil {
		s.fail(w, err, "delete spool")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleWeighSpool: the spool on a kitchen scale. The weight with the empty
// spool ("gross_g") or the filament left ("left_g") sets what is left.
func (s *Server) handleWeighSpool(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	x, ok := s.spoolOf(w, r)
	if !ok {
		return
	}
	var in struct {
		GrossG *float64 `json:"gross_g"`
		LeftG  *float64 `json:"left_g"`
		UsedG  *float64 `json:"used_g"` // or: this much was used (a print not seen)
		Note   string   `json:"note"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	var delta float64
	kind := "weigh"
	switch {
	case in.UsedG != nil:
		delta, kind = *in.UsedG, "fix"
	case in.GrossG != nil:
		if x.SpoolWeight <= 0 {
			writeErr(w, http.StatusBadRequest, "the empty spool's weight is not known: enter it on the filament, or type what is left")
			return
		}
		delta = (x.InitialG - (*in.GrossG - float64(x.SpoolWeight))) - x.UsedG
	case in.LeftG != nil:
		delta = (x.InitialG - *in.LeftG) - x.UsedG
	default:
		writeErr(w, http.StatusBadRequest, "how much does it weigh?")
		return
	}
	note := cleanText(in.Note, maxLine)
	if note == "" && kind == "weigh" {
		note = "Weighed"
	}
	if err := s.store.UseSpool(ctx, x.UserID, x.ID, math.Round(delta*10)/10, kind, note, nil); err != nil {
		s.fail(w, err, "weigh")
		return
	}
	st := s.settingsOf(ctx, x.UserID)
	s.checkLow(ctx, x.UserID, x.ID, st)
	out, _ := s.store.GetSpool(ctx, x.UserID, x.ID)
	writeJSON(w, http.StatusOK, out)
}

// ---- Spoolman: bring spools over ----

type smVendor struct {
	Name string `json:"name"`
}

type smFilament struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Vendor      *smVendor `json:"vendor"`
	Material    string    `json:"material"`
	Density     float64   `json:"density"`
	Diameter    float64   `json:"diameter"`
	Weight      float64   `json:"weight"`
	SpoolWeight float64   `json:"spool_weight"`
	Price       float64   `json:"price"`
	ColorHex    string    `json:"color_hex"`
	Extruder    *float64  `json:"settings_extruder_temp"`
	Bed         *float64  `json:"settings_bed_temp"`
	Comment     string    `json:"comment"`
}

type smSpool struct {
	ID              int64      `json:"id"`
	Filament        smFilament `json:"filament"`
	InitialWeight   *float64   `json:"initial_weight"`
	UsedWeight      float64    `json:"used_weight"`
	RemainingWeight *float64   `json:"remaining_weight"`
	Price           *float64   `json:"price"`
	Location        string     `json:"location"`
	LotNr           string     `json:"lot_nr"`
	Comment         string     `json:"comment"`
	Archived        bool       `json:"archived"`
	FirstUsed       string     `json:"first_used"`
	Registered      string     `json:"registered"`
}

// handleSpoolmanImport brings filaments and spools over from Spoolman: from
// its address (it is asked for /api/v1/spool) or from its JSON export.
// Bringing them over again updates the weights and adds only what is new.
func (s *Server) handleSpoolmanImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		URL    string    `json:"url"`
		Spools []smSpool `json:"spools"`
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(b) > 0 && b[0] == '[' {
		err = json.Unmarshal(b, &in.Spools)
	} else {
		err = json.Unmarshal(b, &in)
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, "that is not Spoolman's spool list")
		return
	}
	if in.URL != "" {
		base := strings.TrimRight(strings.TrimSpace(in.URL), "/")
		if !strings.Contains(base, "://") {
			base = "http://" + base
		}
		base = strings.TrimSuffix(strings.TrimSuffix(base, "/api/v1/spool"), "/api/v1")
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(cctx, http.MethodGet, base+"/api/v1/spool?allow_archived=true", nil)
		res, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
		if err != nil {
			writeErr(w, http.StatusBadGateway, "Spoolman did not answer at "+base)
			return
		}
		defer res.Body.Close()
		if res.StatusCode >= 300 {
			writeErr(w, http.StatusBadGateway, fmt.Sprintf("Spoolman answered %d", res.StatusCode))
			return
		}
		if err := json.NewDecoder(io.LimitReader(res.Body, 64<<20)).Decode(&in.Spools); err != nil {
			writeErr(w, http.StatusBadGateway, "Spoolman's answer was not a spool list")
			return
		}
	}
	res, err := s.importSpoolman(ctx, uid, in.Spools)
	if err != nil {
		s.fail(w, err, "import")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) importSpoolman(ctx context.Context, uid int64, spools []smSpool) (map[string]any, error) {
	addedF, addedS, updated := 0, 0, 0
	fil := map[int64]int64{}
	for _, sp := range spools {
		f := sp.Filament
		fid, ok := fil[f.ID]
		if !ok {
			ext := fmt.Sprintf("spoolman:%d", f.ID)
			if have, err := s.store.FilamentByExternal(ctx, uid, ext); err == nil {
				fid = have.ID
			} else {
				nf := &store.Filament{UserID: uid, Material: strings.ToUpper(firstNonEmpty(clip(f.Material, 20), "PLA")), Name: clip(f.Name, 60), Diameter: firstPosF(f.Diameter, 1.75),
					Density: firstPosF(f.Density, 1.24), NetWeight: int(firstPosF(f.Weight, 1000)), SpoolWeight: int(f.SpoolWeight), Price: int(math.Round(f.Price * 100)),
					Notes: clip(f.Comment, maxNotes), ExternalID: ext, Source: "spoolman"}
				if f.Vendor != nil {
					nf.Brand = clip(f.Vendor.Name, 60)
				}
				if c := "#" + strings.TrimPrefix(strings.ToLower(f.ColorHex), "#"); reColor.MatchString(c) {
					nf.Color = c
				}
				if f.Extruder != nil {
					nf.NozzleTemp = int(*f.Extruder)
				}
				if f.Bed != nil {
					nf.BedTemp = int(*f.Bed)
				}
				if err := s.store.CreateFilament(ctx, nf); err != nil {
					return nil, err
				}
				fid = nf.ID
				addedF++
			}
			fil[f.ID] = fid
		}
		ext := fmt.Sprintf("spoolman:%d", sp.ID)
		initial := firstPosF(f.Weight, 1000)
		if sp.InitialWeight != nil && *sp.InitialWeight > 0 {
			initial = *sp.InitialWeight
		}
		used := sp.UsedWeight
		if sp.RemainingWeight != nil && used == 0 {
			used = math.Max(0, initial-*sp.RemainingWeight)
		}
		if have, err := s.store.SpoolByExternal(ctx, uid, ext); err == nil {
			if math.Abs(have.UsedG-used) >= 0.1 || have.Archived != sp.Archived {
				have.UsedG, have.Archived = used, sp.Archived
				if err := s.store.UpdateSpool(ctx, have); err != nil {
					return nil, err
				}
				updated++
			}
			continue
		}
		x := &store.Spool{UserID: uid, FilamentID: fid, InitialG: initial, UsedG: used, Location: clip(sp.Location, 80), Lot: clip(sp.LotNr, 60), Notes: clip(sp.Comment, maxNotes),
			Archived: sp.Archived, ExternalID: ext, Source: "spoolman"}
		if sp.Price != nil {
			x.Price = int(math.Round(*sp.Price * 100))
		}
		if len(sp.Registered) >= 10 {
			x.BoughtOn = sp.Registered[:10]
		}
		if len(sp.FirstUsed) >= 10 {
			x.OpenedOn = sp.FirstUsed[:10]
		}
		if err := s.store.CreateSpool(ctx, x); err != nil {
			return nil, err
		}
		addedS++
	}
	return map[string]any{"filaments": addedF, "spools": addedS, "updated": updated,
		"text": fmt.Sprintf("%d %s and %d %s brought over from Spoolman, %d updated", addedS, plural(addedS, "spool", "spools"), addedF, plural(addedF, "filament", "filaments"), updated)}, nil
}
