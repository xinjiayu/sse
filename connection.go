package sseserver

import (
	"context"
	"log"
	"net/http"
	"time"
)

type connection struct {
	send   chan []byte
	hub    *hub
	closed bool
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

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel() // 确保协程被正确取消

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
				if *hub.debug {
					log.Printf("Sending message to connection：\\n%+v", string(message))
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
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel() // 使用context来管理通道操作的超时

	select {
	case <-c.hub.stopChan:
		return
	case c.send <- msg:
	case <-ctx.Done(): // 处理超时
		log.Println("Failed to send message: Write timeout")
		c.close()
	}
}

func (c *connection) close() {
	if !c.closed {
		close(c.send)
		c.closed = true
		c.hub.unregister <- c
		if *c.hub.debug {
			log.Println("Connection closed")
		}
	}
}
