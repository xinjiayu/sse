package sseserver

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"
)

const (
	// 增加缓冲区大小，减少阻塞风险
	sendBufferSize = 1024
	// 写入超时时间
	writeTimeout = 5 * time.Second
)

type connection struct {
	send   chan []byte
	hub    *hub
	closed bool
	mu     sync.Mutex // 保护 closed 字段
}

func connectionHandler(hub *hub) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn := hub.pool.Get().(*connection)
		conn.send = make(chan []byte, sendBufferSize)
		conn.hub = hub
		conn.closed = false
		hub.register <- conn
		defer func() {
			conn.close()
			hub.pool.Put(conn) // 将连接放回池中
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

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		go func() {
			<-ctx.Done()
			conn.close()
		}()

		for {
			select {
			case message, ok := <-conn.send:
				if !ok {
					return
				}
				if hub.debug {
					log.Printf("Sending message to connection：\n%s", string(message))
				}
				_, err := w.Write(message)
				if err != nil {
					if hub.debug {
						log.Printf("Error writing message: %v", err)
					}
					return
				}
				flusher.Flush()
			case <-hub.stopChan:
				return
			case <-ctx.Done():
				return
			}
		}
	})
}

func (c *connection) write(msg []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()

	if closed {
		return
	}

	select {
	case <-c.hub.stopChan:
		return
	case c.send <- msg:
	case <-ctx.Done():
		log.Println("Failed to send message: Write timeout")
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
		if c.hub.debug {
			log.Println("Connection closed")
		}
	}
}
