package sseserver

import (
	"log"
	"sync"
	"sync/atomic"
)

type hub struct {
	connections map[*connection]bool
	broadcast   chan SSEMessage
	register    chan *connection
	unregister  chan *connection
	activeCount int32 // 使用原子操作
	pool        *sync.Pool
	stopChan    chan struct{}
	debug       bool
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

func (h *hub) Start(startBroadcast func(), stopBroadcast func(), debug bool) {
	h.debug = debug
	go h.run(startBroadcast, stopBroadcast)
}

func (h *hub) run(startBroadcast func(), stopBroadcast func()) {
	defer func() {
		if r := recover(); r != nil && h.debug {
			log.Println("Recovered in hub.run", r)
		}
	}()

	for {
		select {
		case conn := <-h.register:
			h.connections[conn] = true
			newCount := atomic.AddInt32(&h.activeCount, 1)
			if h.debug {
				log.Printf("Connection registered, active connections: %d\n", newCount)
			}
			if newCount == 1 {
				startBroadcast()
			}
		case conn := <-h.unregister:
			if _, ok := h.connections[conn]; ok {
				delete(h.connections, conn)
				newCount := atomic.AddInt32(&h.activeCount, -1)
				if h.debug {
					log.Printf("Connection unregistered, active connections: %d\n", newCount)
				}
				if newCount == 0 {
					stopBroadcast()
				}
			}
		case message := <-h.broadcast:
			h.broadcastMessage(message)
		case <-h.stopChan:
			h.closeConnections()
			return
		}
	}
}

func (h *hub) broadcastMessage(message SSEMessage) {
	for conn := range h.connections {
		go func(c *connection) {
			select {
			case <-h.stopChan:
				return
			default:
				c.write(message.Bytes())
			}
		}(conn)
	}
}

func (h *hub) closeConnections() {
	for conn := range h.connections {
		conn.close()
	}
	h.connections = make(map[*connection]bool)
	atomic.StoreInt32(&h.activeCount, 0)
}

func (h *hub) Stop() {
	h.closeOnce.Do(func() {
		close(h.stopChan)
	})
}
