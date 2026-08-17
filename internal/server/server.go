package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/mohamed-sameh/aintproxy/internal/config"
	"github.com/mohamed-sameh/aintproxy/internal/rotation"
)

// Rotator is the interface the server needs from a rotation engine.
type Rotator interface {
	Rotate() (oldIP, newIP string, err error)
}

type Server struct {
	http    *http.Server
	rotator Rotator
	config  *config.Config
	logger  *slog.Logger
	mu      sync.Mutex
}

func New(cfg *config.Config, logger *slog.Logger) *Server {
	s := &Server{
		config:  cfg,
		logger:  logger,
		rotator: rotation.New(cfg, logger),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /rotate", s.handleRotate)

	s.http = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler: mux,
	}

	return s
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRotate(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	oldIP, newIP, err := s.rotator.Rotate()
	if err != nil {
		s.logger.Error("Rotation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"old_ip":  oldIP,
		"new_ip":  newIP,
		"rotated": oldIP != newIP,
	})
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
