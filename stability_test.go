package sseserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"
)

func waitUntil(t *testing.T, timeout time.Duration, fn func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf(msg)
}

func TestStability_ConnectionChurnAndBroadcast(t *testing.T) {
	if testing.Short() {
		t.Skip("skip stability test in short mode")
	}

	server := NewServer()
	ts := httptest.NewServer(http.HandlerFunc(server.ServeHTTP))
	defer ts.Close()
	defer server.Stop()

	baseG := runtime.NumGoroutine()
	client := &http.Client{Timeout: 2 * time.Second}
	url := ts.URL + "/subscribe/"

	// 持续广播，模拟业务高频推送。
	broadcastCtx, cancelBroadcast := context.WithCancel(context.Background())
	var broadcasterWG sync.WaitGroup
	broadcasterWG.Add(1)
	go func() {
		defer broadcasterWG.Done()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-broadcastCtx.Done():
				return
			case <-ticker.C:
				select {
				case server.Receive <- SSEMessage{Event: "tick", Data: []byte("x")}:
				case <-broadcastCtx.Done():
					return
				}
			}
		}
	}()

	// 连接震荡：并发快速建立/断开连接，覆盖高并发短连接场景。
	const workers = 40
	const loopsPerWorker = 20
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < loopsPerWorker; j++ {
				reqCtx, cancel := context.WithTimeout(context.Background(), 180*time.Millisecond)
				req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
				if err != nil {
					cancel()
					continue
				}
				resp, err := client.Do(req)
				if err == nil {
					_ = resp.Body.Close()
				}
				cancel()
			}
		}()
	}
	wg.Wait()

	cancelBroadcast()
	broadcasterWG.Wait()

	waitUntil(t, 3*time.Second, func() bool {
		return server.GetActiveConnectionCount() == 0
	}, "连接震荡结束后活跃连接未归零")

	// 允许测试基础设施带来小幅波动，但不应明显增长。
	waitUntil(t, 3*time.Second, func() bool {
		return runtime.NumGoroutine() <= baseG+25
	}, "goroutine 数量未在预期范围内收敛，疑似存在泄漏")
}

func TestStability_SlowConsumerIsolation(t *testing.T) {
	h := newHub()
	h.Start(func() {}, func() {}, false)
	defer h.Stop()

	// 构造一个极小缓冲慢消费者，确保快速触发背压。
	conn := h.newConnection()
	conn.send = make(chan []byte, 1)
	h.register <- conn

	waitUntil(t, 2*time.Second, func() bool {
		return h.GetActiveConnectionCount() == 1
	}, "慢消费者连接未建立")

	for i := 0; i < 200; i++ {
		h.broadcast <- SSEMessage{Event: "burst", Data: []byte("payload")}
	}

	waitUntil(t, 3*time.Second, func() bool {
		return h.GetActiveConnectionCount() == 0
	}, "慢消费者未被隔离剔除，可能影响系统稳定性")
}

func TestStability_StopConvergence(t *testing.T) {
	server := NewServer()
	// 建立一批活跃连接（使用本地 handler，避免网络层对 SSE 首包行为的干扰）。
	const conns = 20
	cancels := make([]context.CancelFunc, 0, conns)
	for i := 0; i < conns; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancels = append(cancels, cancel)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/subscribe/", nil)
		if err != nil {
			t.Fatalf("create request failed: %v", err)
		}
		rr := httptest.NewRecorder()
		go server.ServeHTTP(rr, req)
	}
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()

	waitUntil(t, 2*time.Second, func() bool {
		return server.GetActiveConnectionCount() == conns
	}, "连接未按预期建立")

	if err := server.Stop(); err != nil {
		t.Fatalf("server stop failed: %v", err)
	}

	waitUntil(t, 3*time.Second, func() bool {
		return server.GetActiveConnectionCount() == 0
	}, "Stop 后连接未收敛到 0")
}
