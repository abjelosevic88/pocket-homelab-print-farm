package server

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/slicer"
	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/store"
)

// seedDemo fills the owner's account with a small print farm: four
// printers (two printing now), a shelf of filament, a dozen models with
// their shapes and sliced G-code, a queue, nine months of prints and the
// printers' upkeep. Only when the account is still empty.
func (s *Server) seedDemo(ctx context.Context, uid int64) error {
	ctx = context.WithoutCancel(ctx)
	if have, _ := s.store.ListPrinters(ctx, uid, true); len(have) > 0 {
		return nil
	}
	rng := rand.New(rand.NewSource(42))
	st := s.settingsOf(ctx, uid)

	// ---- filament ----
	type fil struct {
		brand, material, name, color string
		nozzle, bed, price, spool    int
	}
	fils := []fil{
		{"Prusament", "PLA", "Galaxy Black", "#2b2b33", 215, 60, 2499, 201},
		{"Prusament", "PETG", "Jet Black", "#1c1c1f", 240, 85, 2499, 201},
		{"Polymaker", "PLA", "PolyTerra Cotton White", "#f1efe8", 210, 55, 1999, 140},
		{"eSUN", "PETG", "Solid Blue", "#2a5db0", 235, 80, 1899, 224},
		{"Bambu Lab", "PLA", "Basic Orange", "#f47a20", 220, 60, 1999, 250},
		{"Sunlu", "TPU", "95A Clear", "#d9e6ee", 225, 50, 2299, 160},
		{"Polymaker", "ASA", "Grey", "#8a8d91", 255, 100, 2799, 140},
		{"Elegoo", "PLA", "Silk Gold", "#d4a937", 215, 60, 1799, 155},
		{"Prusament", "PLA", "Lipstick Red", "#c3242f", 215, 60, 2499, 201},
	}
	filIDs := make([]int64, len(fils))
	for i, f := range fils {
		x := &store.Filament{UserID: uid, Brand: f.brand, Material: f.material, Name: f.name, Color: f.color, Diameter: 1.75, Density: densities[f.material],
			NozzleTemp: f.nozzle, BedTemp: f.bed, NetWeight: 1000, SpoolWeight: f.spool, Price: f.price}
		if err := s.store.CreateFilament(ctx, x); err != nil {
			return err
		}
		filIDs[i] = x.ID
	}
	// spools: [filament, grams used, bought days ago, archived]
	type sp struct {
		f       int
		used    float64
		ago     int
		archive bool
	}
	spoolsIn := []sp{{0, 612, 120, false}, {0, 0, 20, false}, {1, 455, 90, false}, {2, 905, 150, false}, {2, 120, 40, false}, {3, 330, 70, false},
		{4, 780, 60, false}, {5, 210, 200, false}, {6, 95, 30, false}, {7, 640, 110, false}, {8, 50, 15, false}, {0, 1000, 260, true}, {2, 1000, 230, true}, {4, 1000, 180, true}}
	spoolIDs := make([]int64, len(spoolsIn))
	locs := []string{"Shelf A", "Shelf A", "Shelf B", "Dry box", "Shelf B"}
	for i, x := range spoolsIn {
		f := fils[x.f]
		bought := time.Now().AddDate(0, 0, -x.ago).Format("2006-01-02")
		y := &store.Spool{UserID: uid, FilamentID: filIDs[x.f], InitialG: 1000, UsedG: x.used, Price: f.price, BoughtOn: bought, Location: locs[i%len(locs)], Archived: x.archive}
		if x.used > 0 {
			y.OpenedOn = time.Now().AddDate(0, 0, -x.ago+3).Format("2006-01-02")
		}
		if f.material == "PETG" || f.material == "TPU" {
			y.DriedOn = time.Now().AddDate(0, 0, -rng.Intn(20)-2).Format("2006-01-02")
		}
		if err := s.store.CreateSpool(ctx, y); err != nil {
			return err
		}
		spoolIDs[i] = y.ID
	}

	// ---- printers ----
	type pr struct {
		name, model, loc, mats, color string
		x, y, z                       float64
		watts, wear, price            int
		start                         float64
		spool                         int
		bought                        int
	}
	prs := []pr{
		{"Mk4", "Prusa MK4", "Workshop", "PLA, PETG, ASA, TPU", "#f26b1d", 250, 210, 220, 120, 12, 109900, 140, 0, 400},
		{"Voron", "Voron 2.4 350", "Workshop", "PLA, PETG, ASA, ABS", "#c3242f", 350, 350, 340, 280, 18, 160000, 610, 6, 700},
		{"P1S", "Bambu Lab P1S", "Office", "PLA, PETG, TPU", "#2f8f5b", 256, 256, 256, 110, 10, 69900, 80, 3, 250},
		{"Ender", "Creality Ender-3 V3", "Garage", "PLA, PETG", "#2a5db0", 220, 220, 250, 100, 6, 26900, 35, 9, 180},
	}
	printerIDs := make([]int64, len(prs))
	printers := make([]*store.Printer, len(prs))
	for i, p := range prs {
		sid := spoolIDs[p.spool]
		x := &store.Printer{UserID: uid, Name: p.name, Model: p.model, Kind: "fdm", Connection: "manual", BedX: p.x, BedY: p.y, BedZ: p.z, Nozzle: 0.4,
			Materials: p.mats, Watts: p.watts, WearPerHour: p.wear, Price: p.price, StartHours: p.start, Location: p.loc, Color: p.color, SpoolID: &sid,
			BoughtOn: time.Now().AddDate(0, 0, -p.bought).Format("2006-01-02")}
		if err := s.store.CreatePrinter(ctx, x); err != nil {
			return err
		}
		printerIDs[i], printers[i] = x.ID, x
	}

	// ---- models: shapes and sliced G-code ----
	type mdl struct {
		file, name, tags, designer, desc string
		mesh                             func() *slicer.Mesh
		gcode                            bool
		minutes                          int
		grams                            float64
		material                         string
		fav                              bool
	}
	models := []mdl{
		{"calibration_cube.stl", "Calibration cube", "calibration, test", "", "20 mm cube to check the size and the corners.", func() *slicer.Mesh { return prism(square(20), 20, 0, 1) }, true, 38, 7.8, "PLA", false},
		{"twisted_vase.stl", "Twisted vase", "home, vase mode, gift", "Ana K.", "Print in vase mode (one wall, no top). Looks best in silk.", func() *slicer.Mesh { return prism(star(9, 42, 34), 160, 90, 0.75) }, true, 245, 62, "PLA", true},
		{"gear_24t.stl", "Gear, 24 teeth", "mechanical, spare part", "", "Module 2, 8 mm bore. For the garden gate opener.", func() *slicer.Mesh { return prism(gear(24, 26, 23), 10, 0, 1) }, true, 52, 14.5, "PETG", false},
		{"desk_planter.stl", "Desk planter", "home, plants, gift", "Ana K.", "Hexagon planter for a small succulent.", func() *slicer.Mesh { return prism(polygon(6, 45), 70, 0, 1.25) }, true, 186, 48, "PLA", true},
		{"star_ornament.stl", "Star ornament", "christmas, gift", "", "Hangs on the tree; prints in 20 minutes.", func() *slicer.Mesh { return prism(star(5, 40, 17), 4, 0, 1) }, true, 22, 5.1, "PLA", false},
		{"phone_stand.stl", "Phone stand", "desk, gift, sell", "", "Holds a phone at 60°. Sold at the market.", func() *slicer.Mesh { return wedge(80, 70, 90) }, true, 95, 31, "PETG", true},
		{"hex_bit_holder.stl", "Hex bit holder", "workshop, organisation", "", "Holds 12 screwdriver bits.", func() *slicer.Mesh { return prism(polygon(6, 30), 25, 0, 1) }, true, 64, 18, "PLA", false},
		{"spool_holder.stl", "Spool holder axle", "printer, upgrade", "", "Axle for the top spool holder.", func() *slicer.Mesh { return prism(polygon(40, 14), 90, 0, 1) }, true, 71, 16, "PETG", false},
		{"drawer_knob.stl", "Drawer knob", "home, spare part", "", "Replacement knob for the kitchen drawers (M4 screw).", func() *slicer.Mesh { return prism(polygon(36, 18), 26, 0, 0.7) }, true, 33, 8.2, "PLA", false},
		{"cable_clip.stl", "Cable clip", "desk, organisation", "", "Clips onto the desk edge; print four at once.", func() *slicer.Mesh { return prism(roundRect(30, 14, 5), 12, 0, 1) }, true, 18, 3.4, "PLA", false},
		{"bike_light_mount.stl", "Bike light mount", "outdoor, bike", "", "For a 31.8 mm handlebar. ASA for the sun.", func() *slicer.Mesh { return prism(gear(10, 22, 18), 18, 15, 1) }, true, 58, 12.5, "ASA", false},
		{"flexy_feet.stl", "Washing machine feet", "home, TPU", "", "Soft feet that stop it walking around.", func() *slicer.Mesh { return prism(polygon(40, 32), 15, 0, 0.85) }, true, 49, 22, "TPU", false},
		{"lamp_shade.stl", "Lamp shade", "home, lighting", "Ana K.", "Wide shade for an E27 bulb, vase mode.", func() *slicer.Mesh { return prism(star(14, 75, 68), 140, 30, 0.55) }, false, 0, 0, "PLA", false},
	}
	dir := filepath.Join(s.cfg.DataDir, "tmp")
	_ = os.MkdirAll(dir, 0o755)
	modelIDs := make([]int64, len(models))
	fileIDs := make([]int64, len(models))
	for i, md := range models {
		mesh := md.mesh()
		tmp := filepath.Join(dir, fmt.Sprintf("demo-%d.stl", i))
		if err := os.WriteFile(tmp, mesh.STL(), 0o644); err != nil {
			return err
		}
		m, _, _, err := s.addFile(ctx, uid, 0, md.file, tmp, "upload", "")
		_ = os.Remove(tmp)
		if err != nil {
			return err
		}
		m.Name, m.Tags, m.Designer, m.Description, m.Favourite = md.name, md.tags, md.designer, md.desc, md.fav
		if md.designer != "" {
			m.License, m.URL = "CC BY 4.0", "https://www.printables.com/"
		}
		_ = s.store.UpdateModel(ctx, m)
		modelIDs[i] = m.ID
		if md.gcode {
			g := fakeGcode(md.name, md.minutes, md.grams, md.material, mesh, i)
			tmp := filepath.Join(dir, fmt.Sprintf("demo-%d.gcode", i))
			if err := os.WriteFile(tmp, []byte(g), 0o644); err != nil {
				return err
			}
			gname := strings.TrimSuffix(md.file, ".stl") + "_0.2mm_" + md.material + "_MK4_" + fmt.Sprintf("%dh%dm", md.minutes/60, md.minutes%60) + ".gcode"
			_, f, _, err := s.addFile(ctx, uid, m.ID, gname, tmp, "upload", "")
			_ = os.Remove(tmp)
			if err != nil {
				return err
			}
			fileIDs[i] = f.ID
		}
		// added over the past months
		_, _ = s.store.DB.ExecContext(ctx, `UPDATE models SET created_at = ? WHERE id = ?`, time.Now().AddDate(0, 0, -(260-i*18)).UTC().Format(time.RFC3339Nano), m.ID)
	}

	// ---- nine months of prints ----
	reasons := []string{"adhesion", "adhesion", "spaghetti", "warping", "stringing", "clog", "layer_shift", "supports", "filament_out", "power"}
	notes := map[string]string{"adhesion": "Corner lifted after the first layers", "spaghetti": "Came loose halfway and turned to spaghetti", "warping": "Corners curled up",
		"stringing": "Too many strings between the towers", "clog": "Under-extrusion, then nothing came out", "layer_shift": "Layers shifted at about 3 cm",
		"supports": "Support came loose under the overhang", "filament_out": "The spool ran out overnight", "power": "Power cut"}
	start := time.Now().AddDate(0, -9, 0)
	day := start
	usedOn := map[int64]float64{}
	var lastPrint []int64
	for day.Before(time.Now().Add(-6 * time.Hour)) {
		for pi := range printers {
			if rng.Float64() > 0.62-float64(pi)*0.08 {
				continue
			}
			mi := rng.Intn(len(models) - 1)
			md := models[mi]
			if !md.gcode {
				continue
			}
			p := printers[pi]
			if md.material == "ASA" && pi > 1 || md.material == "TPU" && pi == 1 {
				continue
			}
			copies := 1
			if md.minutes < 40 {
				copies = 1 + rng.Intn(4)
			}
			secs := int(float64(md.minutes*60*copies) * (0.95 + rng.Float64()*0.12))
			at := time.Date(day.Year(), day.Month(), day.Day(), 8+rng.Intn(12), rng.Intn(60), 0, 0, time.Local)
			status := "done"
			x := rng.Float64()
			if x < 0.09 {
				status = "failed"
			} else if x < 0.11 {
				status = "cancelled"
			}
			progress := 1.0
			if status != "done" {
				progress = 0.08 + rng.Float64()*0.7
				secs = int(float64(secs) * progress)
			}
			grams := math.Round(md.grams*float64(copies)*progress*(0.97+rng.Float64()*0.06)*10) / 10
			// the spool of the right material
			var spool *int64
			for si, sx := range spoolsIn {
				if fils[sx.f].material == md.material && !sx.archive {
					id := spoolIDs[si]
					spool = &id
					break
				}
			}
			end := at.Add(time.Duration(secs) * time.Second).UTC().Format(time.RFC3339Nano)
			pid, mid := p.ID, modelIDs[mi]
			x2 := &store.Print{UserID: uid, PrinterID: &pid, ModelID: &mid, SpoolID: spool, Title: md.name, Status: status, StartedAt: at.UTC().Format(time.RFC3339Nano), EndedAt: &end,
				Seconds: secs, EstSeconds: md.minutes * 60 * copies, Progress: progress, Grams: grams, EstGrams: md.grams * float64(copies), Copies: copies, Settings: demoSettings(md.material, rng)}
			if fileIDs[mi] > 0 {
				fid := fileIDs[mi]
				x2.FileID = &fid
				x2.FileName = filepath.Base(fmt.Sprint(fid))
				if f, err := s.store.GetFile(ctx, uid, fid); err == nil {
					x2.FileName = f.Name
				}
			}
			if status == "failed" {
				r := reasons[rng.Intn(len(reasons))]
				x2.FailReason, x2.FailNote = r, notes[r]
			} else if status == "cancelled" {
				x2.FailReason = "cancelled_by_me"
			} else {
				x2.Rating = 3 + rng.Intn(3)
				if rng.Float64() < 0.08 {
					x2.Keep = true
					x2.Notes = "These settings came out perfect."
				}
			}
			var sp *store.Spool
			if spool != nil {
				sp, _ = s.store.GetSpool(ctx, uid, *spool)
			}
			s.workOutCost(x2, p, sp, st)
			if err := s.store.CreatePrint(ctx, x2); err != nil {
				return err
			}
			if spool != nil {
				usedOn[*spool] += grams
				_, _ = s.store.DB.ExecContext(ctx, `INSERT INTO spool_log (user_id, spool_id, print_id, kind, grams, note, at) VALUES (?,?,?,?,?,?,?)`, uid, *spool, x2.ID, "print", grams, md.name, end)
			}
			lastPrint = append(lastPrint, x2.ID)
		}
		day = day.AddDate(0, 0, 1)
	}
	_ = lastPrint
	for id := range usedOn {
		_, _ = s.store.DB.ExecContext(ctx, `UPDATE spools SET last_used_at = (SELECT MAX(at) FROM spool_log WHERE spool_id = ?) WHERE id = ?`, id, id)
	}
	for i, x := range spoolsIn {
		if x.used > 0 && !x.archive {
			_, _ = s.store.DB.ExecContext(ctx, `UPDATE spools SET last_used_at = COALESCE(last_used_at, ?) WHERE id = ?`, time.Now().AddDate(0, 0, -1-i).UTC().Format(time.RFC3339Nano), spoolIDs[i])
		}
	}

	// ---- now: two printing, one done and waiting for its bed to be cleared ----
	nowPrint := func(pi, mi int, ago time.Duration) error {
		md := models[mi]
		pid, mid, fid := printers[pi].ID, modelIDs[mi], fileIDs[mi]
		f, _ := s.store.GetFile(ctx, uid, fid)
		x := &store.Print{UserID: uid, PrinterID: &pid, ModelID: &mid, FileID: &fid, SpoolID: printers[pi].SpoolID, Title: md.name, Status: "printing",
			StartedAt: time.Now().Add(-ago).UTC().Format(time.RFC3339Nano), EstSeconds: md.minutes * 60, EstGrams: md.grams, Copies: 1, Settings: f.Settings, FileName: f.Name}
		return s.store.CreatePrint(ctx, x)
	}
	if err := nowPrint(0, 1, 95*time.Minute); err != nil { // the vase on the MK4, about 40%
		return err
	}
	if err := nowPrint(1, 3, 170*time.Minute); err != nil { // the planter on the Voron, nearly done
		return err
	}
	s.clearBed(ctx, printers[2].ID, true)

	// ---- the queue ----
	type jb struct {
		model                  int
		qty, done              int
		forWhom, due, material string
		price                  int
		printer                int
		notes                  string
	}
	jobs := []jb{
		{5, 12, 4, "Market stall (Saturday)", "+4", "PETG", 900, -1, "Black and blue, six each."},
		{4, 20, 0, "Ana", "+20", "PLA", 0, -1, "Gold for the tree, red for the presents."},
		{2, 2, 0, "Marko", "+3", "PETG", 0, 1, "Spare gears for the gate opener."},
		{8, 8, 0, "", "", "PLA", 0, 3, "Kitchen drawers: all eight at once."},
		{11, 4, 0, "", "", "TPU", 0, 2, ""},
		{1, 1, 0, "Mum's birthday", "+9", "PLA", 0, -1, "Silk gold."},
		{10, 1, 0, "", "", "ASA", 0, 1, ""},
	}
	for _, j := range jobs {
		mid, fid := modelIDs[j.model], fileIDs[j.model]
		x := &store.Job{UserID: uid, Title: models[j.model].name, ModelID: &mid, Quantity: j.qty, DoneCount: j.done, Status: "queued", ForWhom: j.forWhom, Material: j.material, Price: j.price, Notes: j.notes}
		if fid > 0 {
			x.FileID = &fid
		}
		if j.due != "" {
			var n int
			fmt.Sscan(j.due, &n)
			x.DueOn = time.Now().AddDate(0, 0, n).Format("2006-01-02")
		}
		if j.printer >= 0 {
			id := printers[j.printer].ID
			x.PrinterID = &id
		}
		if err := s.store.CreateJob(ctx, x); err != nil {
			return err
		}
	}
	for _, j := range []jb{{9, 16, 16, "", "", "PLA", 0, -1, ""}, {6, 3, 3, "Workshop", "", "PLA", 0, -1, ""}} {
		mid, fid := modelIDs[j.model], fileIDs[j.model]
		fin := time.Now().AddDate(0, 0, -6).UTC().Format(time.RFC3339Nano)
		x := &store.Job{UserID: uid, Title: models[j.model].name, ModelID: &mid, FileID: &fid, Quantity: j.qty, DoneCount: j.done, Status: "done", ForWhom: j.forWhom, Material: j.material, FinishedAt: &fin}
		if err := s.store.CreateJob(ctx, x); err != nil {
			return err
		}
	}

	// ---- upkeep ----
	type uk struct {
		title      string
		hours      float64
		days       int
		lastAgo    int     // days
		lastBefore float64 // hours before now
	}
	ups := []uk{
		{"Clean the nozzle and the bed", 50, 0, 6, 46},
		{"Lubricate the rods / rails", 200, 0, 40, 160},
		{"Check the belt tension", 0, 30, 33, 0},
		{"Replace the PTFE tube", 500, 0, 120, 380},
		{"Tighten the frame screws", 0, 90, 20, 0},
	}
	all, _ := s.store.ListPrinters(ctx, uid, true)
	for pi, p := range all {
		for ui, u := range ups {
			if pi == 2 && ui == 1 { // the P1S has no rods to grease
				continue
			}
			last := time.Now().AddDate(0, 0, -u.lastAgo-pi*4).Format("2006-01-02")
			hours := math.Max(0, p.Hours-u.lastBefore-float64(pi)*7)
			x := &store.Upkeep{UserID: uid, PrinterID: p.ID, Title: u.title, EveryHours: u.hours, EveryDays: u.days, LastDoneOn: last, LastHours: math.Round(hours*10) / 10}
			if err := s.store.CreateUpkeep(ctx, x); err != nil {
				return err
			}
			uid2 := x.ID
			for k := 1; k <= 3; k++ {
				done := time.Now().AddDate(0, 0, -u.lastAgo-pi*4-k*45).Format("2006-01-02")
				if done < start.Format("2006-01-02") {
					break
				}
				_ = s.store.CreateUpkeepLog(ctx, uid, &store.UpkeepLog{PrinterID: p.ID, UpkeepID: &uid2, Title: u.title, DoneOn: done, Hours: math.Max(0, hours-float64(k)*40)})
			}
			_ = s.store.CreateUpkeepLog(ctx, uid, &store.UpkeepLog{PrinterID: p.ID, UpkeepID: &uid2, Title: u.title, DoneOn: last, Hours: x.LastHours})
		}
	}
	_ = s.store.CreateUpkeepLog(ctx, uid, &store.UpkeepLog{PrinterID: all[3].ID, Title: "New hotend (the old one clogged)", DoneOn: time.Now().AddDate(0, 0, -75).Format("2006-01-02"), Cost: 3490, Note: "Bought the bimetal heatbreak too."})
	_ = s.store.CreateUpkeepLog(ctx, uid, &store.UpkeepLog{PrinterID: all[1].ID, Title: "New PEI sheet", DoneOn: time.Now().AddDate(0, 0, -50).Format("2006-01-02"), Cost: 2900})
	s.log.Info("demo data ready", "printers", len(printers), "models", len(models))
	return nil
}

func demoSettings(material string, rng *rand.Rand) []byte {
	temps := map[string][2]int{"PLA": {215, 60}, "PETG": {240, 85}, "ASA": {255, 100}, "TPU": {225, 50}}
	t := temps[material]
	return settingsJSON(map[string]string{"infill": []string{"15%", "20%", "25%"}[rng.Intn(3)], "walls": []string{"2", "3"}[rng.Intn(2)], "supports": "no",
		"nozzle_temp": fmt.Sprint(t[0]), "bed_temp": fmt.Sprint(t[1]), "infill_type": "gyroid", "profile": "0.20mm SPEED"})
}

// fakeGcode is a small G-code file with PrusaSlicer's comments, so the
// reader has something real to read.
func fakeGcode(name string, minutes int, grams float64, material string, mesh *slicer.Mesh, i int) string {
	info := &slicer.Info{}
	_ = info
	temps := map[string][2]int{"PLA": {215, 60}, "PETG": {240, 85}, "ASA": {255, 100}, "TPU": {225, 50}}
	t := temps[material]
	var b strings.Builder
	fmt.Fprintf(&b, "; generated by PrusaSlicer 2.8.1+linux-x64 on 2026-%02d-%02d at 10:%02d:00 UTC\n\n", 1+i%9, 1+i*2, i*3)
	fmt.Fprintf(&b, "; external perimeters extrusion width = 0.45mm\n; layer_height = 0.2\n\nM73 P0 R%d\nM104 S%d\nM140 S%d\nG28\nG1 Z0.2 F720\n", minutes, t[0], t[1])
	// a few moves around the shape's footprint
	minX, minY, maxX, maxY, maxZ := 1e9, 1e9, -1e9, -1e9, 0.0
	for _, tr := range mesh.Tris {
		for k := 0; k < 9; k += 3 {
			minX, maxX = math.Min(minX, float64(tr[k])), math.Max(maxX, float64(tr[k]))
			minY, maxY = math.Min(minY, float64(tr[k+1])), math.Max(maxY, float64(tr[k+1]))
			maxZ = math.Max(maxZ, float64(tr[k+2]))
		}
	}
	ox, oy := 125-(maxX-minX)/2, 105-(maxY-minY)/2
	e := 0.0
	for z := 0.2; z <= maxZ+0.01; z += math.Max(0.2, maxZ/20) {
		fmt.Fprintf(&b, "G1 Z%.2f\n", z)
		for _, c := range [][2]float64{{0, 0}, {maxX - minX, 0}, {maxX - minX, maxY - minY}, {0, maxY - minY}, {0, 0}} {
			e += 0.8
			fmt.Fprintf(&b, "G1 X%.2f Y%.2f E%.4f\n", ox+c[0], oy+c[1], e)
		}
	}
	mm := grams / (math.Pi * 0.875 * 0.875 / 1000 * densities[material])
	fmt.Fprintf(&b, "M107\nM104 S0\nM140 S0\n\n; filament used [mm] = %.2f\n; filament used [cm3] = %.2f\n; filament used [g] = %.2f\n; filament cost = %.2f\n", mm, grams/densities[material], grams, grams*0.025)
	fmt.Fprintf(&b, "; estimated printing time (normal mode) = %dh %dm %ds\n; estimated first layer printing time (normal mode) = 3m 12s\n\n", minutes/60, minutes%60, (i*7)%60)
	fmt.Fprintf(&b, "; prusaslicer_config = begin\n; bed_temperature = %d\n; brim_width = 0\n; fill_density = 15%%\n; fill_pattern = gyroid\n; filament_settings_id = \"Prusament %s\"\n", t[1], material)
	fmt.Fprintf(&b, "; filament_type = %s\n; layer_height = 0.2\n; nozzle_diameter = 0.4\n; perimeters = 2\n; print_settings_id = \"0.20mm SPEED @MK4IS 0.4\"\n; printer_model = MK4IS\n", material)
	fmt.Fprintf(&b, "; support_material = 0\n; temperature = %d\n; prusaslicer_config = end\n", t[0])
	return b.String()
}

// ---- shapes for the demo ----

type pt struct{ x, y float64 }

func polygon(n int, r float64) []pt {
	out := make([]pt, n)
	for i := range out {
		a := 2 * math.Pi * float64(i) / float64(n)
		out[i] = pt{r * math.Cos(a), r * math.Sin(a)}
	}
	return out
}

func square(a float64) []pt {
	return []pt{{-a / 2, -a / 2}, {a / 2, -a / 2}, {a / 2, a / 2}, {-a / 2, a / 2}}
}

func star(n int, ro, ri float64) []pt {
	out := []pt{}
	for i := 0; i < n*2; i++ {
		r := ro
		if i%2 == 1 {
			r = ri
		}
		a := math.Pi * float64(i) / float64(n)
		out = append(out, pt{r * math.Cos(a), r * math.Sin(a)})
	}
	return out
}

func gear(teeth int, ro, ri float64) []pt {
	out := []pt{}
	for i := 0; i < teeth; i++ {
		a := 2 * math.Pi * float64(i) / float64(teeth)
		w := 2 * math.Pi / float64(teeth)
		for _, s := range []struct{ f, r float64 }{{0, ri}, {0.15, ro}, {0.5, ro}, {0.65, ri}} {
			out = append(out, pt{s.r * math.Cos(a+s.f*w), s.r * math.Sin(a+s.f*w)})
		}
	}
	return out
}

func roundRect(w, h, r float64) []pt {
	out := []pt{}
	corners := []pt{{w/2 - r, h/2 - r}, {-w/2 + r, h/2 - r}, {-w/2 + r, -h/2 + r}, {w/2 - r, -h/2 + r}}
	for i, c := range corners {
		for k := 0; k <= 6; k++ {
			a := math.Pi/2*float64(i) + math.Pi/2*float64(k)/6
			out = append(out, pt{c.x + r*math.Cos(a), c.y + r*math.Sin(a)})
		}
	}
	return out
}

// prism extrudes an outline (star-shaped around 0,0) to a height, turning
// it by twist degrees and scaling the top by top.
func prism(base []pt, h, twist, top float64) *slicer.Mesh {
	m := &slicer.Mesh{}
	steps := 1
	if twist != 0 || top != 1 {
		steps = 40
	}
	ring := func(k int) []pt {
		f := float64(k) / float64(steps)
		a := twist * math.Pi / 180 * f
		sc := 1 + (top-1)*f
		out := make([]pt, len(base))
		for i, p := range base {
			out[i] = pt{(p.x*math.Cos(a) - p.y*math.Sin(a)) * sc, (p.x*math.Sin(a) + p.y*math.Cos(a)) * sc}
		}
		return out
	}
	tri := func(a, b, c [3]float64) {
		m.Tris = append(m.Tris, [9]float32{float32(a[0]), float32(a[1]), float32(a[2]), float32(b[0]), float32(b[1]), float32(b[2]), float32(c[0]), float32(c[1]), float32(c[2])})
	}
	n := len(base)
	for k := 0; k < steps; k++ {
		lo, hi := ring(k), ring(k+1)
		z0, z1 := h*float64(k)/float64(steps), h*float64(k+1)/float64(steps)
		for i := 0; i < n; i++ {
			j := (i + 1) % n
			a, b := [3]float64{lo[i].x, lo[i].y, z0}, [3]float64{lo[j].x, lo[j].y, z0}
			c, d := [3]float64{hi[j].x, hi[j].y, z1}, [3]float64{hi[i].x, hi[i].y, z1}
			tri(a, b, c)
			tri(a, c, d)
		}
	}
	bot, tp := ring(0), ring(steps)
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		tri([3]float64{0, 0, 0}, [3]float64{bot[j].x, bot[j].y, 0}, [3]float64{bot[i].x, bot[i].y, 0})
		tri([3]float64{0, 0, h}, [3]float64{tp[i].x, tp[i].y, h}, [3]float64{tp[j].x, tp[j].y, h})
	}
	return m
}

// wedge is a phone stand: a right triangle (base × height) w wide.
func wedge(base, height, w float64) *slicer.Mesh {
	m := &slicer.Mesh{}
	p := [][3]float64{{0, -w / 2, 0}, {base, -w / 2, 0}, {base * 0.25, -w / 2, height}}
	q := [][3]float64{{0, w / 2, 0}, {base, w / 2, 0}, {base * 0.25, w / 2, height}}
	tri := func(a, b, c [3]float64) {
		m.Tris = append(m.Tris, [9]float32{float32(a[0]), float32(a[1]), float32(a[2]), float32(b[0]), float32(b[1]), float32(b[2]), float32(c[0]), float32(c[1]), float32(c[2])})
	}
	tri(p[0], p[2], p[1])
	tri(q[0], q[1], q[2])
	for i := 0; i < 3; i++ {
		j := (i + 1) % 3
		tri(p[i], p[j], q[j])
		tri(p[i], q[j], q[i])
	}
	return m
}
