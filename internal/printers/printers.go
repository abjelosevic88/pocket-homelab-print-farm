// Package printers talks to 3D printers on the local network: Klipper
// through Moonraker, OctoPrint, and Prusa printers through PrusaLink. Each
// gives the same Status, takes a G-code file and starts, pauses, resumes or
// stops a print. A "manual" printer has no driver: the person says when a
// print starts and ends.
package printers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// States a printer can be in, the same words for every kind.
const (
	Idle     = "idle"     // ready, nothing printing
	Printing = "printing" // a print is going
	Paused   = "paused"
	Done     = "done"    // the last print finished (until the next starts)
	Stopped  = "stopped" // the last print was cancelled on the printer
	Error    = "error"   // the printer says something is wrong
	Busy     = "busy"    // heating, homing, a file being read
	Offline  = "offline"
)

// Status is what a printer says about itself.
type Status struct {
	State      string  `json:"state"`
	Message    string  `json:"message"`     // the printer's own words, when it has some
	File       string  `json:"file"`        // the file printing (or last printed)
	JobID      string  `json:"job_id"`      // the printer's id for this print, when it has one
	Progress   float64 `json:"progress"`    // 0..1
	Elapsed    int     `json:"elapsed"`     // seconds printing
	Left       int     `json:"left"`        // seconds left (0 = not known)
	FilamentMM float64 `json:"filament_mm"` // used so far
	Layer      int     `json:"layer"`
	Layers     int     `json:"layers"`
	Nozzle     float64 `json:"nozzle"`
	NozzleTo   float64 `json:"nozzle_target"`
	Bed        float64 `json:"bed"`
	BedTo      float64 `json:"bed_target"`
	Chamber    float64 `json:"chamber,omitempty"`
	Webcam     string  `json:"-"` // a snapshot address the printer knows
	Software   string  `json:"software"`
}

// Kinds are the connections that have a driver.
var Kinds = []string{"moonraker", "octoprint", "prusalink"}

// Driver is one printer's connection.
type Driver interface {
	Status(ctx context.Context) (Status, error)
	// Upload sends a G-code file; with start it begins printing it at once.
	Upload(ctx context.Context, name string, data io.Reader, size int64, start bool) error
	Start(ctx context.Context, name string) error
	Pause(ctx context.Context) error
	Resume(ctx context.Context) error
	Cancel(ctx context.Context) error
}

// Config is how to reach a printer.
type Config struct {
	Kind   string
	URL    string
	APIKey string
	HTTP   *http.Client
}

// ErrNoDriver means the printer is manual (or unknown).
var ErrNoDriver = errors.New("this printer is not connected: start and finish its prints by hand")

// New returns the driver for a connection.
func New(c Config) (Driver, error) {
	base := strings.TrimRight(strings.TrimSpace(c.URL), "/")
	if base == "" {
		return nil, ErrNoDriver
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 8 * time.Second}
	}
	h := &httpc{base: base, key: c.APIKey, hc: hc}
	switch c.Kind {
	case "moonraker":
		return &moonraker{h}, nil
	case "octoprint":
		return &octoprint{h}, nil
	case "prusalink":
		return &prusalink{h}, nil
	}
	return nil, ErrNoDriver
}

// ---------- HTTP ----------

type httpc struct {
	base, key string
	hc        *http.Client
}

// HTTPError is a printer answering with an error status.
type HTTPError struct {
	Code int
	Body string
}

func (e *HTTPError) Error() string {
	switch e.Code {
	case 401, 403:
		return "the printer refused the API key"
	case 404:
		return "the printer does not know that (404)"
	case 409:
		return "the printer is busy: " + firstNonEmpty(e.Body, "it cannot do that now")
	}
	return fmt.Sprintf("the printer answered %d %s", e.Code, e.Body)
}

func (h *httpc) req(ctx context.Context, method, p string, body io.Reader, ctype string, hdr map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, h.base+p, body)
	if err != nil {
		return nil, err
	}
	if h.key != "" {
		req.Header.Set("X-Api-Key", h.key)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := h.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the printer did not answer: %w", err)
	}
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 400))
		res.Body.Close()
		return nil, &HTTPError{Code: res.StatusCode, Body: strings.TrimSpace(stripTags(string(b)))}
	}
	return res, nil
}

func (h *httpc) json(ctx context.Context, method, p string, body any, v any) error {
	var rd io.Reader
	ctype := ""
	if body != nil {
		b, _ := json.Marshal(body)
		rd, ctype = bytes.NewReader(b), "application/json"
	}
	res, err := h.req(ctx, method, p, rd, ctype, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if v == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(v)
}

func (h *httpc) multipart(ctx context.Context, p, field, name string, data io.Reader, fields map[string]string) error {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		var err error
		defer func() { pw.CloseWithError(err) }()
		for k, v := range fields {
			if err = mw.WriteField(k, v); err != nil {
				return
			}
		}
		var fw io.Writer
		if fw, err = mw.CreateFormFile(field, name); err != nil {
			return
		}
		if _, err = io.Copy(fw, data); err != nil {
			return
		}
		err = mw.Close()
	}()
	up := &http.Client{Timeout: 30 * time.Minute, Transport: h.hc.Transport}
	hh := &httpc{base: h.base, key: h.key, hc: up}
	res, err := hh.req(ctx, http.MethodPost, p, pr, mw.FormDataContentType(), nil)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

// ---------- Moonraker (Klipper) ----------

type moonraker struct{ h *httpc }

func (m *moonraker) Status(ctx context.Context) (Status, error) {
	var out struct {
		Result struct {
			Status struct {
				PrintStats struct {
					State         string  `json:"state"`
					Filename      string  `json:"filename"`
					PrintDuration float64 `json:"print_duration"`
					TotalDuration float64 `json:"total_duration"`
					FilamentUsed  float64 `json:"filament_used"`
					Message       string  `json:"message"`
					Info          struct {
						CurrentLayer *int `json:"current_layer"`
						TotalLayer   *int `json:"total_layer"`
					} `json:"info"`
				} `json:"print_stats"`
				VirtualSD struct {
					Progress float64 `json:"progress"`
				} `json:"virtual_sdcard"`
				Display struct {
					Progress float64 `json:"progress"`
				} `json:"display_status"`
				Extruder struct {
					Temperature float64 `json:"temperature"`
					Target      float64 `json:"target"`
				} `json:"extruder"`
				Bed struct {
					Temperature float64 `json:"temperature"`
					Target      float64 `json:"target"`
				} `json:"heater_bed"`
				Webhooks struct {
					State        string `json:"state"`
					StateMessage string `json:"state_message"`
				} `json:"webhooks"`
			} `json:"status"`
		} `json:"result"`
	}
	if err := m.h.json(ctx, http.MethodGet, "/printer/objects/query?print_stats&virtual_sdcard&display_status&extruder&heater_bed&webhooks", nil, &out); err != nil {
		return Status{State: Offline, Message: err.Error()}, err
	}
	st := out.Result.Status
	s := Status{File: st.PrintStats.Filename, Elapsed: int(st.PrintStats.PrintDuration), FilamentMM: st.PrintStats.FilamentUsed,
		Nozzle: st.Extruder.Temperature, NozzleTo: st.Extruder.Target, Bed: st.Bed.Temperature, BedTo: st.Bed.Target, Software: "Klipper"}
	s.Progress = firstPos(st.Display.Progress, st.VirtualSD.Progress)
	if st.PrintStats.Info.CurrentLayer != nil {
		s.Layer = *st.PrintStats.Info.CurrentLayer
	}
	if st.PrintStats.Info.TotalLayer != nil {
		s.Layers = *st.PrintStats.Info.TotalLayer
	}
	switch st.PrintStats.State {
	case "printing":
		s.State = Printing
	case "paused":
		s.State = Paused
	case "complete":
		s.State, s.Progress = Done, 1
	case "cancelled":
		s.State = Stopped
	case "error":
		s.State, s.Message = Error, st.PrintStats.Message
	default:
		s.State = Idle
	}
	if st.Webhooks.State != "" && st.Webhooks.State != "ready" {
		s.State, s.Message = Error, firstNonEmpty(st.Webhooks.StateMessage, "Klipper is "+st.Webhooks.State)
		if st.Webhooks.State == "startup" {
			s.State = Busy
		}
	}
	if s.State == Printing && s.Progress > 0.01 && s.Elapsed > 0 {
		s.Left = int(float64(s.Elapsed)/s.Progress) - s.Elapsed
	}
	// the camera the printer knows
	var cams struct {
		Result struct {
			Webcams []struct {
				Enabled     *bool  `json:"enabled"`
				SnapshotURL string `json:"snapshot_url"`
			} `json:"webcams"`
		} `json:"result"`
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := m.h.json(cctx, http.MethodGet, "/server/webcams/list", nil, &cams); err == nil {
		for _, c := range cams.Result.Webcams {
			if c.SnapshotURL != "" && (c.Enabled == nil || *c.Enabled) {
				s.Webcam = absolute(m.h.base, c.SnapshotURL)
				break
			}
		}
	}
	return s, nil
}

func (m *moonraker) Upload(ctx context.Context, name string, data io.Reader, size int64, start bool) error {
	f := map[string]string{"root": "gcodes"}
	if start {
		f["print"] = "true"
	}
	return m.h.multipart(ctx, "/server/files/upload", "file", name, data, f)
}

func (m *moonraker) Start(ctx context.Context, name string) error {
	return m.h.json(ctx, http.MethodPost, "/printer/print/start?filename="+url.QueryEscape(name), nil, nil)
}
func (m *moonraker) Pause(ctx context.Context) error {
	return m.h.json(ctx, http.MethodPost, "/printer/print/pause", nil, nil)
}
func (m *moonraker) Resume(ctx context.Context) error {
	return m.h.json(ctx, http.MethodPost, "/printer/print/resume", nil, nil)
}
func (m *moonraker) Cancel(ctx context.Context) error {
	return m.h.json(ctx, http.MethodPost, "/printer/print/cancel", nil, nil)
}

// ---------- OctoPrint ----------

type octoprint struct{ h *httpc }

func (o *octoprint) Status(ctx context.Context) (Status, error) {
	var job struct {
		State string `json:"state"`
		Error string `json:"error"`
		Job   struct {
			File struct {
				Name    string `json:"name"`
				Display string `json:"display"`
			} `json:"file"`
			Filament map[string]struct {
				Length float64 `json:"length"`
			} `json:"filament"`
		} `json:"job"`
		Progress struct {
			Completion    *float64 `json:"completion"`
			PrintTime     *float64 `json:"printTime"`
			PrintTimeLeft *float64 `json:"printTimeLeft"`
		} `json:"progress"`
	}
	if err := o.h.json(ctx, http.MethodGet, "/api/job", nil, &job); err != nil {
		return Status{State: Offline, Message: err.Error()}, err
	}
	s := Status{File: firstNonEmpty(job.Job.File.Name, job.Job.File.Display), Software: "OctoPrint", Message: job.Error}
	if job.Progress.Completion != nil {
		s.Progress = *job.Progress.Completion / 100
	}
	if job.Progress.PrintTime != nil {
		s.Elapsed = int(*job.Progress.PrintTime)
	}
	if job.Progress.PrintTimeLeft != nil {
		s.Left = int(*job.Progress.PrintTimeLeft)
	}
	st := strings.ToLower(job.State)
	switch {
	case strings.HasPrefix(st, "printing"), st == "starting", strings.HasPrefix(st, "sending"):
		s.State = Printing
	case strings.HasPrefix(st, "paus"), st == "resuming":
		s.State = Paused
	case st == "cancelling":
		s.State = Busy
	case strings.HasPrefix(st, "offline"), st == "closed", strings.Contains(st, "connect"):
		s.State = Offline
		if job.Error != "" || strings.Contains(st, "error") {
			s.State = Error
		}
	case strings.Contains(st, "error"):
		s.State = Error
	case st == "operational", st == "ready":
		s.State = Idle
		if s.Progress >= 0.999 && s.File != "" {
			s.State = Done
		}
	default:
		s.State = Busy
	}
	if s.State == Offline {
		s.Message = firstNonEmpty(s.Message, "OctoPrint is not connected to the printer")
		return s, nil
	}
	var pr struct {
		Temperature map[string]struct {
			Actual *float64 `json:"actual"`
			Target *float64 `json:"target"`
		} `json:"temperature"`
	}
	if err := o.h.json(ctx, http.MethodGet, "/api/printer?history=false", nil, &pr); err == nil {
		get := func(k string) (float64, float64) {
			t := pr.Temperature[k]
			var a, b float64
			if t.Actual != nil {
				a = *t.Actual
			}
			if t.Target != nil {
				b = *t.Target
			}
			return a, b
		}
		s.Nozzle, s.NozzleTo = get("tool0")
		s.Bed, s.BedTo = get("bed")
		s.Chamber, _ = get("chamber")
	}
	return s, nil
}

func (o *octoprint) Upload(ctx context.Context, name string, data io.Reader, size int64, start bool) error {
	f := map[string]string{}
	if start {
		f["select"], f["print"] = "true", "true"
	}
	return o.h.multipart(ctx, "/api/files/local", "file", name, data, f)
}

func (o *octoprint) Start(ctx context.Context, name string) error {
	if err := o.h.json(ctx, http.MethodPost, "/api/files/local/"+escapePath(name), map[string]any{"command": "select", "print": true}, nil); err != nil {
		return err
	}
	return nil
}
func (o *octoprint) Pause(ctx context.Context) error {
	return o.h.json(ctx, http.MethodPost, "/api/job", map[string]any{"command": "pause", "action": "pause"}, nil)
}
func (o *octoprint) Resume(ctx context.Context) error {
	return o.h.json(ctx, http.MethodPost, "/api/job", map[string]any{"command": "pause", "action": "resume"}, nil)
}
func (o *octoprint) Cancel(ctx context.Context) error {
	return o.h.json(ctx, http.MethodPost, "/api/job", map[string]any{"command": "cancel"}, nil)
}

// ---------- PrusaLink ----------

type prusalink struct{ h *httpc }

type prusaStatus struct {
	Job *struct {
		ID            int     `json:"id"`
		Progress      float64 `json:"progress"`
		TimeRemaining int     `json:"time_remaining"`
		TimePrinting  int     `json:"time_printing"`
	} `json:"job"`
	Printer struct {
		State        string  `json:"state"`
		TempNozzle   float64 `json:"temp_nozzle"`
		TargetNozzle float64 `json:"target_nozzle"`
		TempBed      float64 `json:"temp_bed"`
		TargetBed    float64 `json:"target_bed"`
	} `json:"printer"`
}

func (p *prusalink) status(ctx context.Context) (prusaStatus, error) {
	var st prusaStatus
	err := p.h.json(ctx, http.MethodGet, "/api/v1/status", nil, &st)
	return st, err
}

func (p *prusalink) Status(ctx context.Context) (Status, error) {
	st, err := p.status(ctx)
	if err != nil {
		return Status{State: Offline, Message: err.Error()}, err
	}
	s := Status{Software: "PrusaLink", Nozzle: st.Printer.TempNozzle, NozzleTo: st.Printer.TargetNozzle, Bed: st.Printer.TempBed, BedTo: st.Printer.TargetBed}
	switch strings.ToUpper(st.Printer.State) {
	case "PRINTING":
		s.State = Printing
	case "PAUSED":
		s.State = Paused
	case "FINISHED":
		s.State, s.Progress = Done, 1
	case "STOPPED":
		s.State = Stopped
	case "ERROR":
		s.State = Error
	case "ATTENTION":
		s.State, s.Message = Error, "the printer needs you (look at its screen)"
	case "BUSY":
		s.State = Busy
	default:
		s.State = Idle
	}
	if st.Job != nil {
		s.JobID = fmt.Sprint(st.Job.ID)
		s.Progress = st.Job.Progress / 100
		s.Elapsed, s.Left = st.Job.TimePrinting, st.Job.TimeRemaining
		var job struct {
			File struct {
				Name        string `json:"name"`
				DisplayName string `json:"display_name"`
			} `json:"file"`
		}
		if err := p.h.json(ctx, http.MethodGet, "/api/v1/job", nil, &job); err == nil {
			s.File = firstNonEmpty(job.File.DisplayName, job.File.Name)
		}
	}
	return s, nil
}

func (p *prusalink) Upload(ctx context.Context, name string, data io.Reader, size int64, start bool) error {
	hdr := map[string]string{"Overwrite": "?1"}
	if start {
		hdr["Print-After-Upload"] = "?1"
	}
	up := &httpc{base: p.h.base, key: p.h.key, hc: &http.Client{Timeout: 30 * time.Minute, Transport: p.h.hc.Transport}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, up.base+"/api/v1/files/usb/"+escapePath(name), data)
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "text/x.gcode")
	if up.key != "" {
		req.Header.Set("X-Api-Key", up.key)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := up.hc.Do(req)
	if err != nil {
		return fmt.Errorf("the printer did not answer: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 400))
		return &HTTPError{Code: res.StatusCode, Body: strings.TrimSpace(stripTags(string(b)))}
	}
	return nil
}

func (p *prusalink) Start(ctx context.Context, name string) error {
	return p.h.json(ctx, http.MethodPost, "/api/v1/files/usb/"+escapePath(name), nil, nil)
}

func (p *prusalink) job(ctx context.Context) (int, error) {
	st, err := p.status(ctx)
	if err != nil {
		return 0, err
	}
	if st.Job == nil {
		return 0, errors.New("nothing is printing")
	}
	return st.Job.ID, nil
}

func (p *prusalink) Pause(ctx context.Context) error {
	id, err := p.job(ctx)
	if err != nil {
		return err
	}
	return p.h.json(ctx, http.MethodPut, fmt.Sprintf("/api/v1/job/%d/pause", id), nil, nil)
}
func (p *prusalink) Resume(ctx context.Context) error {
	id, err := p.job(ctx)
	if err != nil {
		return err
	}
	return p.h.json(ctx, http.MethodPut, fmt.Sprintf("/api/v1/job/%d/resume", id), nil, nil)
}
func (p *prusalink) Cancel(ctx context.Context) error {
	id, err := p.job(ctx)
	if err != nil {
		return err
	}
	return p.h.json(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/job/%d", id), nil, nil)
}

// ---------- the camera ----------

// Snapshot fetches one picture from a camera address (a JPEG snapshot URL;
// an MJPEG stream gives its first frame).
func Snapshot(ctx context.Context, hc *http.Client, addr, apiKey string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return nil, "", err
	}
	if apiKey != "" {
		req.Header.Set("X-Api-Key", apiKey)
	}
	res, err := hc.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("the camera did not answer: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return nil, "", &HTTPError{Code: res.StatusCode}
	}
	ct := res.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/") {
		return firstFrame(res.Body)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 12<<20))
	if err != nil {
		return nil, "", err
	}
	if ct == "" || !strings.HasPrefix(ct, "image/") {
		ct = http.DetectContentType(b)
	}
	if !strings.HasPrefix(ct, "image/") {
		return nil, "", errors.New("that address does not give a picture")
	}
	return b, ct, nil
}

// firstFrame reads the first JPEG out of an MJPEG stream.
func firstFrame(r io.Reader) ([]byte, string, error) {
	buf := make([]byte, 0, 256<<10)
	tmp := make([]byte, 32<<10)
	start := -1
	for len(buf) < 12<<20 {
		n, err := r.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if start < 0 {
			start = bytes.Index(buf, []byte{0xFF, 0xD8})
		}
		if start >= 0 {
			if end := bytes.Index(buf[start+2:], []byte{0xFF, 0xD9}); end >= 0 {
				return append([]byte(nil), buf[start:start+2+end+2]...), "image/jpeg", nil
			}
		}
		if err != nil {
			break
		}
	}
	return nil, "", errors.New("no picture in the camera stream")
}

// ---------- small helpers ----------

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func firstPos(v ...float64) float64 {
	for _, x := range v {
		if x > 0 {
			return x
		}
	}
	return 0
}

func absolute(base, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	u, err := url.Parse(base)
	if err != nil {
		return ref
	}
	if !strings.HasPrefix(ref, "/") {
		ref = "/" + ref
	}
	return u.Scheme + "://" + u.Host + ref
}

func escapePath(name string) string {
	parts := strings.Split(path.Clean("/" + name)[1:], "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func stripTags(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
