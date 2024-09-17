package sseserver

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

type Server struct {
	Broadcast chan<- SSEMessage
	Receive   chan SSEMessage
	Options   ServerOptions
	hub       *hub
	mux       *http.ServeMux
	stopChan  chan struct{}
	Debug     bool
	closeOnce sync.Once
}

type ServerOptions struct {
	DisableAdminEndpoints bool
	CorsOptions           *CorsOptions
}

type CorsOptions struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
	MaxAge         int
}

func NewServer(options ...ServerOptions) *Server {
	var opts ServerOptions
	if len(options) > 0 {
		opts = options[0]
	}

	s := &Server{
		hub:      newHub(),
		mux:      http.NewServeMux(),
		stopChan: make(chan struct{}),
		Receive:  make(chan SSEMessage),
		Debug:    false,
		Options:  opts,
	}

	s.hub.Start(s.startBroadcast, s.stopBroadcast, s.Debug)
	s.Broadcast = s.hub.broadcast
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.mux.Handle(
		"/subscribe/",
		http.StripPrefix("/subscribe", s.corsMiddleware(connectionHandler(s.hub))),
	)
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Options.CorsOptions == nil {
			// Default CORS settings (allow all origins)
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		} else {
			origin := r.Header.Get("Origin")
			if origin != "" {
				for _, allowedOrigin := range s.Options.CorsOptions.AllowedOrigins {
					if allowedOrigin == "*" || allowedOrigin == origin {
						w.Header().Set("Access-Control-Allow-Origin", origin)
						break
					}
				}
			}

			if len(s.Options.CorsOptions.AllowedMethods) > 0 {
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(s.Options.CorsOptions.AllowedMethods, ", "))
			}

			if len(s.Options.CorsOptions.AllowedHeaders) > 0 {
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(s.Options.CorsOptions.AllowedHeaders, ", "))
			}

			if s.Options.CorsOptions.MaxAge > 0 {
				w.Header().Set("Access-Control-Max-Age", strconv.Itoa(s.Options.CorsOptions.MaxAge))
			}
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) Serve(addr string) {
	log.Println("Starting server on addr " + addr)
	handler := s.proxyRemoteAddrHandler(s.requestLogger(http.HandlerFunc(s.ServeHTTP)))
	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("ListenAndServe:", err)
	}
}

func (s *Server) proxyRemoteAddrHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.Header.Get("X-Real-IP")
		if ip == "" {
			ip = r.Header.Get("X-Forwarded-For")
			if ip == "" {
				ip = r.RemoteAddr
			}
		}
		r.RemoteAddr = ip
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s %s\n", r.RemoteAddr, r.Method, r.URL)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) startBroadcast() {
	go func() {
		for {
			select {
			case <-s.stopChan:
				return
			case message := <-s.Receive:
				s.Broadcast <- message
			}
		}
	}()
}

func (s *Server) stopBroadcast() {
	s.closeOnce.Do(func() {
		close(s.stopChan)
	})
}
