package main

import (
	"fmt"
	"github.com/xinjiayu/sse"
	"time"
)

func main() {
	// 创建并启动 SSE 服务器
	server := sseserver.NewServer()
	go server.Serve(":8082")

	// 定期向所有订阅的客户端发送消息
	go func() {
		for {
			message := sseserver.SSEMessage{
				Event:     "update",
				Data:      []byte(fmt.Sprintf("Current time: %s", time.Now().Format(time.RFC1123))),
				Namespace: "/sysenv/update",
			}
			server.Broadcast <- message

			time.Sleep(3 * time.Second)

		}
	}()

	// 阻塞主goroutine，以便服务器可以继续运行
	select {}
}
