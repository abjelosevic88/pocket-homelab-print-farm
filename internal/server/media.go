package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Model files, pictures and photos are files in DATA_DIR/media/<user id>/ —
// never in the database — so a big G-code does not slow every query and a
// backup of the folder is a plain copy. The database keeps the relative path.

func (s *Server) mediaRoot() string { return filepath.Join(s.cfg.DataDir, "media") }

// mediaPath is the full path of a relative media name; "" when it would leave the folder.
func (s *Server) mediaPath(rel string) string {
	if rel == "" || strings.Contains(rel, "..") || filepath.IsAbs(rel) {
		return ""
	}
	return filepath.Join(s.mediaRoot(), filepath.FromSlash(rel))
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var reExt = regexp.MustCompile(`^\.[a-z0-9]{1,6}$`)

// newMediaName is a fresh relative name for one person's file.
func newMediaName(uid int64, ext string) string {
	ext = strings.ToLower(ext)
	if !reExt.MatchString(ext) {
		ext = ".bin"
	}
	return fmt.Sprintf("%d/%s%s", uid, randomHex(10), ext)
}

// saveMedia copies r into a new file of the person's; it returns the
// relative name, the size and the SHA-256. At most max bytes.
func (s *Server) saveMedia(uid int64, ext string, r io.Reader, max int64) (string, int64, string, error) {
	rel := newMediaName(uid, ext)
	p := s.mediaPath(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", 0, "", err
	}
	f, err := os.OpenFile(p+".part", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", 0, "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r, max+1))
	cerr := f.Close()
	if err == nil {
		err = cerr
	}
	if err == nil && n > max {
		err = &userErr{fmt.Sprintf("a file can be up to %d MB", max>>20)}
	}
	if err == nil && n == 0 {
		err = &userErr{"the file is empty"}
	}
	if err != nil {
		_ = os.Remove(p + ".part")
		return "", 0, "", err
	}
	return rel, n, hex.EncodeToString(h.Sum(nil)), os.Rename(p+".part", p)
}

// saveBytes keeps a small file (a picture).
func (s *Server) saveBytes(uid int64, ext string, b []byte) (string, error) {
	rel, _, _, err := s.saveMedia(uid, ext, strings.NewReader(string(b)), 32<<20)
	return rel, err
}

// dropMedia removes files.
func (s *Server) dropMedia(rels ...string) {
	for _, rel := range rels {
		if p := s.mediaPath(rel); p != "" {
			_ = os.Remove(p)
		}
	}
}

func mimeByExt(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".stl":
		return "model/stl"
	case ".3mf":
		return "model/3mf"
	case ".obj":
		return "model/obj"
	case ".gcode", ".gco", ".bgcode":
		return "text/x.gcode"
	case ".step", ".stp":
		return "model/step"
	}
	return mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
}

// serveMediaFile sends a media file (download names it).
func (s *Server) serveMediaFile(w http.ResponseWriter, r *http.Request, rel, ctype, download string) {
	p := s.mediaPath(rel)
	f, err := os.Open(p)
	if p == "" || err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if ctype == "" {
		ctype = mimeByExt(rel)
	}
	if ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	if download != "" {
		w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(download))
	}
	http.ServeContent(w, r, filepath.Base(p), fi.ModTime(), f)
}

// imageExt is the file extension for a picture's bytes ("" = not a picture we keep).
func imageExt(b []byte) string {
	switch http.DetectContentType(b) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	}
	return ""
}

// readPicture reads an uploaded picture (multipart "file", or the raw body).
func (s *Server) readPicture(r *http.Request) ([]byte, error) {
	var rd io.Reader = r.Body
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		f, _, err := r.FormFile("file")
		if err != nil {
			return nil, &userErr{"send the picture as the file"}
		}
		defer f.Close()
		rd = f
	}
	b, err := io.ReadAll(io.LimitReader(rd, 16<<20+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 16<<20 {
		return nil, &userErr{"a picture can be up to 16 MB"}
	}
	if imageExt(b) == "" {
		return nil, &userErr{"that is not a picture (PNG, JPEG, WebP)"}
	}
	return b, nil
}

// removeUserMedia drops a person's whole media folder (their account is gone).
func (s *Server) removeUserMedia(_ context.Context, uid int64) {
	if uid > 0 {
		_ = os.RemoveAll(filepath.Join(s.mediaRoot(), fmt.Sprint(uid)))
	}
}
