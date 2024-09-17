package sseserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewServer(t *testing.T) {
	// 测试创建新服务器
	server := NewServer()
	if server == nil {
		t.Fatal("NewServer() 返回了 nil")
	}
	if server.hub == nil {
		t.Error("服务器的 hub 未初始化")
	}
}

func TestServeHTTP(t *testing.T) {
	server := NewServer()

	// 创建一个测试请求
	req, err := http.NewRequest("GET", "/subscribe/", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 创建一个 ResponseRecorder 来记录响应
	rr := httptest.NewRecorder()

	// 调用 ServeHTTP 方法
	server.ServeHTTP(rr, req)

	// 检查状态码
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("处理程序返回了错误的状态码：得到 %v 想要 %v", status, http.StatusOK)
	}

	// 检查 Content-Type
	expectedContentType := "text/event-stream"
	if contentType := rr.Header().Get("Content-Type"); contentType != expectedContentType {
		t.Errorf("处理程序返回了错误的 Content-Type：得到 %v 想要 %v", contentType, expectedContentType)
	}
}

func TestBroadcast(t *testing.T) {
	server := NewServer()

	// 启动一个 goroutine 来模拟客户端连接
	go func() {
		req, _ := http.NewRequest("GET", "/subscribe/", nil)
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, req)
	}()

	// 等待一段时间以确保连接建立
	time.Sleep(100 * time.Millisecond)

	// 发送一条消息
	testMessage := SSEMessage{Event: "test", Data: []byte("Hello, SSE!")}
	server.Broadcast <- testMessage

	// 等待消息被处理
	time.Sleep(100 * time.Millisecond)

	// 检查 hub 的 activeCount
	if server.hub.activeCount != 1 {
		t.Errorf("预期的活跃连接数为 1，但得到 %d", server.hub.activeCount)
	}
}

func TestStopServer(t *testing.T) {
	server := NewServer()

	// 启动服务器
	go server.Serve(":8080")

	// 等待服务器启动
	time.Sleep(100 * time.Millisecond)

	// 停止服务器
	server.hub.Stop()

	// 检查 stopChan 是否被关闭
	select {
	case <-server.hub.stopChan:
		// 正确：stopChan 被关闭
	default:
		t.Error("服务器的 stopChan 未被关闭")
	}
}
