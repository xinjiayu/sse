package sseserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
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
	server.Debug = true

	req, err := http.NewRequest("GET", "/subscribe/", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()

	// 在后台运行 ServeHTTP
	go server.ServeHTTP(rr, req)

	// 等待连接建立
	time.Sleep(100 * time.Millisecond)

	// 发送测试消息
	testMessage := SSEMessage{
		Event: "test",
		Data:  []byte("Hello, SSE!"),
	}
	server.Broadcast <- testMessage

	// 给予更长的等待时间
	time.Sleep(500 * time.Millisecond)

	// 检查响应
	response := rr.Body.String()
	expected := "event:test\ndata:Hello, SSE!\n\n"
	if !strings.Contains(response, expected) {
		t.Errorf("没有收到预期的 SSE 消息。得到的响应：%s", response)
	}
}

func TestBroadcast(t *testing.T) {
	server := NewServer()
	server.Debug = true

	// 模拟客户端连接
	req, _ := http.NewRequest("GET", "/subscribe/", nil)
	rr := httptest.NewRecorder()
	go server.ServeHTTP(rr, req)

	// 给予时间让连接建立
	time.Sleep(100 * time.Millisecond)

	// 验证活跃连接数
	if count := server.GetActiveConnectionCount(); count != 1 {
		t.Errorf("预期的活跃连接数为 1，但得到 %d", count)
	}

	// 发送消息
	testMessage := SSEMessage{Event: "test", Data: []byte("Hello, SSE!")}
	server.Broadcast <- testMessage

	// 给予更长的时间让消息被处理
	time.Sleep(500 * time.Millisecond)

	// 验证消息是否被发送
	response := rr.Body.String()
	expected := "event:test\ndata:Hello, SSE!\n\n"
	if !strings.Contains(response, expected) {
		t.Errorf("没有收到预期的 SSE 消息。得到的响应：%s", response)
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
