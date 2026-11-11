package server

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/slicer"
	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/store"
)

// ---- the model library ----

func (s *Server) modelOf(w http.ResponseWriter, r *http.Request) (*store.Model, bool) {
	m, err := s.store.GetModel(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "model")
		return nil, false
	}
	return m, true
}

func (s *Server) fileOf(w http.ResponseWriter, r *http.Request) (*store.ModelFile, bool) {
	f, err := s.store.GetFile(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "file")
		return nil, false
	}
	return f, true
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	list, total, err := s.store.ListModels(ctx, actorID(ctx), store.ModelFilter{Q: q.Get("q"), Tag: q.Get("tag"), Favourite: queryBool(r, "fav"), Archived: queryBool(r, "archived"),
		Sort: q.Get("sort"), Limit: clampInt(firstPos(queryInt(r, "limit"), 200), 1, 1000), Offset: max(0, queryInt(r, "offset"))})
	if err != nil {
		s.fail(w, err, "models")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": list, "total": total})
}

func (s *Server) handleModelTags(w http.ResponseWriter, r *http.Request) {
	tags, _ := s.store.ModelTags(r.Context(), actorID(r.Context()))
	type tc struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	out := []tc{}
	for k, v := range tags {
		out = append(out, tc{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Tag < out[j].Tag
	})
	writeJSON(w, http.StatusOK, map[string]any{"tags": out})
}

type modelIn struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Tags        *string `json:"tags"`
	Designer    *string `json:"designer"`
	License     *string `json:"license"`
	URL         *string `json:"url"`
	Favourite   *bool   `json:"favourite"`
	Archived    *bool   `json:"archived"`
}

func (in modelIn) apply(m *store.Model) error {
	if in.Name != nil {
		m.Name = cleanText(*in.Name, maxName)
	}
	if in.Description != nil {
		m.Description = cleanText(*in.Description, maxNotes)
	}
	if in.Tags != nil {
		m.Tags = cleanTags(*in.Tags)
	}
	if in.Designer != nil {
		m.Designer = cleanText(*in.Designer, maxName)
	}
	if in.License != nil {
		m.License = cleanText(*in.License, 60)
	}
	if in.URL != nil {
		v, ok := cleanURL(*in.URL)
		if !ok {
			return &userErr{"where it came from is a web address (https://…)"}
		}
		m.URL = v
	}
	if in.Favourite != nil {
		m.Favourite = *in.Favourite
	}
	if in.Archived != nil {
		m.Archived = *in.Archived
	}
	if m.Name == "" {
		return &userErr{"give the model a name"}
	}
	return nil
}

func cleanTags(v string) string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range strings.Split(v, ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] && len(out) < 20 {
			seen[t] = true
			out = append(out, clip(t, 30))
		}
	}
	return strings.Join(out, ", ")
}

func (s *Server) handleCreateModel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in modelIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	m := &store.Model{UserID: actorID(ctx), Source: sourceOf(ctx)}
	if err := in.apply(m); err != nil {
		s.userFail(w, err, "model")
		return
	}
	if err := s.store.CreateModel(ctx, m); err != nil {
		s.fail(w, err, "add model")
		return
	}
	s.afterModelAdded(ctx, m)
	s.modelResponse(w, r, m.ID, http.StatusCreated)
}

func (s *Server) afterModelAdded(ctx context.Context, m *store.Model) {
	s.publish(ctx, m.UserID, "printfarm.model_added", fmt.Sprint(m.ID), map[string]any{"model_id": m.ID, "name": m.Name, "url": s.link(fmt.Sprintf("/models/%d", m.ID))})
}

func (s *Server) modelResponse(w http.ResponseWriter, r *http.Request, id int64, code int) {
	ctx := r.Context()
	uid := actorID(ctx)
	m, err := s.store.GetModel(ctx, uid, id)
	if err != nil {
		s.fail(w, err, "model")
		return
	}
	files, _ := s.store.ListFiles(ctx, uid, id)
	prints, _, _ := s.store.ListPrints(ctx, uid, store.PrintFilter{ModelID: id, Limit: 50})
	kept := []store.Print{}
	for _, p := range prints {
		if p.Keep {
			kept = append(kept, p)
		}
	}
	jobs, _ := s.store.ListJobs(ctx, uid, "queued", "printing")
	queued := []store.Job{}
	for _, j := range jobs {
		if j.ModelID != nil && *j.ModelID == id {
			queued = append(queued, j)
		}
	}
	writeJSON(w, code, map[string]any{"model": m, "files": files, "prints": prints, "kept": kept, "queued": queued})
}

func (s *Server) handleGetModel(w http.ResponseWriter, r *http.Request) {
	s.modelResponse(w, r, idParam(r, "id"), http.StatusOK)
}

func (s *Server) handleUpdateModel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	m, ok := s.modelOf(w, r)
	if !ok {
		return
	}
	var in modelIn
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := in.apply(m); err != nil {
		s.userFail(w, err, "model")
		return
	}
	if err := s.store.UpdateModel(ctx, m); err != nil {
		s.fail(w, err, "save model")
		return
	}
	s.modelResponse(w, r, m.ID, http.StatusOK)
}

func (s *Server) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	m, ok := s.modelOf(w, r)
	if !ok {
		return
	}
	files, _ := s.store.ListFiles(ctx, m.UserID, m.ID)
	if err := s.store.DeleteModel(ctx, m.UserID, m.ID); err != nil {
		s.fail(w, err, "delete model")
		return
	}
	for _, f := range files {
		s.dropMedia(f.Path, f.Thumb)
	}
	s.dropMedia(m.Cover)
	w.WriteHeader(http.StatusNoContent)
}

// thumbColor is the colour models are drawn in until there is a photo.
var thumbColor = color.RGBA{0xf2, 0x7a, 0x2e, 0xff}

// ---- files in ----

// addFile reads a file already on disk (a temp file) into the library: in
// the given model, or a new one named after the file. It returns the model,
// the file and whether it was new (the same file twice is kept once).
func (s *Server) addFile(ctx context.Context, uid, modelID int64, name, tmp, source, ref string) (*store.Model, *store.ModelFile, bool, error) {
	kind := slicer.KindOf(name)
	if kind == "" {
		return nil, nil, false, &userErr{name + ": not a model file (STL, 3MF, OBJ, STEP or G-code)"}
	}
	in, err := os.Open(tmp)
	if err != nil {
		return nil, nil, false, err
	}
	ext := strings.ToLower(filepath.Ext(name))
	if strings.HasSuffix(strings.ToLower(name), ".gcode.3mf") {
		ext = ".3mf"
	}
	rel, size, sha, err := s.saveMedia(uid, ext, in, s.cfg.MaxUploadBytes)
	in.Close()
	if err != nil {
		return nil, nil, false, err
	}
	if old, err := s.store.FileBySHA(ctx, uid, sha); err == nil && (modelID == 0 || old.ModelID == modelID) {
		s.dropMedia(rel)
		m, _ := s.store.GetModel(ctx, uid, old.ModelID)
		return m, old, false, nil
	}
	info, err := slicer.Read(s.mediaPath(rel), name)
	if err != nil {
		s.dropMedia(rel)
		return nil, nil, false, &userErr{name + ": " + err.Error()}
	}
	var m *store.Model
	if modelID > 0 {
		if m, err = s.store.GetModel(ctx, uid, modelID); err != nil {
			s.dropMedia(rel)
			return nil, nil, false, &userErr{"that model is not in the library"}
		}
	} else {
		m = &store.Model{UserID: uid, Name: clip(firstNonEmpty(info.Title, slicer.CleanName(name)), maxName), Source: sourceOf(ctx)}
		if source == "link" {
			m.URL = ref
		}
		if err := s.store.CreateModel(ctx, m); err != nil {
			s.dropMedia(rel)
			return nil, nil, false, err
		}
		s.afterModelAdded(ctx, m)
	}
	f := &store.ModelFile{UserID: uid, ModelID: m.ID, Name: clip(filepath.Base(name), 200), Kind: kind, Path: rel, Size: size, SHA256: sha,
		SizeX: info.SizeX, SizeY: info.SizeY, SizeZ: info.SizeZ, Volume: info.Volume, Triangles: info.Triangles,
		EstSeconds: info.EstSeconds, EstGrams: info.EstGrams, EstMM: info.EstMM, LayerHeight: info.LayerHeight, Nozzle: info.Nozzle,
		Material: clip(info.Material, 20), Slicer: clip(info.Slicer, 80), PrinterModel: clip(info.PrinterModel, 80), Settings: settingsJSON(info.Settings),
		Source: source, SourceRef: clip(ref, 500)}
	if len(info.Thumb) == 0 && slicer.HasMesh(kind) && size < 300<<20 {
		if mesh, err := slicer.MeshOf(s.mediaPath(rel), kind); err == nil {
			info.Thumb = mesh.Render(480, thumbColor)
		}
	}
	if len(info.Thumb) > 0 {
		if t, err := s.saveBytes(uid, ".png", info.Thumb); err == nil {
			f.Thumb = t
		}
	}
	if err := s.store.CreateFile(ctx, f); err != nil {
		s.dropMedia(rel, f.Thumb)
		return nil, nil, false, err
	}
	if m.Cover == "" && f.Thumb != "" {
		if b, err := os.ReadFile(s.mediaPath(f.Thumb)); err == nil {
			if c, err := s.saveBytes(uid, ".png", b); err == nil {
				m.Cover = c
				_ = s.store.UpdateModel(ctx, m)
			}
		}
	}
	if fresh, err := s.store.GetModel(ctx, uid, m.ID); err == nil {
		m = fresh
	}
	if fresh, err := s.store.GetFile(ctx, uid, f.ID); err == nil {
		f = fresh
	}
	return m, f, true, nil
}

// handleUpload takes model files (multipart "file", one or more; "model_id"
// to add them to a model). Each new file is a new model unless model_id is
// given or "together" puts them all in one.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "send the files as a form")
		return
	}
	tmpDir := filepath.Join(s.cfg.DataDir, "tmp")
	_ = os.MkdirAll(tmpDir, 0o755)
	modelID := int64(0)
	together := false
	type added struct {
		Model *store.Model     `json:"model"`
		File  *store.ModelFile `json:"file"`
		New   bool             `json:"new"`
	}
	out := []added{}
	var errs []string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, "the upload was cut short")
			return
		}
		switch part.FormName() {
		case "model_id":
			b, _ := io.ReadAll(io.LimitReader(part, 20))
			fmt.Sscan(string(b), &modelID)
			continue
		case "together":
			b, _ := io.ReadAll(io.LimitReader(part, 10))
			together = strings.TrimSpace(string(b)) == "1" || strings.TrimSpace(string(b)) == "true"
			continue
		case "file":
		default:
			continue
		}
		name := filepath.Base(part.FileName())
		if name == "" || name == "." {
			continue
		}
		tf, err := os.CreateTemp(tmpDir, "up-*")
		if err != nil {
			s.fail(w, err, "upload")
			return
		}
		_, err = io.Copy(tf, io.LimitReader(part, s.cfg.MaxUploadBytes+1))
		tf.Close()
		if err != nil {
			_ = os.Remove(tf.Name())
			errs = append(errs, name+": the upload was cut short")
			continue
		}
		m, f, isNew, err := s.addFile(ctx, uid, modelID, name, tf.Name(), "upload", "")
		_ = os.Remove(tf.Name())
		if err != nil {
			errs = append(errs, plainUserErr(err))
			continue
		}
		if together && modelID == 0 {
			modelID = m.ID
		}
		out = append(out, added{m, f, isNew})
	}
	if len(out) == 0 {
		writeErr(w, http.StatusBadRequest, firstNonEmpty(strings.Join(errs, "; "), "choose one or more model files"))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"added": out, "errors": errs})
}

func plainUserErr(err error) string {
	if ue, ok := err.(*userErr); ok {
		return ue.msg
	}
	return err.Error()
}

// handleAddLink downloads a model file from the web (a direct link to an
// STL, 3MF or G-code file).
func (s *Server) handleAddLink(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		URL     string `json:"url"`
		Name    string `json:"name"`
		ModelID int64  `json:"model_id"`
		Tags    string `json:"tags"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	u, ok := cleanURL(in.URL)
	if !ok || u == "" {
		writeErr(w, http.StatusBadRequest, "paste a link to a model file (https://…/thing.stl)")
		return
	}
	tmp, name, err := s.download(ctx, u)
	if err != nil {
		s.userFail(w, err, "download")
		return
	}
	defer os.Remove(tmp)
	m, f, isNew, err := s.addFile(ctx, uid, in.ModelID, name, tmp, "link", u)
	if err != nil {
		s.userFail(w, err, "add")
		return
	}
	if isNew && in.ModelID == 0 && (strings.TrimSpace(in.Name) != "" || in.Tags != "") {
		if t := cleanText(in.Name, maxName); t != "" {
			m.Name = t
		}
		if in.Tags != "" {
			m.Tags = cleanTags(in.Tags)
		}
		_ = s.store.UpdateModel(ctx, m)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"model": m, "file": f, "new": isNew})
}

// download fetches a file from the web into a temp file, with its name.
func (s *Server) download(ctx context.Context, u string) (string, string, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(cctx, http.MethodGet, u, nil)
	req.Header.Set("User-Agent", s.cfg.UserAgent)
	res, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return "", "", &userErr{"the link did not answer"}
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return "", "", &userErr{fmt.Sprintf("the link answered %d", res.StatusCode)}
	}
	name := ""
	if _, p, err := mime.ParseMediaType(res.Header.Get("Content-Disposition")); err == nil {
		name = p["filename"]
	}
	if name == "" {
		if pu, err := url.Parse(res.Request.URL.String()); err == nil {
			name, _ = url.PathUnescape(path.Base(pu.Path))
		}
	}
	if slicer.KindOf(name) == "" {
		if strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
			return "", "", &userErr{"that link is a web page, not a file: open the page and copy the link of its download button (the .stl or .3mf)"}
		}
		return "", "", &userErr{"that link is not an STL, 3MF, OBJ, STEP or G-code file"}
	}
	dir := filepath.Join(s.cfg.DataDir, "tmp")
	_ = os.MkdirAll(dir, 0o755)
	tf, err := os.CreateTemp(dir, "dl-*")
	if err != nil {
		return "", "", err
	}
	n, err := io.Copy(tf, io.LimitReader(res.Body, s.cfg.MaxUploadBytes+1))
	tf.Close()
	if err != nil || n > s.cfg.MaxUploadBytes {
		_ = os.Remove(tf.Name())
		return "", "", &userErr{"the file could not be downloaded whole"}
	}
	return tf.Name(), filepath.Base(name), nil
}

// ---- files out ----

func (s *Server) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOf(w, r)
	if !ok {
		return
	}
	s.serveMediaFile(w, r, f.Path, mimeByExt(f.Name), f.Name)
}

// handleFileMesh is the shape as binary STL, for the 3D view.
func (s *Server) handleFileMesh(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOf(w, r)
	if !ok {
		return
	}
	if !slicer.HasMesh(f.Kind) {
		writeErr(w, http.StatusBadRequest, "there is no shape to show in a "+strings.ToUpper(f.Kind)+" file")
		return
	}
	if f.Kind == "stl" {
		s.serveMediaFile(w, r, f.Path, "model/stl", "")
		return
	}
	m, err := slicer.MeshOf(s.mediaPath(f.Path), f.Kind)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "model/stl")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(m.STL())
}

// handleFileThumb is the file's picture; a POST keeps one drawn by the 3D view.
func (s *Server) handleFileThumb(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f, ok := s.fileOf(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		if f.Thumb == "" {
			writeErr(w, http.StatusNotFound, "no picture")
			return
		}
		s.serveMediaFile(w, r, f.Thumb, "", "")
		return
	}
	b, err := s.readPicture(r)
	if err != nil {
		s.userFail(w, err, "picture")
		return
	}
	rel, err := s.saveBytes(f.UserID, imageExt(b), b)
	if err != nil {
		s.fail(w, err, "picture")
		return
	}
	old := f.Thumb
	f.Thumb = rel
	if err := s.store.UpdateFile(ctx, f); err != nil {
		s.fail(w, err, "picture")
		return
	}
	s.dropMedia(old)
	// the model's cover, when it has none yet
	if m, err := s.store.GetModel(ctx, f.UserID, f.ModelID); err == nil && m.Cover == "" {
		if c, err := s.saveBytes(f.UserID, imageExt(b), b); err == nil {
			m.Cover = c
			_ = s.store.UpdateModel(ctx, m)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleUpdateFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f, ok := s.fileOf(w, r)
	if !ok {
		return
	}
	var in struct {
		Name     *string `json:"name"`
		ModelID  *int64  `json:"model_id"`
		Material *string `json:"material"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Name != nil {
		n := clip(filepath.Base(strings.TrimSpace(*in.Name)), 200)
		if slicer.KindOf(n) != f.Kind && !(f.Kind == "3mf" && strings.HasSuffix(strings.ToLower(n), ".3mf")) {
			writeErr(w, http.StatusBadRequest, "keep the file's ending (."+f.Kind+")")
			return
		}
		f.Name = n
	}
	if in.ModelID != nil {
		if _, err := s.store.GetModel(ctx, f.UserID, *in.ModelID); err != nil {
			writeErr(w, http.StatusBadRequest, "that model is not in the library")
			return
		}
		f.ModelID = *in.ModelID
	}
	if in.Material != nil {
		f.Material = clip(strings.TrimSpace(*in.Material), 20)
	}
	if err := s.store.UpdateFile(ctx, f); err != nil {
		s.fail(w, err, "save file")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOf(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteFile(r.Context(), f.UserID, f.ID); err != nil {
		s.fail(w, err, "delete file")
		return
	}
	s.dropMedia(f.Path, f.Thumb)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleModelCover(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	m, ok := s.modelOf(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		if m.Cover == "" {
			writeErr(w, http.StatusNotFound, "no picture")
			return
		}
		s.serveMediaFile(w, r, m.Cover, "", "")
	case http.MethodDelete:
		s.dropMedia(m.Cover)
		m.Cover = ""
		_ = s.store.UpdateModel(ctx, m)
		w.WriteHeader(http.StatusNoContent)
	default:
		b, err := s.readPicture(r)
		if err != nil {
			s.userFail(w, err, "picture")
			return
		}
		rel, err := s.saveBytes(m.UserID, imageExt(b), b)
		if err != nil {
			s.fail(w, err, "picture")
			return
		}
		old := m.Cover
		m.Cover = rel
		if err := s.store.UpdateModel(ctx, m); err != nil {
			s.fail(w, err, "picture")
			return
		}
		s.dropMedia(old)
		s.modelResponse(w, r, m.ID, http.StatusOK)
	}
}
