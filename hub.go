package sseserver

import (
	"strings"
	"sync"
	"time"
)

type hub struct {
	broadcast   chan SSEMessage
	connections map[*connection]bool
	register    chan *connection
	unregister  chan *connection
	shutdown    chan struct{}
	sentMsgs    uint64
	startupTime time.Time
	connMutex   sync.RWMutex // 新增读写锁
}

func newHub() *hub {
	return &hub{
		broadcast:   make(chan SSEMessage),
		connections: make(map[*connection]bool),
		register:    make(chan *connection),
		unregister:  make(chan *connection),
		shutdown:    make(chan struct{}),
		startupTime: time.Now(),
		connMutex:   sync.RWMutex{},
	}
}

func (h *hub) Shutdown() {
	close(h.shutdown)
}

func (h *hub) Start() {
	go h.run()
}

func (h *hub) run() {
	for {
		select {
		case <-h.shutdown:
			h.connMutex.Lock()
			for c := range h.connections {
				h._shutdownConn(c)
			}
			h.connections = make(map[*connection]bool) // 重置连接映射
			h.connMutex.Unlock()
			return
		case c := <-h.register:
			h.connMutex.Lock()
			h.connections[c] = true
			h.connMutex.Unlock()
		case c := <-h.unregister:
			h._unregisterConn(c)
		case msg := <-h.broadcast:
			h.sentMsgs++
			h._broadcastMessage(msg)
		}
	}
}

func (h *hub) _unregisterConn(c *connection) {
	h.connMutex.Lock()
	delete(h.connections, c)
	h.connMutex.Unlock()
}

func (h *hub) _shutdownConn(c *connection) {
	h._unregisterConn(c)
	close(c.send)
}

func (h *hub) _broadcastMessage(msg SSEMessage) {
	formattedMsg := msg.sseFormat()
	h.connMutex.RLock()
	defer h.connMutex.RUnlock()

	for c := range h.connections {
		if strings.HasPrefix(msg.Namespace, c.namespace) {
			select {
			case c.send <- formattedMsg:
			default:
				h._shutdownConn(c)
			}
		}
	}
}
