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

// 添加 isExpired 方法，与 isInactive 保持一致
func (c *connection) isExpired() bool {
	return c.isInactive(ConnectionTimeout)
}

func (c *connection) close() {
	c.hub.unregister <- c
	close(c.send)
}

func (c *connection) write(msg []byte) {
	c.updateActivity()
	select {
	case c.send <- msg:
	default:
		c.close()
	}
}
