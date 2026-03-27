package server

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/tjw/restruct/internal/db"
	"github.com/tjw/restruct/internal/server/sse"
)

// Server is the restruct dashboard HTTP server.
type Server struct {
	db     *db.DB
	hub    *sse.Hub
	router chi.Router
	port   string
	srv    *http.Server
}

// New creates a new server.
// New creates a new server. If webFS is non-nil, it serves the embedded SPA.
func New(database *db.DB, port string, devMode bool, webFS fs.FS) *Server {
	hub := sse.NewHub()
	r := chi.NewRouter()

	s := &Server{
		db:   database,
		hub:  hub,
		router: r,
		port: port,
	}

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	if devMode {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   []string{"http://localhost:5173", "http://localhost:3000"},
			AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Content-Type"},
			AllowCredentials: true,
		}))
	}

	// API routes
	r.Route("/api", func(r chi.Router) {
		r.Get("/events", hub.ServeHTTP)
		r.Get("/health", s.handleHealth)
		r.Get("/metrics", s.handleMetrics)

		r.Get("/sessions", s.handleListSessions)
		r.Get("/sessions/{id}", s.handleGetSession)
		r.Get("/sessions/{id}/refinements", s.handleSessionRefinements)

		r.Get("/refinements", s.handleListRefinements)
		r.Get("/refinements/{id}", s.handleGetRefinement)
		r.Get("/refinements/{id}/events", s.handleRefinementEvents)

		r.Get("/stats", s.handleStats)

		r.Post("/stream/start", s.handleStreamStart)
		r.Post("/stream/token", s.handleStreamToken)
		r.Post("/stream/done", s.handleStreamDone)
		r.Post("/stream/error", s.handleStreamError)
	})

	// Serve embedded SPA in production (non-dev mode)
	if webFS != nil && !devMode {
		r.NotFound(MountSPA(webFS))
	}

	return s
}

// Start starts the HTTP server and the DB poller.
func (s *Server) Start(ctx context.Context) error {
	lc := net.ListenConfig{
		Control: setReuseAddr,
	}
	ln, err := lc.Listen(ctx, "tcp", ":"+s.port)
	if err != nil {
		return err
	}
	s.srv = &http.Server{
		Handler: s.router,
	}

	// Start DB poller for SSE updates
	go s.pollForUpdates(ctx)

	slog.Info("server starting", "port", s.port, "url", fmt.Sprintf("http://localhost:%s", s.port))
	return s.srv.Serve(ln)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// Router returns the chi router (for testing or static file mounting).
func (s *Server) Router() chi.Router {
	return s.router
}

// pollForUpdates checks for new refinements and broadcasts SSE events.
func (s *Server) pollForUpdates(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastRefinementID int64

	// Initialize to current max ID
	row := s.db.Pool().QueryRow("SELECT COALESCE(MAX(id), 0) FROM refinements")
	row.Scan(&lastRefinementID)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refs, err := s.db.GetRefinementsSince(lastRefinementID, 50)
			if err != nil {
				continue
			}
			for _, r := range refs {
				s.hub.Broadcast(sse.Event{Type: "refinement:new", Data: r})
				if r.ID > lastRefinementID {
					lastRefinementID = r.ID
				}
			}
		}
	}
}

// setReuseAddr enables SO_REUSEADDR so the server can restart
// immediately without waiting for TIME_WAIT to expire.
func setReuseAddr(network, address string, conn syscall.RawConn) error {
	return conn.Control(func(fd uintptr) {
		syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	})
}
