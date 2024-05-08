package sseserver

import (
	"strings"
	"sync"
	"time"
)

// hub 负责管理客户端连接、广播消息等功能
type hub struct {
	broadcast   chan SSEMessage      // 用于广播的通道
	connections map[*connection]bool // 维护活动连接的集合
	register    chan *connection     // 注册新的连接
	unregister  chan *connection     // 取消连接
	shutdown    chan struct{}        // 关闭信号
	sentMsgs    uint64               // 统计已发送的消息数量
	startupTime time.Time            // 服务启动时间
	connMutex   sync.RWMutex         // 读写锁保护 connections
}

// newHub 创建并初始化 hub 实例
func newHub() *hub {
	return &hub{
		broadcast:   make(chan SSEMessage, 100), // 增加缓冲，防止广播阻塞
		connections: make(map[*connection]bool),
		register:    make(chan *connection, 100), // 增加缓冲，防止注册阻塞
		unregister:  make(chan *connection, 100), // 增加缓冲，防止注销阻塞
		shutdown:    make(chan struct{}),
		startupTime: time.Now(),
		connMutex:   sync.RWMutex{},
	}
}

// Shutdown 触发 hub 停止
func (h *hub) Shutdown() {
	close(h.shutdown) // 发送关闭信号
}

// Start 启动 hub 的运行循环
func (h *hub) Start() {
	go h.run()
}

// run 是 hub 的主事件循环，处理注册、注销、消息广播和关闭
func (h *hub) run() {
	for {
		select {
		case <-h.shutdown:
			h.shutdownConnections()
			return
		case c := <-h.register:
			h.addConnection(c)
		case c := <-h.unregister:
			h.removeConnection(c)
		case msg := <-h.broadcast:
			h.sentMsgs++
			h.broadcastMessage(msg)
		}
	}
}

// shutdownConnections 安全关闭所有连接并清空连接集合
func (h *hub) shutdownConnections() {
	h.connMutex.Lock()
	defer h.connMutex.Unlock()
	for c := range h.connections {
		h.shutdownConnection(c)
	}
	h.connections = make(map[*connection]bool) // 重置连接映射
}

// addConnection 安全注册新的连接
func (h *hub) addConnection(c *connection) {
	h.connMutex.Lock()
	defer h.connMutex.Unlock()
	c.active = true // 设置连接为活动状态
	h.connections[c] = true
}

// removeConnection 安全移除连接
func (h *hub) removeConnection(c *connection) {
	h.connMutex.Lock()
	defer h.connMutex.Unlock()
	c.active = false // 设置连接为非活动状态
	delete(h.connections, c)
	close(c.send) // 确保安全关闭发送通道
}

// shutdownConnection 安全地注销并关闭连接
func (h *hub) shutdownConnection(c *connection) {
	h.removeConnection(c)
}

// broadcastMessage 将消息广播到匹配的连接
func (h *hub) broadcastMessage(msg SSEMessage) {
	formattedMsg := msg.sseFormat()
	h.connMutex.RLock()
	defer h.connMutex.RUnlock()

	for c := range h.connections {
		if c.active && strings.HasPrefix(msg.Namespace, c.namespace) { // 检查连接是否活动
			select {
			case c.send <- formattedMsg: // 尝试发送消息
			default:
				h.shutdownConnection(c) // 如果发送通道阻塞，则关闭连接
			}
		}
	}
}
