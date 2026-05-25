package windsurf

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	PollInterval       = 500 * time.Millisecond
	CascadeMaxWait     = 180 * time.Second
	CascadeIdleGrace   = 8 * time.Second
	CascadeTextStall   = 45 * time.Second
	CascadeThinkStall  = 120 * time.Second
	SSEHeartbeat       = 15 * time.Second
	GRPCShortTimeout   = 5 * time.Second
	GRPCStatusTimeout  = 10 * time.Second
	GRPCRequestTimeout = 45 * time.Second
	SendMaxRetries     = 3
	WarmupMaxRestarts  = 2
)

// PollEventType 轮询事件类型
type PollEventType int

const (
	PollEventTextDelta PollEventType = iota
	PollEventNativeToolCall
	PollEventHeartbeat
)

// PollEvent 轮询事件
type PollEvent struct {
	Type      PollEventType
	TextDelta string
	ToolCall  *WindsurfToolCall
}

// CascadeResult Cascade 执行结果
type CascadeResult struct {
	Content      string
	ToolCalls    []WindsurfToolCall
	Usage        *CascadeUsage
	Model        string
	FinishReason string
}

// CascadeExecutor 封装完整的 Cascade 执行流程
type CascadeExecutor struct {
	Pool       *LSPool
	BinaryPath string
}

// Execute 执行完整的 Cascade 流程（同步模式）
func (e *CascadeExecutor) Execute(ctx context.Context, apiKey, model, message, workspacePath string, options *SendCascadeMessageOptions) (*CascadeResult, error) {
	resolved, ok := ResolveWindsurfModel(model)
	if !ok {
		return nil, fmt.Errorf("unsupported Windsurf model: %s", model)
	}

	poolKey := PoolKeyFromAPIKey(apiKey)
	ls, err := e.Pool.Ensure(ctx, poolKey, e.BinaryPath, workspacePath)
	if err != nil {
		return nil, err
	}

	if err := e.warmup(ctx, ls, apiKey); err != nil {
		// 重试一次
		e.Pool.Invalidate(ls)
		ls, err = e.Pool.Ensure(ctx, poolKey, e.BinaryPath, workspacePath)
		if err != nil {
			return nil, err
		}
		if err := e.warmup(ctx, ls, apiKey); err != nil {
			return nil, err
		}
	}

	cascadeID, err := e.startCascade(ctx, ls, apiKey)
	if err != nil {
		return nil, err
	}

	if err := e.sendMessage(ctx, ls, apiKey, cascadeID, message, resolved, options); err != nil {
		return nil, err
	}

	return e.poll(ctx, ls, cascadeID, resolved.CanonicalName)
}

// ExecuteStream 执行 Cascade 流程（流式模式，通过 channel 发送事件）
func (e *CascadeExecutor) ExecuteStream(ctx context.Context, apiKey, model, message, workspacePath string, options *SendCascadeMessageOptions, events chan<- PollEvent) (*CascadeResult, error) {
	resolved, ok := ResolveWindsurfModel(model)
	if !ok {
		return nil, fmt.Errorf("unsupported Windsurf model: %s", model)
	}

	poolKey := PoolKeyFromAPIKey(apiKey)
	ls, err := e.Pool.Ensure(ctx, poolKey, e.BinaryPath, workspacePath)
	if err != nil {
		return nil, err
	}

	if err := e.warmup(ctx, ls, apiKey); err != nil {
		e.Pool.Invalidate(ls)
		ls, err = e.Pool.Ensure(ctx, poolKey, e.BinaryPath, workspacePath)
		if err != nil {
			return nil, err
		}
		if err := e.warmup(ctx, ls, apiKey); err != nil {
			return nil, err
		}
	}

	cascadeID, err := e.startCascade(ctx, ls, apiKey)
	if err != nil {
		return nil, err
	}

	if err := e.sendMessage(ctx, ls, apiKey, cascadeID, message, resolved, options); err != nil {
		return nil, err
	}

	return e.pollStream(ctx, ls, cascadeID, resolved.CanonicalName, events)
}

func (e *CascadeExecutor) warmup(ctx context.Context, ls *LSHandle, apiKey string) error {
	// InitializeCascadePanelState
	_, err := GRPCUnary(ctx, ls.Port, ls.CSRFToken, "InitializeCascadePanelState",
		BuildInitializePanelStateRequest(apiKey, ls.SessionID, true), GRPCShortTimeout)
	if err != nil {
		return fmt.Errorf("InitializeCascadePanelState: %w", err)
	}

	// GetUserStatus + UpdatePanelState
	statusResp, err := GRPCUnary(ctx, ls.Port, ls.CSRFToken, "GetUserStatus",
		BuildGetUserStatusRequest(apiKey, ls.SessionID), GRPCStatusTimeout)
	if err == nil {
		if userStatus := ExtractUserStatusBytes(statusResp); len(userStatus) > 0 {
			_, _ = GRPCUnary(ctx, ls.Port, ls.CSRFToken, "UpdatePanelStateWithUserStatus",
				BuildUpdatePanelStateWithUserStatusRequest(apiKey, ls.SessionID, userStatus), GRPCShortTimeout)
		}
	}

	// AddTrackedWorkspace
	if ls.WorkspacePath != "" {
		_, _ = GRPCUnary(ctx, ls.Port, ls.CSRFToken, "AddTrackedWorkspace",
			BuildAddTrackedWorkspaceRequest(ls.WorkspacePath), GRPCShortTimeout)
	}

	// UpdateWorkspaceTrust
	_, _ = GRPCUnary(ctx, ls.Port, ls.CSRFToken, "UpdateWorkspaceTrust",
		BuildUpdateWorkspaceTrustRequest(apiKey, ls.SessionID, true), GRPCShortTimeout)

	// Heartbeat
	_, _ = GRPCUnary(ctx, ls.Port, ls.CSRFToken, "Heartbeat",
		BuildHeartbeatRequest(apiKey, ls.SessionID), GRPCShortTimeout)

	return nil
}

func (e *CascadeExecutor) startCascade(ctx context.Context, ls *LSHandle, apiKey string) (string, error) {
	resp, err := GRPCUnary(ctx, ls.Port, ls.CSRFToken, "StartCascade",
		BuildStartCascadeRequest(apiKey, ls.SessionID), GRPCRequestTimeout)
	if err != nil {
		return "", fmt.Errorf("StartCascade: %w", err)
	}
	return ParseStartCascadeResponse(resp)
}

func (e *CascadeExecutor) sendMessage(ctx context.Context, ls *LSHandle, apiKey, cascadeID, text string, model *WindsurfModel, options *SendCascadeMessageOptions) error {
	if options == nil {
		options = &SendCascadeMessageOptions{}
	}
	payload, err := BuildSendCascadeMessageRequest(apiKey, cascadeID, text, model.EnumValue, model.ModelUID, ls.SessionID, options)
	if err != nil {
		return fmt.Errorf("build SendCascadeMessage: %w", err)
	}

	for retry := 0; retry <= SendMaxRetries; retry++ {
		_, err = GRPCUnary(ctx, ls.Port, ls.CSRFToken, "SendUserCascadeMessage", payload, GRPCRequestTimeout)
		if err == nil {
			return nil
		}
		if retry < SendMaxRetries && isRetryableError(err) {
			time.Sleep(time.Duration(250*(retry+1)) * time.Millisecond)
			continue
		}
	}
	return fmt.Errorf("SendUserCascadeMessage: %w", err)
}

func (e *CascadeExecutor) poll(ctx context.Context, ls *LSHandle, cascadeID, model string) (*CascadeResult, error) {
	var deltas []string
	var toolCalls []WindsurfToolCall

	result, err := e.doPoll(ctx, ls, cascadeID, func(event PollEvent) {
		switch event.Type {
		case PollEventTextDelta:
			deltas = append(deltas, event.TextDelta)
		case PollEventNativeToolCall:
			if event.ToolCall != nil {
				toolCalls = append(toolCalls, *event.ToolCall)
			}
		}
	})
	if err != nil {
		return nil, err
	}

	content := strings.Join(deltas, "")
	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	return &CascadeResult{
		Content:      content,
		ToolCalls:    toolCalls,
		Usage:        result,
		Model:        model,
		FinishReason: finishReason,
	}, nil
}

func (e *CascadeExecutor) pollStream(ctx context.Context, ls *LSHandle, cascadeID, model string, events chan<- PollEvent) (*CascadeResult, error) {
	var deltas []string
	var toolCalls []WindsurfToolCall

	result, err := e.doPoll(ctx, ls, cascadeID, func(event PollEvent) {
		switch event.Type {
		case PollEventTextDelta:
			deltas = append(deltas, event.TextDelta)
		case PollEventNativeToolCall:
			if event.ToolCall != nil {
				toolCalls = append(toolCalls, *event.ToolCall)
			}
		}
		// 发送事件到 channel
		select {
		case events <- event:
		default:
		}
	})
	if err != nil {
		return nil, err
	}

	content := strings.Join(deltas, "")
	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	return &CascadeResult{
		Content:      content,
		ToolCalls:    toolCalls,
		Usage:        result,
		Model:        model,
		FinishReason: finishReason,
	}, nil
}

func (e *CascadeExecutor) doPoll(ctx context.Context, ls *LSHandle, cascadeID string, onEvent func(PollEvent)) (*CascadeUsage, error) {
	startedAt := time.Now()
	yieldedByStep := make(map[int]int)
	var sawText, sawThinking, sawActive bool
	var lastGrowthAt = time.Now()
	var lastHeartbeatAt = time.Now()
	var lastStepCount int
	var idleCount int
	var nativeToolsSeen = make(map[int]bool)
	var nativeToolCalls []WindsurfToolCall
	var usageByStep = make(map[int]*CascadeUsage)

	for time.Since(startedAt) < CascadeMaxWait {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		time.Sleep(PollInterval)

		// 心跳
		if time.Since(lastHeartbeatAt) >= SSEHeartbeat {
			onEvent(PollEvent{Type: PollEventHeartbeat})
			lastHeartbeatAt = time.Now()
		}

		// 获取轨迹步骤
		stepsResp, err := GRPCUnary(ctx, ls.Port, ls.CSRFToken, "GetCascadeTrajectorySteps",
			BuildGetTrajectoryStepsRequest(cascadeID, 0), GRPCRequestTimeout)
		if err != nil {
			return nil, fmt.Errorf("GetCascadeTrajectorySteps: %w", err)
		}
		steps, err := ParseTrajectorySteps(stepsResp)
		if err != nil {
			return nil, fmt.Errorf("parse trajectory steps: %w", err)
		}

		// 检查错误步骤
		for _, step := range steps {
			if step.StepType == 17 && strings.TrimSpace(step.ErrorText) != "" {
				return nil, fmt.Errorf("Windsurf error: %s", strings.TrimSpace(step.ErrorText))
			}
		}

		// 收集 usage
		for i, step := range steps {
			if step.Usage != nil {
				if prev, ok := usageByStep[i]; !ok || *prev != *step.Usage {
					usageByStep[i] = step.Usage
					lastGrowthAt = time.Now()
				}
			}
		}

		// 收集原生工具调用
		for i, step := range steps {
			if step.NativeTool != nil && !nativeToolsSeen[i] {
				nativeToolsSeen[i] = true
				lastGrowthAt = time.Now()
				tc := NativeToolCallFromStep(&step)
				if tc != nil {
					nativeToolCalls = append(nativeToolCalls, *tc)
					onEvent(PollEvent{Type: PollEventNativeToolCall, ToolCall: tc})
				}
			}
		}

		// 步骤数增长
		if len(steps) > lastStepCount {
			lastStepCount = len(steps)
			lastGrowthAt = time.Now()
		}

		// 检查 thinking 增长
		for _, step := range steps {
			if step.Thinking != "" {
				sawThinking = true
				lastGrowthAt = time.Now()
			}
		}

		// 发射文本增量
		for i, step := range steps {
			liveText := step.ResponseText
			if liveText == "" {
				liveText = step.Text
			}
			prev := yieldedByStep[i]
			if len(liveText) > prev {
				delta := liveText[prev:]
				yieldedByStep[i] = len(liveText)
				sawText = true
				lastGrowthAt = time.Now()
				onEvent(PollEvent{Type: PollEventTextDelta, TextDelta: delta})
			}
		}

		// 获取轨迹状态
		statusResp, err := GRPCUnary(ctx, ls.Port, ls.CSRFToken, "GetCascadeTrajectory",
			BuildGetTrajectoryRequest(cascadeID), GRPCShortTimeout)
		if err != nil {
			return nil, fmt.Errorf("GetCascadeTrajectory: %w", err)
		}
		status := ParseTrajectoryStatus(statusResp)

		if status == 1 { // idle/done
			if !sawActive && time.Since(startedAt) < CascadeIdleGrace {
				continue
			}
			idleCount++
			growthSettled := time.Since(lastGrowthAt) > PollInterval*2
			sawOutput := sawText || len(nativeToolCalls) > 0
			if (sawOutput && idleCount >= 2 && growthSettled) || idleCount >= 4 {
				break
			}
		} else {
			sawActive = true
			idleCount = 0
		}

		// 停滞超时检测
		stallTimeout := e.stallTimeout(len(nativeToolCalls) > 0, sawThinking, sawText)
		if time.Since(lastGrowthAt) >= stallTimeout && (sawText || len(nativeToolCalls) > 0) {
			break
		}
	}

	if time.Since(startedAt) >= CascadeMaxWait {
		return nil, fmt.Errorf("Windsurf Cascade timed out")
	}

	// 获取精确 usage
	usage := e.fetchGeneratorUsage(ctx, ls, cascadeID)
	if usage == nil {
		usage = sumUsage(usageByStep)
	}
	return usage, nil
}

func (e *CascadeExecutor) fetchGeneratorUsage(ctx context.Context, ls *LSHandle, cascadeID string) *CascadeUsage {
	resp, err := GRPCUnary(ctx, ls.Port, ls.CSRFToken, "GetCascadeTrajectoryGeneratorMetadata",
		BuildGetGeneratorMetadataRequest(cascadeID, 0), GRPCShortTimeout)
	if err != nil {
		return nil
	}
	usage, _ := ParseGeneratorMetadata(resp)
	return usage
}

func (e *CascadeExecutor) stallTimeout(sawNativeTool, sawThinking, sawText bool) time.Duration {
	if sawNativeTool {
		return CascadeMaxWait
	}
	if sawThinking {
		return CascadeThinkStall
	}
	if sawText {
		return CascadeTextStall
	}
	return CascadeMaxWait
}

func sumUsage(byStep map[int]*CascadeUsage) *CascadeUsage {
	if len(byStep) == 0 {
		return nil
	}
	total := &CascadeUsage{EntryCount: uint64(len(byStep))}
	for _, u := range byStep {
		total.InputTokens += u.InputTokens
		total.OutputTokens += u.OutputTokens
		total.CacheWriteTokens += u.CacheWriteTokens
		total.CacheReadTokens += u.CacheReadTokens
	}
	if total.InputTokens == 0 && total.OutputTokens == 0 {
		return nil
	}
	return total
}

func isRetryableError(err error) bool {
	msg := strings.ToLower(err.Error())
	retryable := []string{"panel state", "not_found", "expired", "untrusted", "connection refused", "connection reset"}
	for _, s := range retryable {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// ToOpenAIChatCompletion 将 CascadeResult 转换为 OpenAI chat completion JSON
func (r *CascadeResult) ToOpenAIChatCompletion(requestID string) []byte {
	var message map[string]any
	if len(r.ToolCalls) > 0 {
		var tcs []map[string]any
		for i, tc := range r.ToolCalls {
			tcs = append(tcs, map[string]any{
				"index": i,
				"id":    tc.ID,
				"type":  "function",
				"function": map[string]any{
					"name":      tc.Name,
					"arguments": tc.ArgumentsJSON,
				},
			})
		}
		message = map[string]any{
			"role":       "assistant",
			"content":    nil,
			"tool_calls": tcs,
		}
	} else {
		message = map[string]any{
			"role":    "assistant",
			"content": r.Content,
		}
	}

	resp := map[string]any{
		"id":      "chatcmpl-" + requestID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   r.Model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       message,
			"finish_reason": r.FinishReason,
		}},
	}
	if r.Usage != nil {
		promptTokens := r.Usage.InputTokens + r.Usage.CacheReadTokens
		usageMap := map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": r.Usage.OutputTokens,
			"total_tokens":      promptTokens + r.Usage.OutputTokens + r.Usage.CacheWriteTokens,
		}
		if r.Usage.CacheWriteTokens > 0 || r.Usage.CacheReadTokens > 0 {
			usageMap["cache_creation_input_tokens"] = r.Usage.CacheWriteTokens
			usageMap["cache_read_input_tokens"] = r.Usage.CacheReadTokens
			usageMap["prompt_tokens_details"] = map[string]any{
				"cached_tokens": r.Usage.CacheReadTokens,
			}
		}
		resp["usage"] = usageMap
	}
	b, _ := json.Marshal(resp)
	return b
}
