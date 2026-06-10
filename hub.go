package sseserver

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

const (
	MaxConnections          = 10000
	defaultBroadcastWorkers = 4
)

type hub struct {
	connections      map[*connection]bool
	broadcast        chan SSEMessage
	broadcastQueue   chan SSEMessage
	register         chan *connection
	unregister       chan *connection
	stopChan         chan struct{}
	debug            bool
	activeCount      int32
	broadcastWorkers int
	droppedMessages  int64
	pool             *sync.Pool
	closeOnce        sync.Once
	connMu           sync.RWMutex
	slicePool        *sync.Pool
}

func newHub() *hub {
	return &hub{
		connections:      make(map[*connection]bool),
		broadcast:        make(chan SSEMessage, 1024),
		broadcastQueue:   make(chan SSEMessage, 2048),
		register:         make(chan *connection, 8192),
		unregister:       make(chan *connection, 8192),
		stopChan:         make(chan struct{}),
		broadcastWorkers: defaultBroadcastWorkers,
		pool: &sync.Pool{
			New: func() any {
				return &connection{}
			},
		},
		slicePool: &sync.Pool{
			New: func() any {
				s := make([]*connection, 0, 128)
				return &s
			},
		},
	}
}

func (h *hub) Start(debug bool) {
	h.debug = debug
	go h.run()
	for i := 0; i < h.broadcastWorkers; i++ {
		go h.broadcastWorker()
	}
	go h.periodicLog()
	h.startCleanupRoutine()
}

func (h *hub) run() {
	defer func() {
		if r := recover(); r != nil {
			log.Println("Recovered in hub.run", r)
		}
	}()

	for {
		select {
		case conn := <-h.register:
			h.registerConnection(conn)
		case conn := <-h.unregister:
			h.unregisterConnection(conn)
		case message := <-h.broadcast:
			select {
			case h.broadcastQueue <- message:
			default:
				atomic.AddInt64(&h.droppedMessages, 1)
				if h.debug {
					log.Println("broadcast queue full, message dropped")
				}
			}
		case <-h.stopChan:
			h.closeAllConnections()
			return
		}
	}
}

func (h *hub) broadcastWorker() {
	for {
		select {
		case msg, ok := <-h.broadcastQueue:
			if !ok {
				return
			}
			h.broadcastMessage(msg)
		case <-h.stopChan:
			return
		}
	}
}

func (h *hub) registerConnection(conn *connection) {
	if atomic.LoadInt32(&h.activeCount) >= MaxConnections {
		if h.debug {
			log.Println("达到最大连接数限制，拒绝新连接")
		}
		conn.safeClose()
		h.putConnection(conn)
		return
	}
	h.connMu.Lock()
	h.connections[conn] = true
	h.connMu.Unlock()
	newCount := atomic.AddInt32(&h.activeCount, 1)
	if h.debug {
		log.Printf("新连接注册，当前活跃连接数: %d\n", newCount)
	}
}

func (h *hub) unregisterConnection(conn *connection) {
	h.connMu.Lock()
	_, ok := h.connections[conn]
	if ok {
		delete(h.connections, conn)
	}
	h.connMu.Unlock()

	if ok {
		conn.safeClose()
		h.putConnection(conn)
		newCount := atomic.AddInt32(&h.activeCount, -1)
		if h.debug {
			log.Printf("Connection unregistered, active connections: %d", newCount)
		}
	}
}

func (h *hub) broadcastMessage(message SSEMessage) {
	data := message.Bytes()

	connsPtr := h.slicePool.Get().(*[]*connection)
	conns := (*connsPtr)[:0]

	h.connMu.RLock()
	for conn := range h.connections {
		conns = append(conns, conn)
	}
	h.connMu.RUnlock()

	var failedConns []*connection
	for _, conn := range conns {
		if conn.isClosed() {
			continue
		}
		if !conn.trySend(data) {
			failedConns = append(failedConns, conn)
		}
	}

	for i := range conns {
		conns[i] = nil
	}
	*connsPtr = conns[:0]
	h.slicePool.Put(connsPtr)

	for _, conn := range failedConns {
		select {
		case h.unregister <- conn:
		default:
			h.unregisterConnection(conn)
		}
	}
}

func (h *hub) closeAllConnections() {
	connsPtr := h.slicePool.Get().(*[]*connection)
	conns := (*connsPtr)[:0]

	h.connMu.Lock()
	for conn := range h.connections {
		conns = append(conns, conn)
	}
	h.connMu.Unlock()

	for _, conn := range conns {
		h.unregisterConnection(conn)
	}

	for i := range conns {
		conns[i] = nil
	}
	*connsPtr = conns[:0]
	h.slicePool.Put(connsPtr)
}

func (h *hub) Stop() {
	h.closeOnce.Do(func() {
		close(h.stopChan)
	})
}

func (h *hub) newConnection() *connection {
	conn := h.pool.Get().(*connection)
	conn.reset()
	conn.hub = h
	conn.send = make(chan []byte, 256)
	now := time.Now()
	conn.createdAt = now
	conn.lastActivity = now
	return conn
}

func (h *hub) putConnection(conn *connection) {
	conn.hub = nil
	h.pool.Put(conn)
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
	connsPtr := h.slicePool.Get().(*[]*connection)
	expiredConns := (*connsPtr)[:0]

	h.connMu.RLock()
	for conn := range h.connections {
		if conn.isExpired() {
			expiredConns = append(expiredConns, conn)
		}
	}
	h.connMu.RUnlock()

	for _, conn := range expiredConns {
		select {
		case h.unregister <- conn:
		case <-h.stopChan:
			for i := range expiredConns {
				expiredConns[i] = nil
			}
			*connsPtr = expiredConns[:0]
			h.slicePool.Put(connsPtr)
			return
		default:
			h.unregisterConnection(conn)
		}
	}

	for i := range expiredConns {
		expiredConns[i] = nil
	}
	*connsPtr = expiredConns[:0]
	h.slicePool.Put(connsPtr)
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

func (h *hub) GetDroppedMessageCount() int64 {
	return atomic.LoadInt64(&h.droppedMessages)
}
