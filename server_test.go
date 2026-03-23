package sseserver

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewServer(t *testing.T) {
	server := NewServer()
	if server == nil {
		t.Fatal("NewServer() 返回了 nil")
	}
	if server.hub == nil {
		t.Error("服务器的 hub 未初始化")
	}
}

func TestConnectionHandler_Headers(t *testing.T) {
	server := NewServer()

	ts := httptest.NewServer(server.connectionHandler())
	defer ts.Close()
	defer server.Stop()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type 错误，得到 %v，想要 text/event-stream", ct)
	}
}

func TestServeHTTP(t *testing.T) {
	server := NewServer()
	server.Debug = true

	ts := httptest.NewServer(http.HandlerFunc(server.ServeHTTP))
	defer ts.Close()
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/subscribe/", nil)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// 等待连接建立
	time.Sleep(100 * time.Millisecond)

	// 发送测试消息
	server.Broadcast <- SSEMessage{Event: "test", Data: []byte("Hello, SSE!")}

	// 读取响应
	reader := bufio.NewReader(resp.Body)
	var lines []string
	for i := 0; i < 3; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("读取响应失败: %v", err)
		}
		lines = append(lines, line)
	}
	response := strings.Join(lines, "")
	expected := "event:test\ndata:Hello, SSE!\n\n"
	if response != expected {
		t.Errorf("没有收到预期的 SSE 消息。得到: %q, 想要: %q", response, expected)
	}
}

func TestBroadcast(t *testing.T) {
	server := NewServer()
	server.Debug = true

	ts := httptest.NewServer(http.HandlerFunc(server.ServeHTTP))
	defer ts.Close()
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/subscribe/", nil)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	if count := server.GetActiveConnectionCount(); count != 1 {
		t.Errorf("预期的活跃连接数为 1，但得到 %d", count)
	}

	server.Broadcast <- SSEMessage{Event: "test", Data: []byte("Hello, SSE!")}

	reader := bufio.NewReader(resp.Body)
	var lines []string
	for i := 0; i < 3; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("读取响应失败: %v", err)
		}
		lines = append(lines, line)
	}
	response := strings.Join(lines, "")
	expected := "event:test\ndata:Hello, SSE!\n\n"
	if response != expected {
		t.Errorf("没有收到预期的 SSE 消息。得到: %q, 想要: %q", response, expected)
	}
}

func TestStopServer(t *testing.T) {
	server := NewServer()

	go server.Serve(":8080")
	time.Sleep(100 * time.Millisecond)

	server.hub.Stop()

	select {
	case <-server.hub.stopChan:
	default:
		t.Error("服务器的 stopChan 未被关闭")
	}
}

func TestBroadcastAfterReconnect(t *testing.T) {
	server := NewServer()

	ts := httptest.NewServer(http.HandlerFunc(server.ServeHTTP))
	defer ts.Close()
	defer server.Stop()

	// 第一个连接，随后主动断开
	ctx1, cancel1 := context.WithCancel(context.Background())
	req1, _ := http.NewRequestWithContext(ctx1, "GET", ts.URL+"/subscribe/", nil)
	client := &http.Client{}
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	cancel1()
	resp1.Body.Close()
	time.Sleep(150 * time.Millisecond)

	// 第二个连接
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	req2, _ := http.NewRequestWithContext(ctx2, "GET", ts.URL+"/subscribe/", nil)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	time.Sleep(100 * time.Millisecond)

	server.Broadcast <- SSEMessage{Event: "reconnect", Data: []byte("ok")}

	reader := bufio.NewReader(resp2.Body)
	var lines []string
	for i := 0; i < 3; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("读取响应失败: %v", err)
		}
		lines = append(lines, line)
	}
	got := strings.Join(lines, "")
	if !strings.Contains(got, "event:reconnect\ndata:ok\n\n") {
		t.Fatalf("重连后广播失效，响应: %s", got)
	}
}
