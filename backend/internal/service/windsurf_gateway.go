package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/windsurf"
	"github.com/gin-gonic/gin"
)

// forwardWindsurfMessages 处理 Windsurf 平台的请求转发
func (s *GatewayService) forwardWindsurfMessages(ctx context.Context, c *gin.Context, account *Account, parsed *ParsedRequest, startTime time.Time) (*ForwardResult, error) {
	if s.windsurfRuntime == nil {
		return nil, fmt.Errorf("windsurf runtime not initialized")
	}

	apiKey := account.GetCredential("api_key")
	if apiKey == "" {
		return nil, fmt.Errorf("windsurf account missing api_key credential")
	}

	// 模型映射
	originalModel := parsed.Model
	model := originalModel
	if mapped := account.GetMappedModel(model); mapped != "" {
		model = mapped
	}

	// 提取消息文本
	message := extractWindsurfMessage(parsed.Body)
	if message == "" {
		return nil, fmt.Errorf("empty message in request")
	}

	// 构建选项
	options := buildWindsurfOptions(parsed.Body)

	if parsed.Stream {
		return s.forwardWindsurfStream(ctx, c, apiKey, model, originalModel, message, options, startTime)
	}
	return s.forwardWindsurfSync(ctx, c, apiKey, model, originalModel, message, options, startTime)
}

func (s *GatewayService) forwardWindsurfSync(ctx context.Context, c *gin.Context, apiKey, model, originalModel, message string, options *windsurf.SendCascadeMessageOptions, startTime time.Time) (*ForwardResult, error) {
	result, err := s.windsurfRuntime.Execute(ctx, apiKey, model, message, options)
	if err != nil {
		return nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway}
	}

	respJSON := result.ToOpenAIChatCompletion(fmt.Sprintf("%d", time.Now().UnixNano()))
	c.Header("Content-Type", "application/json")
	c.Data(http.StatusOK, "application/json", respJSON)

	return &ForwardResult{
		Usage:    windsurfUsageToClaude(result.Usage),
		Model:    originalModel,
		Stream:   false,
		Duration: time.Since(startTime),
	}, nil
}

func (s *GatewayService) forwardWindsurfStream(ctx context.Context, c *gin.Context, apiKey, model, originalModel, message string, options *windsurf.SendCascadeMessageOptions, startTime time.Time) (*ForwardResult, error) {
	events := make(chan windsurf.PollEvent, 64)

	// 设置 SSE headers
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	var result *windsurf.CascadeResult
	var execErr error

	go func() {
		defer close(events)
		result, execErr = s.windsurfRuntime.ExecuteStream(ctx, apiKey, model, message, options, events)
	}()

	requestID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	var firstTokenMs *int

	for event := range events {
		switch event.Type {
		case windsurf.PollEventTextDelta:
			if firstTokenMs == nil {
				ms := int(time.Since(startTime).Milliseconds())
				firstTokenMs = &ms
			}
			chunk := buildSSEChunk(requestID, model, event.TextDelta, "")
			fmt.Fprintf(c.Writer, "data: %s\n\n", chunk)
			c.Writer.Flush()
		case windsurf.PollEventNativeToolCall:
			if event.ToolCall != nil {
				if firstTokenMs == nil {
					ms := int(time.Since(startTime).Milliseconds())
					firstTokenMs = &ms
				}
				chunk := buildSSEToolCallChunk(requestID, model, event.ToolCall)
				fmt.Fprintf(c.Writer, "data: %s\n\n", chunk)
				c.Writer.Flush()
			}
		case windsurf.PollEventHeartbeat:
			fmt.Fprintf(c.Writer, ": ping\n\n")
			c.Writer.Flush()
		}
	}

	// 发送结束标记
	finishReason := "stop"
	if result != nil && result.FinishReason != "" {
		finishReason = result.FinishReason
	}
	finishChunk := buildSSEChunk(requestID, model, "", finishReason)
	fmt.Fprintf(c.Writer, "data: %s\n\n", finishChunk)
	fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()

	if execErr != nil {
		return nil, execErr
	}

	var usage ClaudeUsage
	if result != nil {
		usage = windsurfUsageToClaude(result.Usage)
	}

	return &ForwardResult{
		Usage:        usage,
		Model:        originalModel,
		Stream:       true,
		Duration:     time.Since(startTime),
		FirstTokenMs: firstTokenMs,
	}, nil
}

func windsurfUsageToClaude(usage *windsurf.CascadeUsage) ClaudeUsage {
	if usage == nil {
		return ClaudeUsage{}
	}
	return ClaudeUsage{
		InputTokens:              int(usage.InputTokens),
		OutputTokens:             int(usage.OutputTokens),
		CacheCreationInputTokens: int(usage.CacheWriteTokens),
		CacheReadInputTokens:     int(usage.CacheReadTokens),
	}
}

func extractWindsurfMessage(body []byte) string {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	messages, ok := req["messages"].([]any)
	if !ok || len(messages) == 0 {
		return ""
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role == "user" || role == "tool" {
			return extractContentText(msg["content"])
		}
	}
	if last, ok := messages[len(messages)-1].(map[string]any); ok {
		return extractContentText(last["content"])
	}
	return ""
}

func extractContentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if text, ok := m["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func buildWindsurfOptions(body []byte) *windsurf.SendCascadeMessageOptions {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return nil
	}
	options := &windsurf.SendCascadeMessageOptions{}
	if tools, ok := req["tools"].([]any); ok && len(tools) > 0 {
		options.NativeMode = true
		for _, t := range tools {
			if tm, ok := t.(map[string]any); ok {
				if fn, ok := tm["function"].(map[string]any); ok {
					if name, ok := fn["name"].(string); ok {
						options.NativeAllowlist = append(options.NativeAllowlist, name)
					}
				}
			}
		}
	}
	return options
}

func buildSSEChunk(id, model, content, finishReason string) string {
	delta := map[string]any{}
	if content != "" {
		delta["content"] = content
	}
	choice := map[string]any{
		"index": 0,
		"delta": delta,
	}
	if finishReason != "" {
		choice["finish_reason"] = finishReason
		choice["delta"] = map[string]any{}
	}
	chunk := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{choice},
	}
	b, _ := json.Marshal(chunk)
	return string(b)
}

func buildSSEToolCallChunk(id, model string, tc *windsurf.WindsurfToolCall) string {
	choice := map[string]any{
		"index": 0,
		"delta": map[string]any{
			"tool_calls": []any{
				map[string]any{
					"index": 0,
					"id":    tc.ID,
					"type":  "function",
					"function": map[string]any{
						"name":      tc.Name,
						"arguments": tc.ArgumentsJSON,
					},
				},
			},
		},
	}
	chunk := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{choice},
	}
	b, _ := json.Marshal(chunk)
	return string(b)
}
