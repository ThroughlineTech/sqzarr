package api

import (
	"bufio"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danrichardson/sqzarr/internal/config"
	"github.com/danrichardson/sqzarr/internal/db"
	"github.com/danrichardson/sqzarr/internal/queue"
	"github.com/danrichardson/sqzarr/internal/scanner"
	"github.com/danrichardson/sqzarr/internal/transcoder"
)

// Scheduler is the interface the Server uses to read/update scan timing.
type Scheduler interface {
	NextScanAt() time.Time
	LastScanAt() *time.Time
	SetInterval(hours int)
	RecordManualScan()
}

//go:embed frontend_dist
var frontendFS embed.FS

// sysSample holds the latest CPU and GPU readings.
type sysSample struct {
	mu         sync.RWMutex
	CPUPercent float64
	GPUMHz     int
	GPUPercent float64 // -1 if unavailable (fall back to MHz display)
}

// Server is the HTTP API server.
type Server struct {
	cfg               *config.Config
	cfgPath           string
	db                *db.DB
	worker            *queue.Worker
	scanner           *scanner.Scanner
	sched             Scheduler
	encoder           *transcoder.Encoder
	availableEncoders []*transcoder.Encoder
	transcoder        *transcoder.Transcoder
	hub               *wsHub
	log               *slog.Logger
	logPath           string
	httpServer        *http.Server
	sys               sysSample
	mu                sync.Mutex // guards encoder hot-swap
}

// New creates a Server.
func New(
	cfg *config.Config,
	cfgPath string,
	database *db.DB,
	w *queue.Worker,
	s *scanner.Scanner,
	sched Scheduler,
	enc *transcoder.Encoder,
	allEncoders []*transcoder.Encoder,
	tc *transcoder.Transcoder,
	log *slog.Logger,
) *Server {
	srv := &Server{
		cfg:               cfg,
		cfgPath:           cfgPath,
		db:                database,
		worker:            w,
		scanner:           s,
		sched:             sched,
		encoder:           enc,
		availableEncoders: allEncoders,
		transcoder:        tc,
		hub:               newWSHub(),
		log:               log,
		logPath:           filepath.Join(cfg.Server.DataDir, "sqzarr.log"),
	}

	// Subscribe worker events to WebSocket hub; also persist auto-pause to config.
	w.Subscribe(func(e queue.Event) {
		srv.hub.broadcast(e)
		if e.Type == queue.EventPaused {
			srv.persistPaused(true)
		}
	})

	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	srv.httpServer = &http.Server{
		Addr:         cfg.Addr(),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // 0 = no timeout for streaming/WebSocket
		IdleTimeout:  120 * time.Second,
	}
	return srv
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	api := http.NewServeMux()

	// Status
	api.HandleFunc("GET /status", s.handleGetStatus)

	// Jobs
	api.HandleFunc("GET /jobs", s.handleListJobs)
	api.HandleFunc("POST /jobs", s.handleCreateJob)
	api.HandleFunc("POST /jobs/clear", s.handleClearHistory)
	api.HandleFunc("POST /jobs/enqueue-dir", s.handleEnqueueDir)
	api.HandleFunc("GET /jobs/savings", s.handleListSavings)
	api.HandleFunc("GET /jobs/{id}", s.handleGetJob)
	api.HandleFunc("DELETE /jobs/{id}", s.handleCancelJob)
	api.HandleFunc("POST /jobs/{id}/retry", s.handleRetryJob)
	api.HandleFunc("GET /jobs/{id}/log", s.handleGetJobLog)

	// Files
	api.HandleFunc("POST /files/reprocess", s.handleReprocessFile)

	// Directories
	api.HandleFunc("GET /directories", s.handleListDirectories)
	api.HandleFunc("POST /directories", s.handleCreateDirectory)
	api.HandleFunc("POST /directories/batch", s.handleBatchCreateDirectories)
	api.HandleFunc("GET /directories/{id}", s.handleGetDirectory)
	api.HandleFunc("PUT /directories/{id}", s.handleUpdateDirectory)
	api.HandleFunc("DELETE /directories/{id}", s.handleDeleteDirectory)

	// Scanner
	api.HandleFunc("POST /scan", s.handleTriggerScan)
	api.HandleFunc("GET /scan/last", s.handleLastScan)

	// Encoders
	api.HandleFunc("GET /encoders", s.handleGetEncoders)

	// Runtime config
	api.HandleFunc("GET /config", s.handleGetConfig)
	api.HandleFunc("PUT /config", s.handleUpdateConfig)
	api.HandleFunc("POST /plex/verify", s.handleVerifyPlex)

	// Worker control
	api.HandleFunc("POST /queue/pause", s.handlePauseQueue)
	api.HandleFunc("POST /queue/resume", s.handleResumeQueue)

	// Stats
	api.HandleFunc("GET /stats", s.handleGetStats)

	// Originals (held files pending review)
	api.HandleFunc("GET /originals", s.handleListOriginals)
	api.HandleFunc("DELETE /originals/{id}", s.handleDeleteOriginal)
	api.HandleFunc("POST /originals/{id}/restore", s.handleRestoreOriginal)

	// Filesystem browser (for directory picker)
	api.HandleFunc("GET /fs", s.handleBrowseFS)

	// Logs (persistent application log, backs the in-app Logs tab)
	api.HandleFunc("GET /logs", s.handleGetLogs)
	api.HandleFunc("POST /logs/clear", s.handleClearLogs)

	// WebSocket
	api.HandleFunc("GET /ws", s.handleWebSocket)

	// Auth
	api.HandleFunc("POST /auth/login", s.handleLogin)
	api.HandleFunc("POST /auth/change-password", s.handleChangePassword)
	api.HandleFunc("DELETE /auth/password", s.handleRemovePassword)

	// Wrap all API routes with auth middleware.
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1",
		s.authMiddleware(s.requestLogger(api))))

	// SPA fallback — serve embedded frontend.
	fsys, err := fs.Sub(frontendFS, "frontend_dist")
	if err != nil {
		panic(fmt.Sprintf("embed frontend: %v", err))
	}
	fileServer := http.FileServer(http.FS(fsys))
	mux.Handle("/", spaHandler(fileServer, fsys))
}

// ServeHTTP implements http.Handler, used in tests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.httpServer.Handler.ServeHTTP(w, r)
}

// Start begins listening. Returns when ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	go s.hub.run(ctx)
	go s.pollSysStats(ctx)

	errCh := make(chan error, 1)
	go func() {
		s.log.Info("HTTP server listening", "addr", s.cfg.Addr())
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(shutCtx)
	case err := <-errCh:
		return err
	}
}

// pollSysStats updates CPU and GPU readings every 2 seconds.
func (s *Server) pollSysStats(ctx context.Context) {
	// Initialise GPU percent as unknown.
	s.sys.mu.Lock()
	s.sys.GPUPercent = -1
	s.sys.mu.Unlock()

	for {
		cpu := readCPUPercent() // blocks ~400ms internally
		gpuMHz := readGPUMHz()
		gpuPct := readGPUPercent() // -1 if unavailable
		s.sys.mu.Lock()
		s.sys.CPUPercent = cpu
		s.sys.GPUMHz = gpuMHz
		s.sys.GPUPercent = gpuPct
		s.sys.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (s *Server) sysStats() (cpuPct float64, gpuMHz int, gpuPct float64) {
	s.sys.mu.RLock()
	defer s.sys.mu.RUnlock()
	return s.sys.CPUPercent, s.sys.GPUMHz, s.sys.GPUPercent
}

// readCPUPercent returns overall CPU utilisation % by reading /proc/stat twice.
func readCPUPercent() float64 {
	idle1, total1 := cpuStat()
	time.Sleep(400 * time.Millisecond)
	idle2, total2 := cpuStat()
	dt := total2 - total1
	if dt == 0 {
		return 0
	}
	return 100.0 * float64(dt-(idle2-idle1)) / float64(dt)
}

func cpuStat() (idle, total uint64) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 8 || fields[0] != "cpu" {
		return
	}
	var vals [7]uint64
	for i := 0; i < 7; i++ {
		vals[i], _ = strconv.ParseUint(fields[i+1], 10, 64)
	}
	// user nice system idle iowait irq softirq
	idle = vals[3] + vals[4] // idle + iowait
	for _, v := range vals {
		total += v
	}
	return
}

// readGPUMHz returns the current Intel GPU clock frequency in MHz.
// Returns 0 if unavailable.
func readGPUMHz() int {
	candidates := []string{
		"/sys/class/drm/card1/gt_cur_freq_mhz",
		"/sys/class/drm/card0/gt_cur_freq_mhz",
		"/sys/class/drm/card1/gt/gt0/rps_cur_freq_mhz",
		"/sys/class/drm/card0/device/gt_cur_freq_mhz",
	}
	for _, p := range candidates {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		v, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil {
			continue
		}
		return v
	}
	return 0
}

// readGPUPercent returns an estimate of GPU utilisation [0-100] using the
// RC6 residency counter: RC6 is the GPU idle/sleep state, so
// utilisation ≈ 1 - (RC6 residency delta / elapsed time).
// Returns -1 if the interface is not available on this system.
func readGPUPercent() float64 {
	candidates := []string{
		"/sys/class/drm/card1/gt/gt0/rc6_residency_ms",
		"/sys/class/drm/card0/gt/gt0/rc6_residency_ms",
	}
	var rc6Path string
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			rc6Path = p
			break
		}
	}
	if rc6Path == "" {
		return -1
	}

	readRC6 := func() (uint64, error) {
		b, err := os.ReadFile(rc6Path)
		if err != nil {
			return 0, err
		}
		return strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	}

	rc6_0, err := readRC6()
	if err != nil {
		return -1
	}
	t0 := time.Now()
	time.Sleep(500 * time.Millisecond)
	rc6_1, err := readRC6()
	if err != nil {
		return -1
	}
	elapsed := float64(time.Since(t0).Milliseconds())
	if elapsed <= 0 {
		return -1
	}
	idleFraction := float64(rc6_1-rc6_0) / elapsed
	pct := (1.0 - idleFraction) * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}

// spaHandler serves the SPA index.html for any path that doesn't match a
// real static file — enabling client-side routing without 404s on refresh.
func spaHandler(fileServer http.Handler, fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		f, err := fsys.Open(p)
		if err == nil {
			fi, _ := f.Stat()
			f.Close()
			if fi != nil && !fi.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// No matching static file — serve SPA entry point for client-side routing.
		http.ServeFileFS(w, r, fsys, "index.html")
	})
}
