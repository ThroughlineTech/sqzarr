package api

import (
	"net/http"

	"github.com/danrichardson/sqzarr/internal/plex"
	"github.com/danrichardson/sqzarr/internal/queue"
)

// Caller holds s.mu so the worker uses the same settings as verification.
func (s *Server) updatePlexNotifier() {
	var notifier queue.PlexNotifier
	if s.cfg.Plex.Enabled {
		if client := plex.New(s.cfg.Plex.BaseURL, s.cfg.Plex.Token, s.log); client != nil {
			notifier = client
		}
	}
	s.worker.SetPlexNotifier(notifier)
}

// handleVerifyPlex uses saved credentials only; no token is returned to the browser.
func (s *Server) handleVerifyPlex(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := s.cfg.Plex
	s.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	if !cfg.Enabled {
		jsonError(w, "Enable and save the Plex integration before verifying", http.StatusBadRequest)
		return
	}
	client := plex.New(cfg.BaseURL, cfg.Token, s.log)
	if client == nil {
		jsonError(w, "Save a Plex URL and token before verifying", http.StatusBadRequest)
		return
	}
	count, err := client.Verify(r.Context())
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadGateway)
		return
	}
	jsonOK(w, map[string]any{"library_count": count})
}
