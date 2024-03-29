package sseserver

import (
	"log"
	"net/http"
)

// Server is the primary interface to a SSE server.
type Server struct {
	Broadcast chan<- SSEMessage
	Options   ServerOptions
	hub       *hub
	mux       *http.ServeMux
}

// ServerOptions defines a set of high-level user options that can be customized
// for a Server.
type ServerOptions struct {
	DisableAdminEndpoints bool // disables the "/admin" status endpoints
	// DisallowRootSubscribe bool // TODO: possibly consider this option?
}

// NewServer creates a new Server and returns a reference to it.
func NewServer() *Server {
	s := &Server{
		hub: newHub(),
		mux: http.NewServeMux(),
	}
	// start up our actual internal connection hub
	s.hub.Start()
	// then re-export just the hub's broadcast chan to public
	s.Broadcast = s.hub.broadcast
	// setup routes
	s.setupRoutes()
	return s
}

// setupRoutes configures the HTTP routes for the server.
func (s *Server) setupRoutes() {
	s.mux.Handle(
		"/subscribe/",
		http.StripPrefix("/subscribe", connectionHandler(s.hub)),
	)
}

// ServeHTTP implements the http.Handler interface.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Serve is a convenience method to begin serving connections on specified address.
func (s *Server) Serve(addr string) {
	log.Println("Starting server on addr " + addr)
	handler := s.proxyRemoteAddrHandler(s.requestLogger(http.HandlerFunc(s.ServeHTTP)))
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}

// ProxyRemoteAddrHandler is HTTP middleware to determine the actual RemoteAddr
// of a http.Request when your server sits behind a proxy or load balancer.
func (s *Server) proxyRemoteAddrHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.Header.Get("X-Real-IP")
		if ip == "" {
			ip = r.Header.Get("X-Forwarded-For")
		}
		if ip != "" {
			r.RemoteAddr = ip
		}
		next.ServeHTTP(w, r)
	})
}

// requestLogger is a sample of integrating logging via HTTP middleware.
func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Println("CONNECT\t", r.URL.Path, "\t", r.RemoteAddr)
		next.ServeHTTP(w, r)
		log.Println("DISCONNECT\t", r.URL.Path, "\t", r.RemoteAddr)
	})
}
