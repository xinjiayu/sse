package sseserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	Broadcast chan<- SSEMessage
	Options   ServerOptions
	hub       *hub
	mux       *http.ServeMux
	stopChan  chan struct{}
	Debug     bool
	closeOnce sync.Once
	server    *http.Server

	heartbeatData []byte

	ipConns   map[string]int32
	ipConnsMu sync.Mutex
}

type ServerOptions struct {
	DisableAdminEndpoints bool
	CorsOptions           *CorsOptions
	HeartbeatInterval     time.Duration // 心跳间隔，0 = 禁用（默认）
	MaxConnectionsPerIP   int           // 单 IP 最大连接数，0 = 不限制（默认）
	BroadcastWorkers      int           // 广播 worker 数量，0 = 默认 4
	ShutdownTimeout       time.Duration // 优雅关闭超时，0 = 默认 5s
	IdleTimeout           time.Duration // 空闲连接超时，0 = 默认 30s
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
		Debug:    false,
		Options:  opts,
	}

	if opts.BroadcastWorkers > 0 {
		s.hub.broadcastWorkers = opts.BroadcastWorkers
	}
	if opts.HeartbeatInterval > 0 {
		s.heartbeatData = []byte(":keepalive\n\n")
	}
	if opts.MaxConnectionsPerIP > 0 {
		s.ipConns = make(map[string]int32)
	}

	s.hub.Start(s.Debug)
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
		s.logDebug("New SSE connection established from %s", r.RemoteAddr)
		defer s.logDebug("SSE connection closed for %s", r.RemoteAddr)

		// Per-IP 连接限制
		if s.ipConns != nil {
			ip := extractIP(r.RemoteAddr)
			s.ipConnsMu.Lock()
			if s.ipConns[ip] >= int32(s.Options.MaxConnectionsPerIP) {
				s.ipConnsMu.Unlock()
				http.Error(w, "Too many connections", http.StatusTooManyRequests)
				return
			}
			s.ipConns[ip]++
			s.ipConnsMu.Unlock()
			defer func() {
				s.ipConnsMu.Lock()
				s.ipConns[ip]--
				if s.ipConns[ip] <= 0 {
					delete(s.ipConns, ip)
				}
				s.ipConnsMu.Unlock()
			}()
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
		flusher.Flush() // 立即发送 headers，避免客户端等待首条消息才收到响应头

		// 创建并注册新连接
		conn := s.hub.newConnection()
		sendCh := conn.send

		select {
		case s.hub.register <- conn:
		default:
			go func() {
				select {
				case s.hub.register <- conn:
				case <-s.stopChan:
					conn.safeClose()
					s.hub.putConnection(conn)
				}
			}()
		}
		defer func() {
			select {
			case s.hub.unregister <- conn:
			default:
				s.hub.unregisterConnection(conn)
			}
			s.logDebug("Connection closed for %s", r.RemoteAddr)
		}()

		ctx := r.Context()

		var heartbeatC <-chan time.Time
		if s.Options.HeartbeatInterval > 0 {
			hbTicker := time.NewTicker(s.Options.HeartbeatInterval)
			defer hbTicker.Stop()
			heartbeatC = hbTicker.C
		}

		idleTimeout := 30 * time.Second
		if s.Options.IdleTimeout > 0 {
			idleTimeout = s.Options.IdleTimeout
		}
		idleTicker := time.NewTicker(idleTimeout)
		defer idleTicker.Stop()

		for {
			select {
			case msg, ok := <-sendCh:
				if !ok {
					return
				}
				select {
				case <-ctx.Done():
					return
				default:
				}

				conn.updateActivity()
				if _, err := w.Write(msg); err != nil {
					s.logError("Error writing to client: %v", err)
					return
				}

				select {
				case <-ctx.Done():
					return
				default:
				}

				s.safeFlush(flusher)
			case <-heartbeatC:
				select {
				case <-ctx.Done():
					return
				default:
				}
				if _, err := w.Write(s.heartbeatData); err != nil {
					return
				}
				s.safeFlush(flusher)
				conn.updateActivity()
			case <-idleTicker.C:
				if _, err := w.Write([]byte(":\n\n")); err != nil {
					return
				}
				s.safeFlush(flusher)
			case <-ctx.Done():
				return
			}
		}
	})
}

// safeFlush 安全地调用 Flush，捕获可能的 panic 防止段错误导致程序崩溃
func (s *Server) safeFlush(flusher http.Flusher) {
	defer func() {
		if r := recover(); r != nil {
			s.logError("Recovered from panic in Flush: %v", r)
		}
	}()
	flusher.Flush()
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

func (s *Server) ServeListener(listener net.Listener) error {
	log.Println("Starting server on " + listener.Addr().String())
	handler := s.proxyRemoteAddrHandler(s.requestLogger(http.HandlerFunc(s.ServeHTTP)))
	s.server = &http.Server{
		Handler: handler,
	}
	return s.server.Serve(listener)
}

func (s *Server) Stop() error {
	s.closeOnce.Do(func() {
		close(s.stopChan)
	})
	s.hub.Stop()
	timeout := 5 * time.Second
	if s.Options.ShutdownTimeout > 0 {
		timeout = s.Options.ShutdownTimeout
	}
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
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

func (s *Server) GetActiveConnectionCount() int32 {
	return s.hub.GetActiveConnectionCount()
}

func (s *Server) GetDroppedMessageCount() int64 {
	return s.hub.GetDroppedMessageCount()
}

func (s *Server) addHealthCheckEndpoint() {
	s.mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		count := s.GetActiveConnectionCount()
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Active connections: %d", count)
	})
}

func (s *Server) logError(format string, v ...any) {
	log.Printf(format, v...)
}

func (s *Server) logDebug(format string, v ...any) {
	if s.Debug {
		log.Printf(format, v...)
	}
}

func extractIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
