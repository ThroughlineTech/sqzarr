package api

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danrichardson/sqzarr/internal/config"
)

func TestVerifyPlexUpstreamFailureIsSanitized(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "test-only-private-upstream-body")
	}))
	defer upstream.Close()
	cfg := config.Defaults()
	cfg.Plex.Enabled = true
	cfg.Plex.BaseURL = upstream.URL
	cfg.Plex.Token = "test-only-private-saved-token"
	var logs bytes.Buffer
	s := &Server{cfg: cfg, log: slog.New(slog.NewTextHandler(&logs, nil))}
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/plex/verify", nil))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("Plex auth failure must be 502, got %d", w.Code)
	}
	for _, secret := range []string{cfg.Plex.Token, "test-only-private-upstream-body", upstream.URL} {
		if strings.Contains(w.Body.String()+logs.String(), secret) {
			t.Fatal("upstream details leaked to response or log")
		}
	}
}

func TestVerifyPlexEndpoint(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "test-only-saved-token" {
			t.Error("saved token not used")
		}
		fmt.Fprint(w, `<MediaContainer><Directory key="1"/></MediaContainer>`)
	}))
	defer upstream.Close()
	for _, tc := range []struct {
		name                 string
		enabled, token, auth bool
		status               int
	}{
		{"success", true, true, false, 200},
		{"disabled", false, true, false, 400},
		{"missing token", true, false, false, 400},
		{"auth required", true, true, true, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Plex.Enabled = tc.enabled
			cfg.Plex.BaseURL = upstream.URL
			if tc.token {
				cfg.Plex.Token = "test-only-saved-token"
			}
			if tc.auth {
				cfg.Auth.PasswordHash = "test-only-hash"
			}
			s := &Server{cfg: cfg, log: slog.Default()}
			mux := http.NewServeMux()
			s.registerRoutes(mux)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/plex/verify", nil))
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), cfg.Plex.Token) && cfg.Plex.Token != "" {
				t.Fatal("token leaked")
			}
			if tc.status == 200 && !strings.Contains(w.Body.String(), `"library_count":1`) {
				t.Fatal("missing library count")
			}
		})
	}
}
