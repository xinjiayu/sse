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
	connMu      sync.RWMutex // 保护 connections map 的读写操作
	slicePool   *sync.Pool   // 复用连接切片，减少 GC 压力
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
		slicePool: &sync.Pool{
			New: func() interface{} {
				// 预分配一个合理大小的切片
				s := make([]*connection, 0, 128)
				return &s
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
		conn.safeClose()
		// 归还连接到 pool
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
	if newCount == 1 {
		startBroadcast()
	}
}

func (h *hub) unregisterConnection(conn *connection, stopBroadcast func()) {
	h.connMu.Lock()
	_, ok := h.connections[conn]
	if ok {
		delete(h.connections, conn)
	}
	h.connMu.Unlock()

	if ok {
		// 使用安全的方式关闭 channel，防止重复关闭导致 panic
		conn.safeClose()
		// 归还连接到 pool 以便复用，减少 GC 压力
		h.putConnection(conn)
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

	// 从 pool 获取切片，减少 GC 压力
	connsPtr := h.slicePool.Get().(*[]*connection)
	conns := (*connsPtr)[:0]

	// 先收集所有连接，避免在遍历时发生并发问题
	h.connMu.RLock()
	for conn := range h.connections {
		conns = append(conns, conn)
	}
	h.connMu.RUnlock()

	// 发送消息给所有连接
	var failedConns []*connection
	for _, conn := range conns {
		select {
		case conn.send <- data:
		default:
			failedConns = append(failedConns, conn)
		}
	}

	// 清理并归还切片到 pool
	for i := range conns {
		conns[i] = nil // 帮助 GC
	}
	*connsPtr = conns[:0]
	h.slicePool.Put(connsPtr)

	// 通过 unregister channel 处理发送失败的连接
	for _, conn := range failedConns {
		select {
		case h.unregister <- conn:
		default:
			// 退化路径也要走统一注销逻辑，保持 map/计数/pool 状态一致
			h.unregisterConnection(conn, func() {})
		}
	}
}

func (h *hub) closeAllConnections() {
	// 从 pool 获取切片
	connsPtr := h.slicePool.Get().(*[]*connection)
	conns := (*connsPtr)[:0]

	h.connMu.Lock()
	for conn := range h.connections {
		conns = append(conns, conn)
	}
	h.connMu.Unlock()

	for _, conn := range conns {
		h.unregisterConnection(conn, func() {})
	}

	// 清理并归还切片到 pool
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
	// 重置连接状态，确保从 pool 复用的连接可以正常使用
	conn.reset()
	conn.hub = h
	conn.send = make(chan []byte, 256)
	now := time.Now()
	conn.createdAt = now
	conn.lastActivity = now
	return conn
}

// putConnection 将连接归还到 pool 以便复用，减少 GC 压力
func (h *hub) putConnection(conn *connection) {
	// 清理连接引用，帮助 GC
	conn.hub = nil
	conn.send = nil
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
	// 从 pool 获取切片
	connsPtr := h.slicePool.Get().(*[]*connection)
	expiredConns := (*connsPtr)[:0]

	// 收集需要清理的连接，避免在遍历时直接修改 map
	h.connMu.RLock()
	for conn := range h.connections {
		if conn.isExpired() {
			expiredConns = append(expiredConns, conn)
		}
	}
	h.connMu.RUnlock()

	// 通过 unregister channel 安全地注销过期连接
	for _, conn := range expiredConns {
		select {
		case h.unregister <- conn:
		case <-h.stopChan:
			// 清理并归还切片
			for i := range expiredConns {
				expiredConns[i] = nil
			}
			*connsPtr = expiredConns[:0]
			h.slicePool.Put(connsPtr)
			return
		default:
			// 退化路径走统一注销，避免多路径状态不一致
			h.unregisterConnection(conn, func() {})
		}
	}

	// 清理并归还切片到 pool
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
