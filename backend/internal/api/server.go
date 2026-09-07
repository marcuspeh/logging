package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// Server is the HTTP front-end for queries.
type Server struct {
	loader *IndexLoader
	engine *Engine
	logger *slog.Logger
}

// NewServer wires the HTTP routes against the given loader.
func NewServer(loader *IndexLoader, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		loader: loader,
		engine: NewEngine(loader),
		logger: logger,
	}
}

// Router returns the configured HTTP handler.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", s.handleHealth)
	r.Get("/query", s.handleQuery)
	r.Get("/files", s.handleFiles)
	return r
}

// Run starts the HTTP server on addr and blocks until ctx is cancelled,
// then performs a graceful shutdown bounded by shutdownGrace.
func (s *Server) Run(ctx context.Context, addr string, shutdownGrace time.Duration) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Router(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("api listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		s.logger.Info("api shutting down", "addr", addr)
		return srv.Shutdown(shutdownCtx)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// fileEntry is the JSON shape for /files.
type fileEntry struct {
	File      string    `json:"file"`
	Started   time.Time `json:"started"`
	Ended     time.Time `json:"ended"`
	RowCount  int64     `json:"row_count"`
	Projects  []string  `json:"projects"`
	SizeBytes int64     `json:"size_bytes"`
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	entries := s.loader.Entries()
	out := make([]fileEntry, 0, len(entries))
	for _, e := range entries {
		idx := e.Index
		out = append(out, fileEntry{
			File:      idx.File,
			Started:   idx.Started,
			Ended:     idx.Ended,
			RowCount:  idx.RowCount,
			Projects:  idx.Projects,
			SizeBytes: idx.SizeBytes,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	q, err := parseQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	resp, err := s.engine.Execute(q)
	if err != nil {
		s.logger.Error("query failed", "err", err)
		writeError(w, http.StatusInternalServerError, fmt.Errorf("query: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// parseQuery decodes query params into a Query. Returns an error when
// neither project nor logid is set, or when a parameter is malformed.
func parseQuery(r *http.Request) (Query, error) {
	q := Query{Order: OrderDesc, Limit: 100}
	q.Project = r.URL.Query().Get("project")
	q.LogID = r.URL.Query().Get("logid")
	q.Level = r.URL.Query().Get("level")

	if q.Project == "" && q.LogID == "" {
		return q, errors.New("either 'project' or 'logid' query parameter is required")
	}

	if v := r.URL.Query().Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return q, fmt.Errorf("'from' must be RFC3339: %w", err)
		}
		q.From = t
	}
	if v := r.URL.Query().Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return q, fmt.Errorf("'to' must be RFC3339: %w", err)
		}
		q.To = t
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return q, fmt.Errorf("'limit' must be a positive integer")
		}
		q.Limit = n
	}
	if v := r.URL.Query().Get("order"); v != "" {
		switch v {
		case "asc":
			q.Order = OrderAsc
		case "desc":
			q.Order = OrderDesc
		default:
			return q, fmt.Errorf("'order' must be 'asc' or 'desc'")
		}
	}
	return q, nil
}

func writeJSON(w http.ResponseWriter, code int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
