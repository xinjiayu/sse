package sseserver

type SSEMessage struct {
	Event     string
	Data      []byte
	Namespace string
}

func (msg SSEMessage) Bytes() []byte {
	// 预计算总长度，单次分配
	size := 0
	if msg.Event != "" {
		size += 6 + len(msg.Event) + 1 // "event:" + event + "\n"
	}
	if msg.Namespace != "" {
		size += 10 + len(msg.Namespace) + 1 // "namespace:" + ns + "\n"
	}

	dataLen := len(msg.Data)
	nlCount := 0
	for _, b := range msg.Data {
		if b == '\n' {
			nlCount++
		}
	}
	lines := nlCount + 1
	// lines * "data:" + data bytes + lines * "\n" + final "\n"
	size += lines*5 + dataLen + lines + 1

	buf := make([]byte, 0, size)

	if msg.Event != "" {
		buf = append(buf, "event:"...)
		buf = append(buf, msg.Event...)
		buf = append(buf, '\n')
	}
	if msg.Namespace != "" {
		buf = append(buf, "namespace:"...)
		buf = append(buf, msg.Namespace...)
		buf = append(buf, '\n')
	}

	// 直接操作 []byte，避免 string 转换
	start := 0
	for i, b := range msg.Data {
		if b == '\n' {
			buf = append(buf, "data:"...)
			buf = append(buf, msg.Data[start:i]...)
			buf = append(buf, '\n')
			start = i + 1
		}
	}
	buf = append(buf, "data:"...)
	buf = append(buf, msg.Data[start:]...)
	buf = append(buf, '\n')

	buf = append(buf, '\n')
	return buf
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
