package llm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const maxStreamLineSize = 1024 * 1024

// ReadContentDeltas reads OpenAI-compatible server-sent events and calls
// onDelta with each choices[].delta.content value.
func ReadContentDeltas(r io.Reader, onDelta func(string) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxStreamLineSize)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}

		delta, err := ExtractContentDelta([]byte(data))
		if err != nil {
			return err
		}
		if delta == "" {
			continue
		}
		if err := onDelta(delta); err != nil {
			return err
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read completion stream: %w", err)
	}
	return nil
}

func ExtractContentDelta(data []byte) (string, error) {
	var chunk chatCompletionChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return "", fmt.Errorf("parse completion chunk: %w", err)
	}

	for _, choice := range chunk.Choices {
		if choice.Delta.Content != nil {
			return contentValueToString(choice.Delta.Content), nil
		}
	}

	return "", nil
}

func contentValueToString(value any) string { // no clue how this works
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		var builder strings.Builder
		for _, item := range typed {
			switch part := item.(type) {
			case string:
				builder.WriteString(part)
			case map[string]any:
				if text, ok := part["text"].(string); ok {
					builder.WriteString(text)
				}
			}
		}
		return builder.String()
	default:
		return ""
	}
}

// `json:thing` means golang writes this in json keyed by <thing>
type chatCompletionChunk struct {
	Choices []struct {
		Delta struct {
			Content any `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}
