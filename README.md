# sse server

SSE（Server-Sent Events）服务器是一个高性能的实时数据推送解决方案，适用于需要服务器向客户端推送实时更新的场景。本手册将指导您如何安装、配置和使用这个 SSE 服务器。

使用方式参：

example 下的示例代码

## 安装

### 前置条件

- Go 1.16 或更高版本

### 安装步骤

1. 使用 `go get` 命令安装 SSE 服务器包：

   ```
   go get github.com/xinjiayu/sse
   ```

2. 在您的项目中导入 SSE 服务器包：

   ```go
   import "github.com/xinjiayu/sse"
   ```

## 基本使用

### 创建和启动服务器

```go
package main

import (
    "log"
    "github.com/xinjiayu/sse"
)

func main() {
    // 创建新的 SSE 服务器实例
    server := sseserver.NewServer()

    // 启动服务器，监听 8080 端口
    log.Fatal(server.Serve(":8080"))
}
```

### 发送消息

要向所有连接的客户端发送消息，使用 `Broadcast` 通道：

```go
message := sseserver.SSEMessage{
    Event: "update",
    Data:  []byte("这是一条实时更新消息"),
}
server.Broadcast <- message
```

### 客户端订阅

客户端可以通过访问 `/subscribe/` 端点来订阅 SSE 更新。例如：

```javascript
let eventSource = new EventSource('http://your-server:8080/subscribe/');
eventSource.onmessage = function(event) {
    console.log('收到更新:', event.data);
};
```

## 高级配置

### 调试模式

启用调试模式以获取详细的日志输出：

```go
server := sseserver.NewServer()
server.Debug = true
```

### 自定义路由

SSE 服务器使用默认的 `/subscribe/` 路径处理订阅请求。如需自定义，可以在启动服务器前设置：

```go
http.Handle("/custom-sse/", http.StripPrefix("/custom-sse", server))
```

### CORS 设置

SSE 服务器支持配置跨源资源共享（CORS）选项，允许您控制哪些域可以访问您的 SSE 端点。以下是如何配置 CORS 的说明：

#### 默认 CORS 设置

默认情况下，SSE 服务器允许来自所有域的请求。如果您不指定 CORS 选项，服务器将使用以下默认设置：

```go
server := sseserver.NewServer()
```

这将允许所有域访问您的 SSE 端点。

#### 自定义 CORS 设置

要自定义 CORS 设置，您可以在创建服务器时提供 `CorsOptions`：

```go
server := sseserver.NewServer(sseserver.ServerOptions{
    CorsOptions: &sseserver.CorsOptions{
        AllowedOrigins: []string{"https://example.com", "https://api.example.com"},
        AllowedMethods: []string{"GET", "OPTIONS"},
        AllowedHeaders: []string{"Content-Type", "Authorization"},
        MaxAge:         3600,
    },
})
```

- `AllowedOrigins`: 指定允许访问的域名列表。使用 `*` 允许所有域。
- `AllowedMethods`: 指定允许的 HTTP 方法。
- `AllowedHeaders`: 指定允许的 HTTP 头。
- `MaxAge`: 指定预检请求结果的缓存时间（秒）。

#### 动态 CORS 设置

如果您需要更动态的 CORS 控制，您可以实现自己的 CORS 中间件并将其应用到服务器：

```go
func customCorsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // 实现自定义的 CORS 逻辑
        origin := r.Header.Get("Origin")
        if /* 自定义条件 */ {
            w.Header().Set("Access-Control-Allow-Origin", origin)
        }
        // 设置其他 CORS 头...
        next.ServeHTTP(w, r)
    })
}

// 应用自定义中间件
http.Handle("/subscribe/", customCorsMiddleware(server))
```

#### CORS 配置示例

1. 允许特定域：

```go
server := sseserver.NewServer(sseserver.ServerOptions{
    CorsOptions: &sseserver.CorsOptions{
        AllowedOrigins: []string{"https://app.example.com"},
    },
})
```

2. 允许多个域：

```go
server := sseserver.NewServer(sseserver.ServerOptions{
    CorsOptions: &sseserver.CorsOptions{
        AllowedOrigins: []string{"https://app1.example.com", "https://app2.example.com"},
    },
})
```

3. 允许所有子域：

```go
server := sseserver.NewServer(sseserver.ServerOptions{
    CorsOptions: &sseserver.CorsOptions{
        AllowedOrigins: []string{"*.example.com"},
    },
})
```

注意：在生产环境中，建议明确指定允许的域，而不是使用通配符（`*`），以增强安全性。

## 最佳实践

1. **错误处理**：始终检查并处理 `Serve` 方法返回的错误。

2. **消息格式**：使用结构化的消息格式（如 JSON）作为 `SSEMessage` 的 `Data` 字段，以便客户端易于解析。

3. **安全性**：在生产环境中，考虑使用 HTTPS 和适当的认证机制来保护您的 SSE 端点。

4. **客户端重连**：建议客户端实现自动重连逻辑，以处理可能的连接中断。

## 常见问题解答

Q: 服务器支持多少并发连接？
A: 服务器设计用于处理大量并发连接。具体数量取决于服务器硬件和网络条件。

Q: 如何处理客户端断开连接？
A: 服务器会自动检测并清理断开的连接，您无需手动处理。

Q: 可以向特定客户端发送消息吗？
A: 当前版本支持向所有连接的客户端广播消息。如需点对点消息功能，请考虑使用其他解决方案。

## 故障排除

如果遇到问题：

1. 检查服务器日志，特别是在启用调试模式的情况下。
2. 确保客户端能够访问服务器地址和端口。
3. 验证防火墙设置是否允许 SSE 连接。

如果问题持续，请查阅项目的 GitHub 仓库或联系技术支持。

## 稳定性压测（Soak Test）

项目内置了长时稳定性测试，可用于验证高并发下的长期运行能力（连接收敛、goroutine 收敛、消息吞吐）。

默认跳过，开启方式：

```bash
SOAK=1 SOAK_DURATION_SEC=1800 SOAK_CLIENTS=200 SOAK_MSG_PER_SEC=300 go test -run TestSoak_LongRun -v ./...
```

常用参数：

- `SOAK=1`：开启 soak 测试
- `SOAK_DURATION_SEC`：持续时长（秒），如 `1800` 表示 30 分钟
- `SOAK_CLIENTS`：并发连接数
- `SOAK_MSG_PER_SEC`：每秒推送消息数
- `SOAK_REPORT_SEC`：日志打点间隔（秒）

建议先从小规模参数开始，再逐步放大至目标负载。

## 其它

如果想将sse服务集成到你已有的HTTP服务中，请参访问 【[SSE服务器与现有 HTTP 服务集成指南](httpdoc.md)】