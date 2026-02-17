package sseserver

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// TestSoak_LongRun 默认跳过，通过 SOAK=1 开启：
// SOAK=1 SOAK_DURATION_SEC=1800 SOAK_CLIENTS=200 SOAK_MSG_PER_SEC=300 go test -run TestSoak_LongRun -v ./...
func TestSoak_LongRun(t *testing.T) {
	if os.Getenv("SOAK") != "1" {
		t.Skip("set SOAK=1 to enable long-run soak test")
	}

	durationSec := envInt("SOAK_DURATION_SEC", 60)
	clients := envInt("SOAK_CLIENTS", 80)
	msgPerSec := envInt("SOAK_MSG_PER_SEC", 200)
	reportSec := envInt("SOAK_REPORT_SEC", 10)

	t.Logf("soak config: duration=%ds clients=%d msg_per_sec=%d report_sec=%d", durationSec, clients, msgPerSec, reportSec)

	server := NewServer()
	ts := httptest.NewServer(http.HandlerFunc(server.ServeHTTP))
	defer ts.Close()
	defer func() {
		_ = server.Stop()
	}()

	startG := runtime.NumGoroutine()

	var sent uint64
	var recvDataLines uint64
	var recvEvents uint64

	// 建立长连接客户端
	rootCtx, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()

	client := &http.Client{
		Timeout: 0, // SSE 长连接
	}
	url := ts.URL + "/subscribe/"

	var clientWG sync.WaitGroup
	clientWG.Add(clients)
	for i := 0; i < clients; i++ {
		go func() {
			defer clientWG.Done()
			req, err := http.NewRequestWithContext(rootCtx, http.MethodGet, url, nil)
			if err != nil {
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			reader := bufio.NewReader(resp.Body)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "event:") {
					atomic.AddUint64(&recvEvents, 1)
				}
				if strings.HasPrefix(line, "data:") {
					atomic.AddUint64(&recvDataLines, 1)
				}
			}
		}()
	}

	waitUntil(t, 5*time.Second, func() bool {
		return int(server.GetActiveConnectionCount()) == clients
	}, "soak 客户端未在预期时间内全部连上")

	// 推送协程
	runCtx, cancelRun := context.WithTimeout(rootCtx, time.Duration(durationSec)*time.Second)
	defer cancelRun()

	var pubWG sync.WaitGroup
	pubWG.Add(1)
	go func() {
		defer pubWG.Done()
		interval := time.Second / time.Duration(msgPerSec)
		if interval <= 0 {
			interval = time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-runCtx.Done():
				return
			case now := <-ticker.C:
				payload := []byte(fmt.Sprintf(`{"ts":%d}`, now.UnixNano()))
				select {
				case server.Receive <- SSEMessage{Event: "soak", Data: payload}:
					atomic.AddUint64(&sent, 1)
				case <-runCtx.Done():
					return
				}
			}
		}
	}()

	// 期间定期打点
	reportTicker := time.NewTicker(time.Duration(reportSec) * time.Second)
	defer reportTicker.Stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-runCtx.Done():
				return
			case <-reportTicker.C:
				t.Logf("[soak] sent=%d recv_events=%d recv_data_lines=%d active_conn=%d goroutines=%d",
					atomic.LoadUint64(&sent),
					atomic.LoadUint64(&recvEvents),
					atomic.LoadUint64(&recvDataLines),
					server.GetActiveConnectionCount(),
					runtime.NumGoroutine(),
				)
			}
		}
	}()

	<-runCtx.Done()
	pubWG.Wait()
	cancelAll()
	clientWG.Wait()
	<-done

	if err := server.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}

	waitUntil(t, 5*time.Second, func() bool {
		return server.GetActiveConnectionCount() == 0
	}, "soak 结束后活跃连接未收敛为 0")

	endG := runtime.NumGoroutine()
	if endG > startG+40 {
		t.Fatalf("goroutine 未收敛: start=%d end=%d", startG, endG)
	}

	if atomic.LoadUint64(&sent) == 0 {
		t.Fatalf("soak 未发送任何消息")
	}
	if atomic.LoadUint64(&recvDataLines) == 0 {
		t.Fatalf("soak 客户端未收到任何 data 行")
	}

	t.Logf("[soak-final] sent=%d recv_events=%d recv_data_lines=%d startG=%d endG=%d",
		atomic.LoadUint64(&sent),
		atomic.LoadUint64(&recvEvents),
		atomic.LoadUint64(&recvDataLines),
		startG, endG,
	)
}
