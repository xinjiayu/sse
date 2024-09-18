package sseserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConnectionHandler(t *testing.T) {
	server := NewServer()
	handler := server.connectionHandler()

	// 创建一个测试请求
	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 创建一个 ResponseRecorder 来记录响应
	rr := httptest.NewRecorder()

	// 调用处理程序
	go handler.ServeHTTP(rr, req)

	// 等待一段时间以确保连接建立
	time.Sleep(100 * time.Millisecond)

	// 检查响应头
	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type 错误，得到 %v，想要 text/event-stream", ct)
	}

	// 检查连接是否被注册
	if server.GetActiveConnectionCount() != 1 {
		t.Errorf("活跃连接数错误，得到 %d，想要 1", server.GetActiveConnectionCount())
	}
}

func TestConnectionWrite(t *testing.T) {
	h := newHub()
	conn := h.newConnection()

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
	conn := h.newConnection()

	// 关闭连接
	conn.close()

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
