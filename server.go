package sseserver

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const inactivityTimeout = 10 * time.Minute

type Server struct {
	Broadcast chan<- SSEMessage
	Receive   chan SSEMessage
	Options   ServerOptions
	hub       *hub
	mux       *http.ServeMux
	stopChan  chan struct{}
	Debug     bool
	closeOnce sync.Once
	server    *http.Server
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
		http.StripPrefix("/subscribe", s.corsMiddleware(s.connectionHandler())),
	)
	s.addHealthCheckEndpoint()
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Options.CorsOptions == nil {
			s.setDefaultCORSHeaders(w)
		} else {
			s.setCustomCORSHeaders(w, r)
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) setDefaultCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func (s *Server) setCustomCORSHeaders(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) connectionHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Debug {
			log.Printf("New SSE connection established from %s", r.RemoteAddr)
			defer log.Printf("SSE connection closed for %s", r.RemoteAddr)
		}

		// 设置 headers
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// 创建并注册新连接
		conn := s.hub.newConnection()
		s.hub.register <- conn
		defer func() {
			s.hub.unregister <- conn
			if s.Debug {
				log.Printf("Connection closed for %s", r.RemoteAddr)
			}
		}()

		// 主循环
		for {
			select {
			case msg, ok := <-conn.send:
				if !ok {
					return
				}
				conn.updateActivity() // 更新最后活动时间
				if _, err := w.Write(msg); err != nil {
					s.logError("Error writing to client: %v", err)
					return
				}
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) Serve(addr string) error {
	log.Println("Starting server on addr " + addr)
	handler := s.proxyRemoteAddrHandler(s.requestLogger(http.HandlerFunc(s.ServeHTTP)))
	s.server = &http.Server{
		Addr:    addr,
		Handler: handler,
	}
	return s.server.ListenAndServe()
}

func (s *Server) Stop() error {
	s.stopBroadcast()
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.server.Shutdown(ctx)
	}
	return nil
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

func (s *Server) GetActiveConnectionCount() int32 {
	return s.hub.GetActiveConnectionCount()
}

func (s *Server) addHealthCheckEndpoint() {
	s.mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		count := s.GetActiveConnectionCount()
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Active connections: %d", count)
	})
}

func (s *Server) startPeriodicCleanup() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.cleanupInactiveConnections()
			case <-s.stopChan:
				return
			}
		}
	}()
}

func (s *Server) cleanupInactiveConnections() {
	for conn := range s.hub.connections {
		if conn.isInactive(inactivityTimeout) {
			s.hub.unregister <- conn
			if s.Debug {
				log.Printf("Inactive connection closed: %v", conn)
			}
		}
	}
}

func (s *Server) logError(format string, v ...interface{}) {
	if s.Debug {
		log.Printf(format, v...)
	}
}
