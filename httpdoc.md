# SSE 服务器与现有 HTTP 服务集成指南

本指南将帮助您将 SSE 服务器功能集成到已有的 HTTP 服务中。

## 1. 基本集成步骤

### 1.1 创建 SSE 服务器实例

首先，创建一个 SSE 服务器实例，但不要直接调用它的 `Serve` 方法：

```go
sseServer := sseserver.NewServer()
```

### 1.2 将 SSE 处理器添加到现有的 HTTP 服务中

在您现有的 HTTP 服务中，添加一个新的路由来处理 SSE 请求：

```go
http.Handle("/events/", http.StripPrefix("/events", sseServer))
```

这样，SSE 服务就会处理所有发往 `/events/` 路径的请求。

### 1.3 启动您的 HTTP 服务

继续使用您原有的方式启动 HTTP 服务。

## 2. 完整示例

下面是一个完整的示例，展示如何将 SSE 服务器集成到现有的 HTTP 服务中：

```go
package main

import (
    "fmt"
    "net/http"
    "github.com/your-username/sseserver"
)

func main() {
    // 创建 SSE 服务器实例
    sseServer := sseserver.NewServer()

    // 设置常规 HTTP 路由
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, "欢迎访问主页！")
    })

    // 添加 SSE 路由
    http.Handle("/events/", http.StripPrefix("/events", sseServer))

    // 启动广播协程
    go func() {
        for {
            // 模拟定期发送消息
            msg := sseserver.SSEMessage{
                Event: "update",
                Data:  []byte("这是一条定期更新"),
            }
            sseServer.Broadcast <- msg
            // 实际应用中，这里可能是基于某些触发条件发送消息
        }
    }()

    // 启动 HTTP 服务
    fmt.Println("服务器正在监听 :8080...")
    err := http.ListenAndServe(":8080", nil)
    if err != nil {
        fmt.Printf("服务器启动失败: %s\n", err)
    }
}
```

## 3. 使用说明

1. 常规 HTTP 请求会正常处理。
2. 客户端可以通过连接到 `http://your-server:8080/events/` 来订阅 SSE 更新。
3. 您可以在任何需要的地方使用 `sseServer.Broadcast` 通道发送消息。

## 4. 注意事项

1. **路由冲突**：确保 SSE 路由 (`/events/` 在本例中) 不会与您现有的路由冲突。

2. **性能考虑**：SSE 连接是长期保持的。在高并发场景下，请确保您的服务器有足够的资源处理这些长连接。

3. **安全性**：如果需要，在 SSE 路由上添加适当的认证和授权检查。

4. **跨域问题**：如果客户端和服务器不在同一域，需要配置 CORS（跨源资源共享）。

## 5. 跨域支持

如果您需要支持跨域请求，可以在 SSE 处理器之前添加一个中间件：

```go
func corsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Access-Control-Allow-Origin", "*")
        w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

        if r.Method == "OPTIONS" {
            w.WriteHeader(http.StatusOK)
            return
        }

        next.ServeHTTP(w, r)
    })
}

// 使用中间件
http.Handle("/events/", corsMiddleware(http.StripPrefix("/events", sseServer)))
```

## 6. 结语

通过这种方式，您可以轻松地将 SSE 功能集成到现有的 HTTP 服务中，而不需要运行单独的服务器。这种集成方式既保留了原有 HTTP 服务的所有功能，又增加了实时数据推送的能力。