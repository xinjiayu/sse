package sseserver

import (
	"strings"
)

type SSEMessage struct {
	Event     string
	Data      []byte
	Namespace string
}

func (msg SSEMessage) Bytes() []byte {
	var sb strings.Builder

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
