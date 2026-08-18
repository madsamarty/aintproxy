package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/mohamed-sameh/aintproxy/internal/config"
	"github.com/mohamed-sameh/aintproxy/internal/rotation"
)

// Rotator is the interface the server needs from a rotation engine.
type Rotator interface {
	Rotate() (oldIP, newIP string, err error)
	HardRotate() ([]rotation.RotateResult, error)
	Info() (*rotation.InfoResult, error)
}

type Server struct {
	http        *http.Server
	rotator     Rotator
	config      *config.Config
	logger      *slog.Logger
	mu          sync.Mutex
	lastRotate  time.Time
	startedAt   time.Time
	driverName  string
	history     *History
}

func New(cfg *config.Config, logger *slog.Logger) (*Server, error) {
	return NewWithDriver(cfg, logger, "")
}

func NewWithDriver(cfg *config.Config, logger *slog.Logger, driverName string) (*Server, error) {
	rot, err := rotation.New(cfg, logger)
	if err != nil {
		return nil, err
	}

	s := &Server{
		config:     cfg,
		logger:     logger,
		rotator:    rot,
		startedAt:  time.Now(),
		driverName: driverName,
		history:    NewHistory(100),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /rotate", s.handleRotate)
	mux.HandleFunc("POST /hard-rotate", s.handleHardRotate)
	mux.HandleFunc("GET /info", s.handleInfo)
	mux.HandleFunc("GET /history", s.handleHistory)
	mux.HandleFunc("GET /metrics", s.handleMetrics)

	s.http = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler: mux,
	}

	return s, nil
}

func (s *Server) Start() error {
	s.logger.Info("Rotation service listening",
		"addr", s.http.Addr)
	return s.http.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ip, _ := s.rotator.Info()
	publicIP := ""
	if ip != nil {
		publicIP = ip.PublicIP
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"uptime":       time.Since(s.startedAt).String(),
		"started_at":   s.startedAt.UTC().Format(time.RFC3339),
		"current_ip":   publicIP,
		"driver":       s.driverName,
		"last_rotate":  s.lastRotate.Format(time.RFC3339),
		"history_size": s.history.Len(),
	})
}

func (s *Server) handleRotate(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.lastRotate.IsZero() && time.Since(s.lastRotate) < 30*time.Second {
		remaining := 30*time.Second - time.Since(s.lastRotate)
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":      "rate limited",
			"retry_after": remaining.Seconds(),
		})
		return
	}

	oldIP, newIP, err := s.rotator.Rotate()
	s.lastRotate = time.Now()

	if err != nil {
		s.logger.Error("Rotation failed", "error", err)
		s.history.Add(rotation.RotateResult{
			OldIP: oldIP,
			Err:   err,
		})
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	result := rotation.RotateResult{
		OldIP: oldIP,
		NewIP: newIP,
	}
	s.history.Add(result)

	writeJSON(w, http.StatusOK, map[string]any{
		"old_ip":  oldIP,
		"new_ip":  newIP,
		"rotated": oldIP != newIP,
	})
}

func (s *Server) handleHardRotate(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.lastRotate.IsZero() && time.Since(s.lastRotate) < 30*time.Second {
		remaining := 30*time.Second - time.Since(s.lastRotate)
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":      "rate limited",
			"retry_after": remaining.Seconds(),
		})
		return
	}

	results, err := s.rotator.HardRotate()
	s.lastRotate = time.Now()

	for _, r := range results {
		s.history.Add(r)
	}

	if err != nil {
		s.logger.Error("Hard rotate failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	info, err := s.rotator.Info()
	if err != nil {
		s.logger.Error("Info failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"history": s.history.All(),
		"total":   s.history.Len(),
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	all := s.history.All()
	totalRotations := len(all)
	successful := 0
	failed := 0
	for _, h := range all {
		if h.Err != nil {
			failed++
		} else {
			successful++
		}
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP aintproxy_rotations_total Total rotation attempts\n")
	fmt.Fprintf(w, "# TYPE aintproxy_rotations_total counter\n")
	fmt.Fprintf(w, "aintproxy_rotations_total %d\n", totalRotations)
	fmt.Fprintf(w, "# HELP aintproxy_rotations_successful Successful rotations\n")
	fmt.Fprintf(w, "# TYPE aintproxy_rotations_successful counter\n")
	fmt.Fprintf(w, "aintproxy_rotations_successful %d\n", successful)
	fmt.Fprintf(w, "# HELP aintproxy_rotations_failed Failed rotations\n")
	fmt.Fprintf(w, "# TYPE aintproxy_rotations_failed counter\n")
	fmt.Fprintf(w, "aintproxy_rotations_failed %d\n", failed)
	fmt.Fprintf(w, "# HELP aintproxy_uptime_seconds Server uptime in seconds\n")
	fmt.Fprintf(w, "# TYPE aintproxy_uptime_seconds gauge\n")
	fmt.Fprintf(w, "aintproxy_uptime_seconds %.2f\n", time.Since(s.startedAt).Seconds())
}

func (s *Server) authorized(r *http.Request) bool {
	expected := s.config.Server.AuthToken
	if expected == "" {
		return true
	}
	supplied := r.Header.Get("X-Auth-Token")
	return subtle.ConstantTimeCompare([]byte(supplied), []byte(expected)) == 1
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
