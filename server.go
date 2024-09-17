package sseserver

import (
	"log"
	"net/http"
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
}

func NewServer() *Server {
	s := &Server{
		hub:      newHub(),
		mux:      http.NewServeMux(),
		stopChan: make(chan struct{}),
		Receive:  make(chan SSEMessage),
		Debug:    false,
	}
	s.hub.Start(s.startBroadcast, s.stopBroadcast, s.Debug)
	s.Broadcast = s.hub.broadcast
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.mux.Handle(
		"/subscribe/",
		http.StripPrefix("/subscribe", connectionHandler(s.hub)),
	)
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
