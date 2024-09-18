package sseserver

import (
	"fmt"
	"strings"
)

type SSEMessage struct {
	Event     string
	Data      []byte
	Namespace string
}

func (msg SSEMessage) Bytes() []byte {
	var builder strings.Builder

	if msg.Event != "" {
		builder.WriteString(fmt.Sprintf("event:%s\n", msg.Event))
	}

	if msg.Namespace != "" {
		builder.WriteString(fmt.Sprintf("namespace:%s\n", msg.Namespace))
	}

	dataStr := string(msg.Data)
	dataLines := strings.Split(dataStr, "\n")
	for _, line := range dataLines {
		builder.WriteString(fmt.Sprintf("data:%s\n", line))
	}

	builder.WriteString("\n")

	return []byte(builder.String())
}

// NewSSEMessage 创建一个新的 SSEMessage
func NewSSEMessage(event string, data []byte, namespace string) SSEMessage {
	return SSEMessage{
		Event:     event,
		Data:      data,
		Namespace: namespace,
	}
}

// IsValid 检查 SSEMessage 是否有效
func (msg SSEMessage) IsValid() bool {
	return len(msg.Data) > 0
}
