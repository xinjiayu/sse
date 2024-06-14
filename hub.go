package sseserver

import (
	"sync"
)

type hub struct {
	connections map[*connection]bool
	broadcast   chan SSEMessage
	register    chan *connection
	unregister  chan *connection
	mu          sync.Mutex
	activeCount int
	pool        *sync.Pool
}

func newHub() *hub {
	return &hub{
		connections: make(map[*connection]bool),
		broadcast:   make(chan SSEMessage, 512),  // 增加缓冲区大小
		register:    make(chan *connection, 512), // 增加缓冲区大小
		unregister:  make(chan *connection, 512), // 增加缓冲区大小
		pool: &sync.Pool{
			New: func() interface{} {
				return &connection{}
			},
		},
	}
}

func (h *hub) Start(startBroadcast func(), stopBroadcast func()) {
	go func() {
		for {
			select {
			case conn := <-h.register:
				h.mu.Lock()
				h.connections[conn] = true
				h.activeCount++
				//log.Printf("Connection registered, active connections: %d\n", h.activeCount)
				if h.activeCount == 1 {
					startBroadcast() // 开始推流
				}
				h.mu.Unlock()
			case conn := <-h.unregister:
				h.mu.Lock()
				if _, ok := h.connections[conn]; ok {
					delete(h.connections, conn)
					conn.close()
					h.activeCount--
					h.pool.Put(conn) // 将连接放回池中
					//log.Printf("Connection unregistered, active connections: %d\n", h.activeCount)
					if h.activeCount == 0 {
						stopBroadcast() // 停止推流
					}
				}
				h.mu.Unlock()
			case message := <-h.broadcast:
				h.mu.Lock()
				for conn := range h.connections {
					conn := conn
					go func() {
						select {
						case conn.send <- message.Bytes():
						default:
							conn.close()
							h.mu.Lock()
							delete(h.connections, conn)
							h.activeCount--
							h.pool.Put(conn) // 将连接放回池中
							//log.Printf("Connection closed due to send buffer full, active connections: %d\n", h.activeCount)
							if h.activeCount == 0 {
								stopBroadcast() // 停止推流
							}
							h.mu.Unlock()
						}
					}()
				}
				h.mu.Unlock()
			}
		}
	}()
}
