package sseserver

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestNewHub(t *testing.T) {
	h := newHub()
	if h == nil {
		t.Fatal("newHub() 返回了 nil")
	}
	if h.connections == nil {
		t.Error("connections map 未初始化")
	}
	if h.broadcast == nil {
		t.Error("broadcast 通道未初始化")
	}
	if h.register == nil {
		t.Error("register 通道未初始化")
	}
	if h.unregister == nil {
		t.Error("unregister 通道未初始化")
	}
}

func TestHubStart(t *testing.T) {
	h := newHub()
	startCalled := false
	stopCalled := false

	start := func() { startCalled = true }
	stop := func() { stopCalled = true }

	h.Start(start, stop, true)

	// 注册一个连接
	conn := &connection{send: make(chan []byte, 1)}
	h.register <- conn

	// 等待处理
	time.Sleep(100 * time.Millisecond)

	if !startCalled {
		t.Error("start 函数未被调用")
	}

	if atomic.LoadInt32(&h.activeCount) != 1 {
		t.Error("活跃连接数未正确增加")
	}

	// 注销连接
	h.unregister <- conn

	// 等待处理
	time.Sleep(100 * time.Millisecond)

	if !stopCalled {
		t.Error("stop 函数未被调用")
	}

	if atomic.LoadInt32(&h.activeCount) != 0 {
		t.Error("活跃连接数未正确减少")
	}
}

func TestHubBroadcast(t *testing.T) {
	h := newHub()
	h.Start(func() {}, func() {}, true)

	conn := &connection{
		send: make(chan []byte, 1),
		hub:  h, // 确保设置 hub 字段
	}
	h.register <- conn

	// 等待连接注册
	time.Sleep(100 * time.Millisecond)

	// 广播消息
	msg := SSEMessage{Event: "test", Data: []byte("Hello, World!")}
	h.broadcast <- msg

	// 增加等待时间
	time.Sleep(200 * time.Millisecond)

	// 检查消息是否被发送到连接
	select {
	case receivedMsg := <-conn.send:
		expectedMsg := "event:test\ndata:Hello, World!\n\n"
		if string(receivedMsg) != expectedMsg {
			t.Errorf("广播的消息不匹配，得到 %s，想要 %s", string(receivedMsg), expectedMsg)
		}
	default:
		t.Error("未收到广播消息")
	}
}

func TestHubStop(t *testing.T) {
	h := newHub()
	h.Start(func() {}, func() {}, true)

	// 停止 hub
	h.Stop()

	// 检查 stopChan 是否被关闭
	select {
	case <-h.stopChan:
		// 正确：stopChan 被关闭
	default:
		t.Error("stopChan 未被关闭")
	}

	// 尝试再次停止，确保不会 panic
	h.Stop()
}
