package sseserver

import (
	"log"
	"net/http"
	"sync"
)

type connection struct {
	send   chan []byte
	hub    *hub
	closed bool
	mu     sync.Mutex
}

func connectionHandler(hub *hub) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn := hub.pool.Get().(*connection) // 从池中获取连接
		conn.send = make(chan []byte, 512)   // 增加缓冲区大小
		conn.hub = hub
		conn.closed = false
		hub.register <- conn
		defer func() {
			conn.close()
		}()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}

		// 使用请求的 Context 来处理连接关闭通知
		notify := r.Context().Done()
		go func() {
			<-notify
			conn.close()
		}()

		for {
			select {
			case message, ok := <-conn.send:
				if !ok {
					return
				}
				if *hub.debug {
					log.Printf("Sending message to connection：\n%+v", string(message))
				}
				_, err := w.Write(message)
				if err != nil {
					if *hub.debug {
						log.Printf("Error writing message: %v", err)
					}
					return
				}
				flusher.Flush()
			case <-hub.stopChan:
				return
			}
		}
	})
}

func (c *connection) write(msg []byte) {
	select {
	case c.send <- msg:
	case <-c.hub.stopChan:
		return
	default:
		c.close()
	}
}

func (c *connection) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		close(c.send)
		c.closed = true
		c.hub.unregister <- c
		if *c.hub.debug {
			log.Println("Connection closed")
		}
	}
}
