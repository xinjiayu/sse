package sseserver

import (
	"strings"
)

type SSEMessage struct {
	Event     string
	Data      []byte
	Namespace string
}

func (msg SSEMessage) sseFormat() []byte {
	var sb strings.Builder

	// 预估所需容量：event字段和data字段的长度加上固定的其他字符长度
	estimatedCapacity := len(msg.Event) + len(msg.Data)*2 + 20 // 20 是额外字符的大概长度
	sb.Grow(estimatedCapacity)

	if msg.Event != "" {
		sb.WriteString("event:")
		sb.WriteString(msg.Event)
		sb.WriteRune('\n')
	}

	dataLines := strings.Split(string(msg.Data), "\n")
	for _, line := range dataLines {
		sb.WriteString("data:")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	return []byte(sb.String())
}
