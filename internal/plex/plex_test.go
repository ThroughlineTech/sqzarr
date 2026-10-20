package plex

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVerify(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   int
		fail   bool
	}{
		{"libraries", 200, `<MediaContainer><Directory key="1"/><Directory key="2"/></MediaContainer>`, 2, false},
		{"empty", 200, `<MediaContainer/>`, 0, false},
		{"unauthorized", 401, "private-test-token", 0, true},
		{"forbidden", 403, "private-test-token", 0, true},
		{"server error", 500, "private-test-token", 0, true},
		{"html", 200, `<html/>`, 0, true},
		{"malformed", 200, `<MediaContainer>private-test-token`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/library/sections" || r.URL.RawQuery != "" || r.Header.Get("X-Plex-Token") != "private-test-token" {
					t.Error("incorrect request or token placement")
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			n, err := New(srv.URL, "private-test-token", slog.Default()).Verify(context.Background())
			if (err != nil) != tc.fail || n != tc.want {
				t.Fatalf("count=%d err=%v", n, err)
			}
			if err != nil && strings.Contains(err.Error(), "private-test-token") {
				t.Fatal("token leaked")
			}
		})
	}
}

func TestRedirectAndRefreshTokenSafety(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect followed") }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" || r.Header.Get("X-Plex-Token") != "test-token" {
			t.Error("unsafe token placement")
		}
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer srv.Close()
	c := New(srv.URL, "test-token", slog.Default())
	if _, err := c.Verify(context.Background()); err == nil {
		t.Fatal("accepted redirect")
	}
	if err := c.refreshSection("1"); err == nil {
		t.Fatal("accepted refresh redirect")
	}
}

func TestVerifyTimeoutAndInvalidURL(t *testing.T) {
	for _, raw := range []string{"file:///tmp/secret", "http://user:private-test-token@localhost", "http://localhost/?token=private-test-token"} {
		_, err := New(raw, "test-token", slog.Default()).Verify(context.Background())
		if err == nil || strings.Contains(err.Error(), "private-test-token") {
			t.Fatalf("unsafe result: %v", err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	c := New(srv.URL, "test-token", slog.Default())
	c.http.Timeout = 20 * time.Millisecond
	if _, err := c.Verify(context.Background()); err == nil {
		t.Fatal("expected timeout")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Verify(ctx); err == nil {
		t.Fatal("expected cancellation")
	}
}
