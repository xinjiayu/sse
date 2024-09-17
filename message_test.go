package sseserver

import (
	"bytes"
	"testing"
)

func TestSSEMessageBytes(t *testing.T) {
	testCases := []struct {
		name     string
		message  SSEMessage
		expected []byte
	}{
		{
			name: "仅数据",
			message: SSEMessage{
				Data: []byte("Hello, World!"),
			},
			expected: []byte("data:Hello, World!\n\n"),
		},
		{
			name: "带事件的消息",
			message: SSEMessage{
				Event: "update",
				Data:  []byte("New content"),
			},
			expected: []byte("event:update\ndata:New content\n\n"),
		},
		{
			name: "多行数据",
			message: SSEMessage{
				Data: []byte("Line 1\nLine 2\nLine 3"),
			},
			expected: []byte("data:Line 1\ndata:Line 2\ndata:Line 3\n\n"),
		},
		{
			name: "带事件和多行数据",
			message: SSEMessage{
				Event: "multiline",
				Data:  []byte("First line\nSecond line"),
			},
			expected: []byte("event:multiline\ndata:First line\ndata:Second line\n\n"),
		},
		{
			name: "空数据",
			message: SSEMessage{
				Event: "empty",
				Data:  []byte{},
			},
			expected: []byte("event:empty\ndata:\n\n"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.message.Bytes()
			if !bytes.Equal(result, tc.expected) {
				t.Errorf("期望 %q，但得到 %q", tc.expected, result)
			}
		})
	}
}
