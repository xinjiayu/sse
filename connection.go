package sseserver

import (
	"context"
	"net/http"
	"time"
)

const connBufSize = 256

type connection struct {
	r         *http.Request
	w         http.ResponseWriter
	created   time.Time
	send      chan []byte
	namespace string
	msgsSent  uint64
}

func newConnection(w http.ResponseWriter, r *http.Request, namespace string) *connection {
	return &connection{
		send:      make(chan []byte, connBufSize),
		w:         w,
		r:         r,
		created:   time.Now(),
		namespace: namespace,
	}
}

type connectionStatus struct {
	Path      string `json:"request_path"`
	Namespace string `json:"namespace"`
	Created   int64  `json:"created_at"`
	ClientIP  string `json:"client_ip"`
	UserAgent string `json:"user_agent"`
	MsgsSent  uint64 `json:"msgs_sent"`
}

func (c *connection) Status() connectionStatus {
	return connectionStatus{
		Path:      c.r.URL.Path,
		Namespace: c.namespace,
		Created:   c.created.Unix(),
		ClientIP:  c.r.RemoteAddr,
		UserAgent: c.r.UserAgent(),
		MsgsSent:  c.msgsSent,
	}
}

func (c *connection) writer(ctx context.Context) {
	keepaliveTickler := time.NewTicker(15 * time.Second)
	keepaliveMsg := []byte(":keepalive\n")
	defer keepaliveTickler.Stop()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			if _, err := c.w.Write(msg); err != nil {
				return
			}
			if f, ok := c.w.(http.Flusher); ok {
				f.Flush()
				c.msgsSent++
			}

		case <-keepaliveTickler.C:
			if _, err := c.w.Write(keepaliveMsg); err != nil {
				return
			}
			if f, ok := c.w.(http.Flusher); ok {
				f.Flush()
			}

		case <-ctx.Done():
			return
		}
	}
}

func connectionHandler(h *hub) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		headers.Set("Access-Control-Allow-Origin", "*") // Configurable in production
		headers.Set("Content-Type", "text/event-stream; charset=utf-8")
		headers.Set("Cache-Control", "no-cache")
		headers.Set("Connection", "keep-alive")
		headers.Set("Server", "mroth/sseserver")

		namespace := r.URL.Path
		c := newConnection(w, r, namespace)
		h.register <- c
		defer func() {
			h.unregister <- c
		}()

		c.writer(r.Context())
	})
}
