package sseserver

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestConnectionHandler(t *testing.T) {
	h := newHub()
	go h.run(func() {}, func() {}) // 确保 hub 在运行

	handler := connectionHandler(h)

	// 创建一个测试请求
	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 创建一个 ResponseRecorder 来记录响应
	rr := httptest.NewRecorder()

	// 在一个 goroutine 中调用处理程序
	go handler.ServeHTTP(rr, req)

	// 等待连接建立和注册
	time.Sleep(500 * time.Millisecond)

	// 检查活跃连接数
	if count := atomic.LoadInt32(&h.activeCount); count != 1 {
		t.Errorf("活跃连接数错误，得到 %d，想要 1", count)
	}

	// 停止 hub
	close(h.stopChan)
}

func TestConnectionWrite(t *testing.T) {
	h := newHub()
	conn := &connection{
		send: make(chan []byte, sendBufferSize),
		hub:  h,
	}

	// 写入消息
	message := []byte("test message")
	conn.write(message)

	// 检查消息是否被写入
	select {
	case receivedMsg := <-conn.send:
		if string(receivedMsg) != string(message) {
			t.Errorf("写入的消息不匹配，得到 %s，想要 %s", string(receivedMsg), string(message))
		}
	case <-time.After(time.Second):
		t.Error("写入消息超时")
	}
}

func TestConnectionClose(t *testing.T) {
	h := newHub()
	conn := &connection{
		send: make(chan []byte, sendBufferSize),
		hub:  h,
	}

	// 关闭连接
	conn.close()

	// 检查 closed 标志
	if !conn.closed {
		t.Error("连接未被正确标记为关闭")
	}

	// 检查 send 通道是否被关闭
	select {
	case _, ok := <-conn.send:
		if ok {
			t.Error("send 通道未被关闭")
		}
	default:
		t.Error("send 通道未被关闭")
	}
}
