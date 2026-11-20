package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-print-farm/internal/store"
)

// Router builds the chi router.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	if s.cfg.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.RequestID)
	r.Use(s.logging)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	r.Get("/pocket-ask.js", s.core.AskWidget("printfarm"))
	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)
	r.Get("/api/health", s.handleHealth)
	if s.cfg.MetricsEnabled {
		r.Get("/metrics", s.handleMetrics)
	}

	r.Route("/api/v1", func(r chi.Router) {
		// uploads and file downloads: no time limit
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/models/upload", s.handleUpload)
			r.Post("/models/link", s.handleAddLink)
			r.Post("/apps/files/import", s.handleFromFiles)
			r.Get("/files/{id}/download", s.handleFileDownload)
			r.Get("/files/{id}/mesh", s.handleFileMesh)
			r.Post("/jobs/{id}/start", s.handleStartJob)
			r.Post("/prints", s.handleCreatePrint)
			r.Post("/import", s.handleImport)
			r.Get("/export.json", s.handleExportJSON)
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(120 * time.Second))
			r.Post("/auth/login", s.handleLogin)
			r.Post("/auth/setup", s.handleSetup)
			r.Get("/auth/status", s.handleAuthStatus)
			r.Get("/auth/oidc/start", s.handleOIDCStart)
			r.Get("/auth/oidc/callback", s.handleOIDCCallback)

			r.Group(func(r chi.Router) {
				r.Use(s.requireAuth)
				r.Post("/auth/logout", s.handleLogout)
				r.Get("/auth/me", s.handleMe)
				r.Put("/auth/me", s.handleUpdateMe)

				// for the person, and for the rest of the box: Pocket Day, Search, AI
				r.Get("/home", s.handleHome)
				r.Get("/farm", s.handleFarm)
				r.Get("/summary", s.handleSummary)
				r.Get("/day/{date}", s.handleDay)
				r.Get("/search", s.handleSearch)
				r.Get("/stats", s.handleStats)

				// printers
				r.Get("/printers", s.handlePrinters)
				r.Post("/printers", s.handleCreatePrinter)
				r.Post("/printers/test", s.handleTestPrinter)
				r.Put("/printers/order", s.handleOrderPrinters)
				r.Get("/printers/{id}", s.handleGetPrinter)
				r.Put("/printers/{id}", s.handleUpdatePrinter)
				r.Delete("/printers/{id}", s.handleDeletePrinter)
				r.Post("/printers/{id}/control", s.handleControl)
				r.Put("/printers/{id}/spool", s.handleLoadSpool)
				r.Get("/printers/{id}/snapshot", s.handleSnapshot)
				r.Get("/printers/{id}/photo", s.handlePrinterPhoto)
				r.Post("/printers/{id}/photo", s.handlePrinterPhoto)
				r.Delete("/printers/{id}/photo", s.handlePrinterPhoto)

				// the queue
				r.Get("/jobs", s.handleJobs)
				r.Post("/jobs", s.handleCreateJob)
				r.Put("/jobs/order", s.handleOrderJobs)
				r.Get("/jobs/{id}", s.handleGetJob)
				r.Put("/jobs/{id}", s.handleUpdateJob)
				r.Delete("/jobs/{id}", s.handleDeleteJob)
				r.Post("/jobs/{id}/invoice", s.handleJobInvoice)
				r.Post("/jobs/{id}/task", s.handleJobTask)

				// prints
				r.Get("/prints", s.handlePrints)
				r.Get("/prints/{id}", s.handleGetPrint)
				r.Put("/prints/{id}", s.handleUpdatePrint)
				r.Delete("/prints/{id}", s.handleDeletePrint)
				r.Post("/prints/{id}/finish", s.handleFinishPrint)
				r.Get("/prints/{id}/photo", s.handlePrintPhoto)
				r.Post("/prints/{id}/photo", s.handlePrintPhoto)
				r.Delete("/prints/{id}/photo", s.handlePrintPhoto)
				r.Post("/prints/{id}/inventory", s.handlePrintToInventory)
				r.Post("/prints/{id}/calendar", s.handlePrintCalendar)
				r.Post("/prints/{id}/chat", s.handlePrintChat)
				r.Post("/prints/{id}/issue", s.handlePrintIssue)
				r.Post("/prints/{id}/files", s.handlePrintPhotoToFiles)
				r.Post("/prints/{id}/diagnose", s.handleAIDiagnose)

				// models
				r.Get("/models", s.handleModels)
				r.Post("/models", s.handleCreateModel)
				r.Get("/models/tags", s.handleModelTags)
				r.Get("/models/{id}", s.handleGetModel)
				r.Put("/models/{id}", s.handleUpdateModel)
				r.Delete("/models/{id}", s.handleDeleteModel)
				r.Get("/models/{id}/cover", s.handleModelCover)
				r.Post("/models/{id}/cover", s.handleModelCover)
				r.Delete("/models/{id}/cover", s.handleModelCover)
				r.Post("/models/{id}/settings", s.handleAISettings)
				r.Put("/files/{id}", s.handleUpdateFile)
				r.Delete("/files/{id}", s.handleDeleteFile)
				r.Get("/files/{id}/thumb", s.handleFileThumb)
				r.Post("/files/{id}/thumb", s.handleFileThumb)
				r.Post("/files/{id}/files", s.handleFileToFiles)

				// filament
				r.Get("/filaments", s.handleFilaments)
				r.Post("/filaments", s.handleCreateFilament)
				r.Get("/filaments/{id}", s.handleGetFilament)
				r.Put("/filaments/{id}", s.handleUpdateFilament)
				r.Delete("/filaments/{id}", s.handleDeleteFilament)
				r.Get("/spools", s.handleSpools)
				r.Post("/spools", s.handleCreateSpool)
				r.Get("/spools/{id}", s.handleGetSpool)
				r.Put("/spools/{id}", s.handleUpdateSpool)
				r.Delete("/spools/{id}", s.handleDeleteSpool)
				r.Post("/spools/{id}/weigh", s.handleWeighSpool)
				r.Post("/spools/{id}/money", s.handleSpoolToMoney)
				r.Post("/import/spoolman", s.handleSpoolmanImport)

				// upkeep
				r.Get("/upkeep", s.handleUpkeep)
				r.Post("/upkeep", s.handleCreateUpkeep)
				r.Put("/upkeep/{id}", s.handleUpdateUpkeep)
				r.Delete("/upkeep/{id}", s.handleDeleteUpkeep)
				r.Post("/upkeep/done", s.handleUpkeepDone)
				r.Delete("/upkeep/log/{id}", s.handleDeleteUpkeepLog)

				// the other Pocket apps
				r.Get("/apps", s.handleApps)
				r.Get("/apps/files", s.handleFilesList)
				r.Get("/apps/inventory/locations", s.handleInventoryLocations)
				r.Get("/apps/money", s.handleMoneyAccounts)
				r.Get("/apps/invoicing/clients", s.handleInvoiceClients)
				r.Get("/apps/contacts", s.handleContacts)
				r.Get("/apps/tasks/lists", s.handleTaskLists)
				r.Get("/apps/chats", s.handleChats)
				r.Get("/apps/parcels", s.handleParcels)
				r.Get("/apps/home/things", s.handleHomeThings)

				// AI
				r.Get("/ai/status", s.handleAIStatus)
				r.Get("/ai/models", s.handleAIModels)

				r.Post("/integrations/test", s.handleIntegrationTest)
				r.Post("/notify/test", s.handleNotifyTest)

				r.Get("/users", s.handleListUsers)
				r.Post("/users", s.handleCreateUser)
				r.Put("/users/{id}", s.handleUpdateUser)
				r.Delete("/users/{id}", s.handleDeleteUser)

				r.Get("/settings", s.handleGetSettings)
				r.Put("/settings", s.handlePutSettings)
				r.Get("/settings/system", s.handleSystemInfo)

				r.Get("/tokens", s.handleListTokens)
				r.Post("/tokens", s.handleCreateToken)
				r.Delete("/tokens/{id}", s.handleDeleteToken)

				r.Get("/backup", s.handleBackup)
				r.Get("/export.csv", s.handleExportCSV)
				r.Get(strings.TrimPrefix(core.PortablePath, "/api/v1"), portable.Handler())
			})
		})
	})

	r.NotFound(s.handleSPA)
	return r
}

// ---------- helpers ----------

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.requests.Add(1)
		if ww.Status() >= 500 {
			s.errors.Add(1)
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			return
		}
		s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "dur", time.Since(start).String(), "ip", r.RemoteAddr)
	})
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

func (s *Server) fail(w http.ResponseWriter, err error, context string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, store.ErrConflict), errors.Is(err, core.ErrOperationConflict):
		writeErr(w, http.StatusConflict, "already exists")
		return
	}
	s.log.Error(context, "err", err)
	writeErr(w, http.StatusInternalServerError, context+": "+err.Error())
}

func (s *Server) readBody(r *http.Request) ([]byte, error) {
	return io.ReadAll(http.MaxBytesReader(nil, r.Body, s.cfg.MaxBodyBytes))
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 32<<20))
	return dec.Decode(v)
}

// decodeKeys decodes JSON into v and also reports which top-level keys were
// present, so PUT handlers can tell "not sent" from "sent empty".
func decodeKeys(r *http.Request, v any) (map[string]bool, error) {
	b, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(raw))
	for k := range raw {
		keys[k] = true
	}
	return keys, json.Unmarshal(b, v)
}

func idParam(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return n
}

func queryInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(name))
	return n
}

func queryBool(r *http.Request, name string) bool {
	v := strings.ToLower(r.URL.Query().Get(name))
	return v == "1" || v == "true" || v == "yes"
}

func validDate(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// ---------- health / metrics ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"core_outbox": s.coreOutboxHealth(r.Context()), "status": "ok", "version": Version, "uptime": time.Since(s.started).Round(time.Second).String()})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DB.PingContext(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts := s.store.Counts(ctx)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP pocket_printfarm_info Build info.\n# TYPE pocket_printfarm_info gauge\npocket_printfarm_info{version=%q} 1\n", Version)
	fmt.Fprintf(w, "# HELP pocket_printfarm_uptime_seconds Process uptime.\n# TYPE pocket_printfarm_uptime_seconds gauge\npocket_printfarm_uptime_seconds %d\n", int(time.Since(s.started).Seconds()))
	fmt.Fprintf(w, "# HELP pocket_printfarm_http_requests_total HTTP requests served.\n# TYPE pocket_printfarm_http_requests_total counter\npocket_printfarm_http_requests_total %d\n", s.requests.Load())
	fmt.Fprintf(w, "# HELP pocket_printfarm_http_errors_total HTTP 5xx responses.\n# TYPE pocket_printfarm_http_errors_total counter\npocket_printfarm_http_errors_total %d\n", s.errors.Load())
	fmt.Fprintf(w, "# HELP pocket_printfarm_rows Rows per table.\n# TYPE pocket_printfarm_rows gauge\n")
	for k, v := range counts {
		fmt.Fprintf(w, "pocket_printfarm_rows{table=%q} %d\n", k, v)
	}
}

// ---------- static / SPA ----------

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if s.webFS == nil {
		http.Error(w, "web UI not built (run `make web`)", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if f, err := s.webFS.Open(path); err == nil {
		f.Close()
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.FileServerFS(s.webFS).ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	r.URL.Path = "/"
	http.FileServerFS(s.webFS).ServeHTTP(w, r)
}
