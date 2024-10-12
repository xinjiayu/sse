package sseserver

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

const (
	MaxConnections = 10000
)

type hub struct {
	connections map[*connection]bool
	broadcast   chan SSEMessage
	register    chan *connection
	unregister  chan *connection
	stopChan    chan struct{}
	debug       bool
	activeCount int32
	pool        *sync.Pool
	closeOnce   sync.Once
}

func newHub() *hub {
	return &hub{
		connections: make(map[*connection]bool),
		broadcast:   make(chan SSEMessage, 1024),
		register:    make(chan *connection, 1024),
		unregister:  make(chan *connection, 1024),
		stopChan:    make(chan struct{}),
		pool: &sync.Pool{
			New: func() interface{} {
				return &connection{}
			},
		},
	}
}

func (h *hub) Start(startBroadcast func(), stopBroadcast func(), debug bool) {
	h.debug = debug
	go h.run(startBroadcast, stopBroadcast)
	go h.periodicLog()
	h.startCleanupRoutine()
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
			h.registerConnection(conn, startBroadcast)
		case conn := <-h.unregister:
			h.unregisterConnection(conn, stopBroadcast)
		case message := <-h.broadcast:
			h.broadcastMessage(message)
		case <-h.stopChan:
			h.closeAllConnections()
			return
		}
	}
}

func (h *hub) registerConnection(conn *connection, startBroadcast func()) {
	if atomic.LoadInt32(&h.activeCount) >= MaxConnections {
		if h.debug {
			log.Println("达到最大连接数限制，拒绝新连接")
		}
		conn.close()
		return
	}
	h.connections[conn] = true
	newCount := atomic.AddInt32(&h.activeCount, 1)
	if h.debug {
		log.Printf("新连接注册，当前活跃连接数: %d\n", newCount)
	}
	if newCount == 1 {
		startBroadcast()
	}
}

func (h *hub) unregisterConnection(conn *connection, stopBroadcast func()) {
	if _, ok := h.connections[conn]; ok {
		delete(h.connections, conn)
		close(conn.send)
		newCount := atomic.AddInt32(&h.activeCount, -1)
		if h.debug {
			log.Printf("Connection unregistered, active connections: %d", newCount)
		}
		if newCount == 0 {
			stopBroadcast()
		}
	}
}

func (h *hub) broadcastMessage(message SSEMessage) {
	data := message.Bytes()
	for conn := range h.connections {
		select {
		case conn.send <- data:
		default:
			h.unregisterConnection(conn, func() {})
		}
	}
}

func (h *hub) closeAllConnections() {
	for conn := range h.connections {
		h.unregisterConnection(conn, func() {})
	}
}

func (h *hub) Stop() {
	h.closeOnce.Do(func() {
		close(h.stopChan)
	})
}

func (h *hub) newConnection() *connection {
	conn := h.pool.Get().(*connection)
	conn.hub = h
	conn.send = make(chan []byte, 256)
	now := time.Now()
	conn.createdAt = now
	conn.lastActivity = now
	return conn
}

func (h *hub) periodicLog() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if h.debug {
				log.Printf("当前活跃连接数: %d", atomic.LoadInt32(&h.activeCount))
			}
		case <-h.stopChan:
			return
		}
	}
}

func (h *hub) cleanupExpiredConnections() {
	for conn := range h.connections {
		if conn.isExpired() {
			h.unregisterConnection(conn, func() {})
		}
	}
}

func (h *hub) startCleanupRoutine() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				h.cleanupExpiredConnections()
			case <-h.stopChan:
				return
			}
		}
	}()
}

func (h *hub) GetActiveConnectionCount() int32 {
	return atomic.LoadInt32(&h.activeCount)
}
