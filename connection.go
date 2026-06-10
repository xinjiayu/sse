package sseserver

import (
	"sync"
	"time"
)

const ConnectionTimeout = 30 * time.Minute

type connection struct {
	send         chan []byte
	hub          *hub
	createdAt    time.Time
	lastActivity time.Time
	mu           sync.Mutex
	closed       bool      // 标记 send channel 是否已关闭
	closeOnce    sync.Once // 确保 channel 只关闭一次
}

// reset 重置连接状态，用于从 sync.Pool 复用时初始化
// 必须在获取连接后、使用前调用
func (c *connection) reset() {
	c.mu.Lock()
	c.closed = false
	c.mu.Unlock()
	// 重置 closeOnce，使其可以再次执行
	c.closeOnce = sync.Once{}
}

func (c *connection) updateActivity() {
	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()
}

func (c *connection) isInactive(timeout time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Since(c.lastActivity) > timeout
}

// isExpired 检查连接是否已过期
func (c *connection) isExpired() bool {
	return c.isInactive(ConnectionTimeout)
}

// safeClose 安全地关闭 send channel，确保只关闭一次，防止 panic
func (c *connection) safeClose() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		close(c.send)
		c.mu.Unlock()
	})
}

// isClosed 检查连接是否已关闭
func (c *connection) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// trySend 尝试非阻塞发送消息到连接，返回是否成功。
// 持有 mutex 期间完成发送，与 safeClose 互斥，避免向已关闭的 channel 发送。
func (c *connection) trySend(data []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	select {
	case c.send <- data:
		return true
	default:
		return false
	}
}
