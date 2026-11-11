// Package slicer reads what there is to know from the files of a 3D model:
// the size and volume of a mesh (STL, 3MF, OBJ), and from sliced G-code the
// slicer's estimates (time, filament), its settings and its picture. It
// also turns any mesh into a binary STL for the 3D view in the browser.
package slicer

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Info is what was read from a file.
type Info struct {
	Kind         string            `json:"kind"`
	Title        string            `json:"title,omitempty"`
	SizeX        float64           `json:"size_x"`
	SizeY        float64           `json:"size_y"`
	SizeZ        float64           `json:"size_z"`
	Volume       float64           `json:"volume"` // mm³
	Triangles    int               `json:"triangles"`
	EstSeconds   int               `json:"est_seconds"`
	EstGrams     float64           `json:"est_grams"`
	EstMM        float64           `json:"est_mm"`
	LayerHeight  float64           `json:"layer_height"`
	Nozzle       float64           `json:"nozzle"`
	Material     string            `json:"material"`
	Slicer       string            `json:"slicer"`
	PrinterModel string            `json:"printer_model"`
	Settings     map[string]string `json:"settings"`
	Thumb        []byte            `json:"-"` // PNG
}

// KindOf is a file's kind from its name ("" = not a model file).
func KindOf(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.HasSuffix(n, ".gcode.3mf"):
		return "3mf"
	case strings.HasSuffix(n, ".stl"):
		return "stl"
	case strings.HasSuffix(n, ".3mf"):
		return "3mf"
	case strings.HasSuffix(n, ".obj"):
		return "obj"
	case strings.HasSuffix(n, ".step"), strings.HasSuffix(n, ".stp"):
		return "step"
	case strings.HasSuffix(n, ".bgcode"):
		return "bgcode"
	case strings.HasSuffix(n, ".gcode"), strings.HasSuffix(n, ".gco"), strings.HasSuffix(n, ".g"):
		return "gcode"
	}
	return ""
}

// HasMesh says whether the 3D view can show this kind.
func HasMesh(kind string) bool { return kind == "stl" || kind == "3mf" || kind == "obj" }

// Read reads a file on disk.
func Read(path, name string) (*Info, error) {
	kind := KindOf(name)
	if kind == "" {
		return nil, errors.New("not a model file: STL, 3MF, OBJ, STEP or G-code")
	}
	info := &Info{Kind: kind, Settings: map[string]string{}}
	switch kind {
	case "stl":
		m, err := readSTLFile(path)
		if err != nil {
			return nil, err
		}
		m.fill(info)
	case "obj":
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		m, err := readOBJ(f)
		if err != nil {
			return nil, err
		}
		m.fill(info)
	case "3mf":
		if err := read3MF(path, info); err != nil {
			return nil, err
		}
	case "gcode":
		if err := readGcodeFile(path, info); err != nil {
			return nil, err
		}
	}
	return info, nil
}

// ---------- meshes ----------

// Mesh is triangles, three points each.
type Mesh struct{ Tris [][9]float32 }

func (m *Mesh) fill(info *Info) {
	if len(m.Tris) == 0 {
		return
	}
	minX, minY, minZ := math.MaxFloat64, math.MaxFloat64, math.MaxFloat64
	maxX, maxY, maxZ := -math.MaxFloat64, -math.MaxFloat64, -math.MaxFloat64
	vol := 0.0
	for _, t := range m.Tris {
		for i := 0; i < 9; i += 3 {
			x, y, z := float64(t[i]), float64(t[i+1]), float64(t[i+2])
			minX, maxX = math.Min(minX, x), math.Max(maxX, x)
			minY, maxY = math.Min(minY, y), math.Max(maxY, y)
			minZ, maxZ = math.Min(minZ, z), math.Max(maxZ, z)
		}
		// signed volume of the tetrahedron from the origin
		ax, ay, az := float64(t[0]), float64(t[1]), float64(t[2])
		bx, by, bz := float64(t[3]), float64(t[4]), float64(t[5])
		cx, cy, cz := float64(t[6]), float64(t[7]), float64(t[8])
		vol += (ax*(by*cz-bz*cy) - ay*(bx*cz-bz*cx) + az*(bx*cy-by*cx)) / 6
	}
	info.SizeX, info.SizeY, info.SizeZ = round2(maxX-minX), round2(maxY-minY), round2(maxZ-minZ)
	info.Volume, info.Triangles = round2(math.Abs(vol)), len(m.Tris)
}

// STL writes the mesh as binary STL.
func (m *Mesh) STL() []byte {
	var b bytes.Buffer
	b.Write(make([]byte, 80))
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(m.Tris)))
	rec := make([]byte, 50)
	for _, t := range m.Tris {
		n := normal(t)
		for i, v := range n {
			binary.LittleEndian.PutUint32(rec[i*4:], math.Float32bits(v))
		}
		for i, v := range t {
			binary.LittleEndian.PutUint32(rec[12+i*4:], math.Float32bits(v))
		}
		b.Write(rec)
	}
	return b.Bytes()
}

func normal(t [9]float32) [3]float32 {
	ux, uy, uz := t[3]-t[0], t[4]-t[1], t[5]-t[2]
	vx, vy, vz := t[6]-t[0], t[7]-t[1], t[8]-t[2]
	nx, ny, nz := uy*vz-uz*vy, uz*vx-ux*vz, ux*vy-uy*vx
	l := float32(math.Sqrt(float64(nx*nx + ny*ny + nz*nz)))
	if l == 0 {
		return [3]float32{}
	}
	return [3]float32{nx / l, ny / l, nz / l}
}

// MeshOf reads a mesh file (STL, 3MF, OBJ) for the 3D view.
func MeshOf(path, kind string) (*Mesh, error) {
	switch kind {
	case "stl":
		return readSTLFile(path)
	case "obj":
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return readOBJ(f)
	case "3mf":
		zr, err := zip.OpenReader(path)
		if err != nil {
			return nil, errors.New("this 3MF file cannot be opened")
		}
		defer zr.Close()
		return mesh3MF(&zr.Reader)
	}
	return nil, errors.New("there is no shape to show in this file")
}

const maxTris = 4_000_000

func readSTLFile(path string) (*Mesh, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, _ := f.Stat()
	return ReadSTL(f, fi.Size())
}

// ReadSTL reads binary or ASCII STL.
func ReadSTL(r io.ReaderAt, size int64) (*Mesh, error) {
	head := make([]byte, 84)
	n, _ := r.ReadAt(head, 0)
	if n >= 84 {
		count := int64(binary.LittleEndian.Uint32(head[80:84]))
		if 84+count*50 == size && count <= maxTris {
			m := &Mesh{Tris: make([][9]float32, 0, count)}
			rd := bufio.NewReaderSize(io.NewSectionReader(r, 84, size-84), 1<<16)
			rec := make([]byte, 50)
			for i := int64(0); i < count; i++ {
				if _, err := io.ReadFull(rd, rec); err != nil {
					return nil, errors.New("the STL file is cut short")
				}
				var t [9]float32
				for k := 0; k < 9; k++ {
					t[k] = math.Float32frombits(binary.LittleEndian.Uint32(rec[12+k*4:]))
				}
				m.Tris = append(m.Tris, t)
			}
			return m, nil
		}
	}
	// ASCII
	sc := bufio.NewScanner(io.NewSectionReader(r, 0, size))
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	m := &Mesh{}
	var cur [9]float32
	k := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "vertex") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		for i := 0; i < 3; i++ {
			v, _ := strconv.ParseFloat(f[1+i], 32)
			cur[k*3+i] = float32(v)
		}
		if k++; k == 3 {
			m.Tris = append(m.Tris, cur)
			k = 0
			if len(m.Tris) > maxTris {
				return nil, errors.New("the STL file is too big to read")
			}
		}
	}
	if len(m.Tris) == 0 {
		return nil, errors.New("there are no triangles in this STL file")
	}
	return m, nil
}

func readOBJ(r io.Reader) (*Mesh, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	var vs [][3]float32
	m := &Mesh{}
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "v":
			if len(f) >= 4 {
				var p [3]float32
				for i := 0; i < 3; i++ {
					v, _ := strconv.ParseFloat(f[1+i], 32)
					p[i] = float32(v)
				}
				vs = append(vs, p)
			}
		case "f":
			idx := []int{}
			for _, x := range f[1:] {
				n, err := strconv.Atoi(strings.SplitN(x, "/", 2)[0])
				if err != nil {
					continue
				}
				if n < 0 {
					n = len(vs) + n + 1
				}
				if n >= 1 && n <= len(vs) {
					idx = append(idx, n-1)
				}
			}
			for i := 1; i+1 < len(idx); i++ {
				a, b, c := vs[idx[0]], vs[idx[i]], vs[idx[i+1]]
				m.Tris = append(m.Tris, [9]float32{a[0], a[1], a[2], b[0], b[1], b[2], c[0], c[1], c[2]})
			}
			if len(m.Tris) > maxTris {
				return nil, errors.New("the OBJ file is too big to read")
			}
		}
	}
	if len(m.Tris) == 0 {
		return nil, errors.New("there are no faces in this OBJ file")
	}
	return m, nil
}

// ---------- 3MF ----------

type xmlModel struct {
	Metadata []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:",chardata"`
	} `xml:"metadata"`
	Objects []struct {
		ID   string `xml:"id,attr"`
		Mesh struct {
			Vertices []struct {
				X float32 `xml:"x,attr"`
				Y float32 `xml:"y,attr"`
				Z float32 `xml:"z,attr"`
			} `xml:"vertices>vertex"`
			Triangles []struct {
				V1 int `xml:"v1,attr"`
				V2 int `xml:"v2,attr"`
				V3 int `xml:"v3,attr"`
			} `xml:"triangles>triangle"`
		} `xml:"mesh"`
	} `xml:"resources>object"`
}

func mesh3MF(zr *zip.Reader) (*Mesh, error) {
	m := &Mesh{}
	for _, f := range zr.File {
		n := strings.ToLower(f.Name)
		if !strings.HasSuffix(n, ".model") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		var xm xmlModel
		err = xml.NewDecoder(io.LimitReader(rc, 1<<30)).Decode(&xm)
		rc.Close()
		if err != nil {
			continue
		}
		for _, o := range xm.Objects {
			vs := o.Mesh.Vertices
			for _, t := range o.Mesh.Triangles {
				if t.V1 < len(vs) && t.V2 < len(vs) && t.V3 < len(vs) && t.V1 >= 0 && t.V2 >= 0 && t.V3 >= 0 {
					a, b, c := vs[t.V1], vs[t.V2], vs[t.V3]
					m.Tris = append(m.Tris, [9]float32{a.X, a.Y, a.Z, b.X, b.Y, b.Z, c.X, c.Y, c.Z})
				}
			}
			if len(m.Tris) > maxTris {
				return nil, errors.New("the 3MF file is too big to read")
			}
		}
	}
	if len(m.Tris) == 0 {
		return nil, errors.New("there is no shape in this 3MF file")
	}
	return m, nil
}

func read3MF(path string, info *Info) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return errors.New("this 3MF file cannot be opened")
	}
	defer zr.Close()
	var thumbs []*zip.File
	for _, f := range zr.File {
		n := strings.ToLower(f.Name)
		switch {
		case strings.HasSuffix(n, ".png") && strings.Contains(n, "metadata/") && !strings.Contains(n, "_small") && !strings.Contains(n, "pick_") && !strings.Contains(n, "top_"):
			thumbs = append(thumbs, f)
		case strings.HasPrefix(n, "metadata/plate_") && strings.HasSuffix(n, ".gcode"):
			// a sliced 3MF (Bambu Studio, OrcaSlicer): its G-code has the estimates
			if rc, err := f.Open(); err == nil {
				readGcode(rc, int64(f.UncompressedSize64), info)
				rc.Close()
			}
		case strings.HasSuffix(n, "3dmodel.model"):
			if rc, err := f.Open(); err == nil {
				var xm xmlModel
				if xml.NewDecoder(io.LimitReader(rc, 64<<20)).Decode(&xm) == nil {
					for _, md := range xm.Metadata {
						if strings.EqualFold(md.Name, "Title") && strings.TrimSpace(md.Value) != "" {
							info.Title = strings.TrimSpace(md.Value)
						}
						if strings.EqualFold(md.Name, "Application") && info.Slicer == "" {
							info.Slicer = strings.TrimSpace(md.Value)
						}
					}
				}
				rc.Close()
			}
		}
	}
	if len(thumbs) > 0 {
		best := thumbs[0]
		for _, t := range thumbs {
			if strings.Contains(strings.ToLower(t.Name), "thumbnail") || strings.Contains(strings.ToLower(t.Name), "plate_1.png") {
				best = t
				break
			}
		}
		if rc, err := best.Open(); err == nil {
			b, _ := io.ReadAll(io.LimitReader(rc, 8<<20))
			rc.Close()
			if bytes.HasPrefix(b, []byte("\x89PNG")) {
				info.Thumb = b
			}
		}
	}
	gs, gx, gy, gz := info.EstSeconds, info.SizeX, info.SizeY, info.SizeZ
	if m, err := mesh3MF(&zr.Reader); err == nil {
		m.fill(info)
	}
	if gs > 0 && gx > 0 {
		info.SizeX, info.SizeY, info.SizeZ = gx, gy, gz
	}
	return nil
}

// ---------- G-code ----------

func readGcodeFile(path string, info *Info) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, _ := f.Stat()
	readGcode(f, fi.Size(), info)
	return nil
}

// The slicers write their estimates and settings as comments: PrusaSlicer,
// SuperSlicer, OrcaSlicer and Bambu Studio as "; key = value" (at the start
// and the end), Cura as ";KEY:value" at the start.
var (
	reKV       = regexp.MustCompile(`^;\s*([A-Za-z0-9_ \[\]()./%-]+?)\s*[=:]\s*(.*)$`)
	reDuration = regexp.MustCompile(`(\d+)\s*([dhms])`)
	reThumb    = regexp.MustCompile(`^;\s*thumbnail(?:_PNG)?\s+begin\s+(\d+)x(\d+)`)
)

func readGcode(r io.Reader, size int64, info *Info) {
	const edge = 2 << 20
	var head, tail []byte
	if ra, ok := r.(io.ReaderAt); ok && size > 2*edge {
		head = make([]byte, edge)
		n, _ := ra.ReadAt(head, 0)
		head = head[:n]
		tail = make([]byte, edge)
		n, _ = ra.ReadAt(tail, size-edge)
		tail = tail[:n]
		if i := bytes.IndexByte(tail, '\n'); i >= 0 {
			tail = tail[i+1:]
		}
	} else {
		all, _ := io.ReadAll(io.LimitReader(r, 3*edge))
		if len(all) > 2*edge && size > int64(len(all)) {
			head = all[:edge]
		} else {
			head = all
		}
		if size > int64(len(all)) {
			// read on to the end, keeping only the last part
			buf := append([]byte(nil), all[len(all)-min(len(all), edge):]...)
			chunk := make([]byte, 1<<20)
			for {
				n, err := r.Read(chunk)
				if n > 0 {
					buf = append(buf, chunk[:n]...)
					if len(buf) > 2*edge {
						buf = append([]byte(nil), buf[len(buf)-edge:]...)
					}
				}
				if err != nil {
					break
				}
			}
			tail = buf
		}
	}
	kv := map[string]string{}
	var thumb []byte
	thumbArea := 0
	minX, minY, maxX, maxY, maxZ := math.MaxFloat64, math.MaxFloat64, -math.MaxFloat64, -math.MaxFloat64, 0.0
	moves := 0
	scan := func(b []byte, moves0 bool) {
		sc := bufio.NewScanner(bytes.NewReader(b))
		sc.Buffer(make([]byte, 1<<16), 4<<20)
		var tb strings.Builder
		inThumb, area := false, 0
		for sc.Scan() {
			line := sc.Text()
			if inThumb {
				if strings.Contains(line, "thumbnail end") || strings.Contains(line, "thumbnail_PNG end") {
					inThumb = false
					if area > thumbArea {
						if png, err := base64.StdEncoding.DecodeString(tb.String()); err == nil && bytes.HasPrefix(png, []byte("\x89PNG")) {
							thumb, thumbArea = png, area
						}
					}
					tb.Reset()
					continue
				}
				tb.WriteString(strings.TrimSpace(strings.TrimPrefix(line, ";")))
				continue
			}
			if m := reThumb.FindStringSubmatch(line); m != nil {
				w, _ := strconv.Atoi(m[1])
				h, _ := strconv.Atoi(m[2])
				inThumb, area = true, w*h
				continue
			}
			if strings.HasPrefix(line, ";") {
				if m := reKV.FindStringSubmatch(line); m != nil {
					k := strings.ToLower(strings.TrimSpace(m[1]))
					if _, ok := kv[k]; !ok {
						kv[k] = strings.TrimSpace(m[2])
					}
				} else if strings.HasPrefix(line, "; generated by") || strings.HasPrefix(line, ";Generated with") {
					kv["generated by"] = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "; generated by"), ";Generated with"))
				}
				continue
			}
			if !moves0 || moves > 2_000_000 {
				continue
			}
			// a bounding box from the moves that extrude
			if strings.HasPrefix(line, "G1 ") || strings.HasPrefix(line, "G0 ") {
				moves++
				if !strings.Contains(line, " E") && !strings.HasPrefix(line, "G1 Z") {
					continue
				}
				for _, f := range strings.Fields(line)[1:] {
					if len(f) < 2 {
						continue
					}
					v, err := strconv.ParseFloat(f[1:], 64)
					if err != nil {
						continue
					}
					switch f[0] {
					case 'X':
						minX, maxX = math.Min(minX, v), math.Max(maxX, v)
					case 'Y':
						minY, maxY = math.Min(minY, v), math.Max(maxY, v)
					case 'Z':
						if v < 500 {
							maxZ = math.Max(maxZ, v)
						}
					}
				}
			}
		}
	}
	scan(head, true)
	if len(tail) > 0 {
		scan(tail, false)
	}
	info.Thumb = thumb

	get := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := kv[k]; ok && v != "" {
				return v
			}
		}
		return ""
	}
	num := func(keys ...string) float64 {
		f := strings.Fields(get(keys...))
		if len(f) == 0 {
			return 0
		}
		v := strings.TrimSuffix(f[0], "%")
		v = strings.SplitN(v, ",", 2)[0]
		n, _ := strconv.ParseFloat(v, 64)
		return n

	}
	// time
	if v := get("estimated printing time (normal mode)", "total estimated time", "estimated printing time", "model printing time"); v != "" {
		info.EstSeconds = parseDuration(v)
	} else if v := get("time"); v != "" { // Cura
		info.EstSeconds, _ = strconv.Atoi(strings.TrimSpace(v))
	} else if v := get("print_time", "estimated_time"); v != "" {
		info.EstSeconds = parseDuration(v)
	}
	// filament
	info.EstGrams = round2(num("filament used [g]", "total filament weight [g]", "total filament used [g]", "filament_weight", "filament used (g)"))
	info.EstMM = round2(num("filament used [mm]", "total filament length [mm]", "filament_length"))
	if v := get("filament used"); v != "" && info.EstMM == 0 { // Cura: "4.5m"
		f, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(v), "m"), 64)
		info.EstMM = round2(f * 1000)
	}
	info.LayerHeight = num("layer_height", "layer height")
	info.Nozzle = num("nozzle_diameter")
	info.Material = strings.Trim(strings.SplitN(get("filament_type", "filament type", "material"), ";", 2)[0], `" `)
	info.PrinterModel = strings.Trim(get("printer_model", "printer_settings_id", "machine_name", "printer name"), `" `)
	info.Slicer = firstNonEmpty(strings.TrimSpace(get("generated by")), slicerOf(get("flavor")))
	if strings.HasPrefix(info.Slicer, "with ") {
		info.Slicer = strings.TrimPrefix(info.Slicer, "with ")
	}
	if v := num("max_layer_z"); v > 0 {
		maxZ = v
	}
	if maxX > minX && maxY > minY {
		info.SizeX, info.SizeY = round2(maxX-minX), round2(maxY-minY)
	}
	if mx := num("maxx"); mx > 0 { // Cura
		info.SizeX, info.SizeY = round2(mx-num("minx")), round2(num("maxy")-num("miny"))
		maxZ = math.Max(maxZ, num("maxz"))
	}
	info.SizeZ = round2(maxZ)
	if info.EstGrams == 0 && info.EstMM > 0 {
		d := num("filament_diameter")
		if d == 0 {
			d = 1.75
		}
		dens := num("filament_density")
		if dens == 0 {
			dens = 1.24
		}
		info.EstGrams = round2(info.EstMM * math.Pi * (d / 2) * (d / 2) / 1000 * dens)
	}
	// the settings people compare
	set := map[string][]string{
		"infill":      {"fill_density", "sparse_infill_density", "infill_sparse_density", "infill density"},
		"walls":       {"perimeters", "wall_loops", "wall_line_count"},
		"supports":    {"support_material", "enable_support", "support_enable"},
		"brim":        {"brim_width", "brim_type", "adhesion_type"},
		"nozzle_temp": {"temperature", "nozzle_temperature", "material_print_temperature"},
		"bed_temp":    {"bed_temperature", "hot_plate_temp", "material_bed_temperature"},
		"speed":       {"perimeter_speed", "outer_wall_speed", "speed_print"},
		"infill_type": {"fill_pattern", "sparse_infill_pattern", "infill_pattern"},
		"profile":     {"print_settings_id", "print_settings"},
		"filament":    {"filament_settings_id"},
	}
	for k, keys := range set {
		if v := strings.Trim(strings.SplitN(get(keys...), ";", 2)[0], `" `); v != "" && len(v) < 80 {
			if k == "supports" {
				switch v {
				case "0", "false":
					v = "no"
				case "1", "true":
					v = "yes"
				}
			}
			info.Settings[k] = v
		}
	}
}

func slicerOf(flavor string) string {
	if flavor != "" {
		return "Cura"
	}
	return ""
}

// parseDuration reads "1d 2h 3m 4s", "2h 3m", "45m 10s" or plain seconds.
func parseDuration(v string) int {
	v = strings.TrimSpace(v)
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	total := 0
	for _, m := range reDuration.FindAllStringSubmatch(v, -1) {
		n, _ := strconv.Atoi(m[1])
		switch m[2] {
		case "d":
			total += n * 86400
		case "h":
			total += n * 3600
		case "m":
			total += n * 60
		case "s":
			total += n
		}
	}
	return total
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// CleanName is a model's name from its file name: "phone_stand-v2.stl" → "Phone stand v2".
func CleanName(name string) string {
	n := filepath.Base(name)
	low := strings.ToLower(n)
	for _, ext := range []string{".gcode.3mf", ".bgcode", ".gcode", ".stl", ".3mf", ".obj", ".step", ".stp", ".gco"} {
		if strings.HasSuffix(low, ext) {
			n = n[:len(n)-len(ext)]
			break
		}
	}
	n = strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(n)
	n = strings.Join(strings.Fields(n), " ")
	if n == "" {
		return "Model"
	}
	r := []rune(n)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 32
	}
	return string(r)
}
