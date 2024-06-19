package sseserver

import (
	"log"
	"sync"
)

type hub struct {
	connections map[*connection]bool
	broadcast   chan SSEMessage
	register    chan *connection
	unregister  chan *connection
	activeCount int
	pool        *sync.Pool
	stopChan    chan struct{}
	debug       *bool // 添加 debug 字段
	closeOnce   sync.Once
}

func newHub() *hub {
	return &hub{
		connections: make(map[*connection]bool),
		broadcast:   make(chan SSEMessage, 1024),
		register:    make(chan *connection, 1024),
		unregister:  make(chan *connection, 1024),
		pool: &sync.Pool{
			New: func() interface{} {
				return &connection{}
			},
		},
		stopChan: make(chan struct{}),
	}
}

func (h *hub) Start(startBroadcast func(), stopBroadcast func(), debug *bool) {
	h.debug = debug
	go func() {
		defer func() {
			if r := recover(); r != nil && *debug {
				log.Println("Recovered in Start", r)
			}
		}()

		for {
			select {
			case conn := <-h.register:
				h.connections[conn] = true
				h.activeCount++
				if *h.debug {
					log.Printf("Connection registered, active connections: %d\n", h.activeCount)
				}
				if h.activeCount == 1 {
					startBroadcast() // 开始推流
				}
			case conn := <-h.unregister:
				if _, ok := h.connections[conn]; ok {
					delete(h.connections, conn)
					conn.close()
					h.activeCount--
					h.pool.Put(conn) // 将连接放回池中
					if *h.debug {
						log.Printf("Connection unregistered, active connections: %d\n", h.activeCount)
					}
					if h.activeCount == 0 {
						stopBroadcast() // 停止推流
					}
				}
			case message := <-h.broadcast:
				for conn := range h.connections {
					conn := conn
					go func(c *connection) {
						select {
						case <-h.stopChan:
							return
						default:
							if !c.closed {
								c.send <- message.Bytes()
							}
						}
					}(conn)
				}
			case <-h.stopChan:
				h.closeConnections()
				return
			}
		}
	}()
}

func (h *hub) closeConnections() {
	for conn := range h.connections {
		conn.close()
		h.pool.Put(conn)
	}
	h.connections = make(map[*connection]bool)
	h.activeCount = 0
}

func (h *hub) Stop() {
	h.closeOnce.Do(func() {
		close(h.stopChan)
	})
}
