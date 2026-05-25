package windsurf

import (
	"fmt"
	"runtime"
	"strings"
)

const (
	DefaultClientVersion = "2.0.67"
	DefaultIDEVersion    = "1.9600.41"
	DefaultCSRFToken     = "windsurf-api-csrf-fixed-token"
	DefaultCodeiumAPIURL = "https://server.self-serve.windsurf.com"
	DefaultRegisterURL   = "https://api.codeium.com/register_user/"
	LSServicePath        = "/exa.language_server_pb.LanguageServerService"
)

// CascadeStep 表示一个 Cascade 轨迹步骤
type CascadeStep struct {
	StepType     uint64
	Status       uint64
	Text         string
	ResponseText string
	ModifiedText string
	Thinking     string
	ErrorText    string
	NativeTool   *CascadeNativeToolStep
	Usage        *CascadeUsage
}

// CascadeNativeToolStep 表示原生工具调用步骤
type CascadeNativeToolStep struct {
	Kind      string
	Arguments map[string]any
}

// CascadeUsage 表示 token 用量
type CascadeUsage struct {
	InputTokens      uint64
	OutputTokens     uint64
	CacheWriteTokens uint64
	CacheReadTokens  uint64
	EntryCount       uint64
}

// CascadeImage 表示附带的图片
type CascadeImage struct {
	Base64Data string
	MimeType   string
}

// SendCascadeMessageOptions 发送消息的选项
type SendCascadeMessageOptions struct {
	ToolPreamble    string
	Images          []CascadeImage
	AdditionalSteps [][]byte
	NativeMode      bool
	NativeAllowlist []string
}

func currentOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}

func currentArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "x86_64"
}

func metadataRequestID(seed string) uint64 {
	hash := uint64(0xcbf29ce484222325)
	for i := 0; i < len(seed); i++ {
		hash ^= uint64(seed[i])
		hash *= 0x100000001b3
	}
	return hash & 0x0000_ffff_ffff_ffff
}

// BuildMetadata 构建客户端元数据
func BuildMetadata(apiKey, sessionID string) []byte {
	var out []byte
	out = append(out, WriteStringField(1, "windsurf")...)
	out = append(out, WriteStringField(2, DefaultClientVersion)...)
	out = append(out, WriteStringField(3, apiKey)...)
	out = append(out, WriteStringField(4, "en")...)
	out = append(out, WriteStringField(5, currentOS())...)
	out = append(out, WriteStringField(7, DefaultClientVersion)...)
	out = append(out, WriteStringField(8, currentArch())...)
	out = append(out, WriteVarintField(9, metadataRequestID(sessionID))...)
	out = append(out, WriteStringField(10, sessionID)...)
	out = append(out, WriteStringField(12, "windsurf")...)
	return out
}

// BuildInitializePanelStateRequest 构建初始化面板状态请求
func BuildInitializePanelStateRequest(apiKey, sessionID string, trusted bool) []byte {
	var out []byte
	out = append(out, WriteMessageField(1, BuildMetadata(apiKey, sessionID))...)
	out = append(out, WriteBoolField(3, trusted)...)
	return out
}

// BuildHeartbeatRequest 构建心跳请求
func BuildHeartbeatRequest(apiKey, sessionID string) []byte {
	return WriteMessageField(1, BuildMetadata(apiKey, sessionID))
}

// BuildGetUserStatusRequest 构建获取用户状态请求
func BuildGetUserStatusRequest(apiKey, sessionID string) []byte {
	return WriteMessageField(1, BuildMetadata(apiKey, sessionID))
}

// BuildUpdatePanelStateWithUserStatusRequest 构建更新面板状态请求
func BuildUpdatePanelStateWithUserStatusRequest(apiKey, sessionID string, userStatusBytes []byte) []byte {
	var out []byte
	out = append(out, WriteMessageField(1, BuildMetadata(apiKey, sessionID))...)
	if len(userStatusBytes) > 0 {
		out = append(out, WriteMessageField(2, userStatusBytes)...)
	}
	return out
}

// BuildAddTrackedWorkspaceRequest 构建添加工作区请求
func BuildAddTrackedWorkspaceRequest(workspacePath string) []byte {
	return WriteStringField(1, workspacePath)
}

// BuildUpdateWorkspaceTrustRequest 构建更新工作区信任请求
func BuildUpdateWorkspaceTrustRequest(apiKey, sessionID string, trusted bool) []byte {
	var out []byte
	out = append(out, WriteMessageField(1, BuildMetadata(apiKey, sessionID))...)
	out = append(out, WriteBoolField(2, trusted)...)
	return out
}

// BuildStartCascadeRequest 构建启动 Cascade 请求
func BuildStartCascadeRequest(apiKey, sessionID string) []byte {
	var out []byte
	out = append(out, WriteMessageField(1, BuildMetadata(apiKey, sessionID))...)
	out = append(out, WriteVarintField(4, 1)...)
	out = append(out, WriteVarintField(5, 1)...)
	return out
}

// BuildGetTrajectoryStepsRequest 构建获取轨迹步骤请求
func BuildGetTrajectoryStepsRequest(cascadeID string, offset uint64) []byte {
	out := WriteStringField(1, cascadeID)
	if offset > 0 {
		out = append(out, WriteVarintField(2, offset)...)
	}
	return out
}

// BuildGetTrajectoryRequest 构建获取轨迹状态请求
func BuildGetTrajectoryRequest(cascadeID string) []byte {
	return WriteStringField(1, cascadeID)
}

// BuildGetGeneratorMetadataRequest 构建获取生成器元数据请求
func BuildGetGeneratorMetadataRequest(cascadeID string, offset uint64) []byte {
	out := WriteStringField(1, cascadeID)
	if offset > 0 {
		out = append(out, WriteVarintField(2, offset)...)
	}
	return out
}

// BuildSendCascadeMessageRequest 构建发送 Cascade 消息请求
func BuildSendCascadeMessageRequest(apiKey, cascadeID, text string, modelEnum uint32, modelUID, sessionID string, options *SendCascadeMessageOptions) ([]byte, error) {
	if modelEnum == 0 && modelUID == "" {
		return nil, fmt.Errorf("windsurf cascade model enum or model uid is required")
	}
	if options == nil {
		options = &SendCascadeMessageOptions{}
	}

	item := WriteStringField(1, text)
	config, err := buildCascadeConfig(modelEnum, modelUID, options)
	if err != nil {
		return nil, err
	}

	var out []byte
	out = append(out, WriteStringField(1, cascadeID)...)
	out = append(out, WriteMessageField(2, item)...)
	out = append(out, WriteMessageField(3, BuildMetadata(apiKey, sessionID))...)
	out = append(out, WriteMessageField(5, config)...)

	for _, img := range options.Images {
		if img.Base64Data == "" {
			continue
		}
		mime := img.MimeType
		if mime == "" {
			mime = "image/png"
		}
		var imgMsg []byte
		imgMsg = append(imgMsg, WriteStringField(1, img.Base64Data)...)
		imgMsg = append(imgMsg, WriteStringField(2, mime)...)
		out = append(out, WriteMessageField(6, imgMsg)...)
	}

	for _, step := range options.AdditionalSteps {
		if len(step) > 0 {
			out = append(out, WriteMessageField(9, step)...)
		}
	}
	return out, nil
}

func buildCascadeConfig(modelEnum uint32, modelUID string, options *SendCascadeMessageOptions) ([]byte, error) {
	if modelEnum == 0 && modelUID == "" {
		return nil, fmt.Errorf("windsurf cascade config requires a model identifier")
	}

	hasPreamble := options.ToolPreamble != ""
	forceDefault := options.NativeMode || (len(options.Images) > 0 && !hasPreamble)
	plannerMode := uint64(3)
	if forceDefault {
		plannerMode = 1
	}

	var conversationalConfig []byte
	conversationalConfig = append(conversationalConfig, WriteVarintField(4, plannerMode)...)

	if hasPreamble {
		fullSection := options.ToolPreamble + "\n\n" + toolReinforcement
		var additional []byte
		additional = append(additional, WriteVarintField(1, 1)...)
		additional = append(additional, WriteStringField(2, fullSection)...)
		conversationalConfig = append(conversationalConfig, WriteMessageField(12, additional)...)

		var comm []byte
		comm = append(comm, WriteVarintField(1, 1)...)
		comm = append(comm, WriteStringField(2, communicationWithTools)...)
		conversationalConfig = append(conversationalConfig, WriteMessageField(13, comm)...)
	} else {
		var noTool []byte
		noTool = append(noTool, WriteVarintField(1, 1)...)
		noTool = append(noTool, WriteStringField(2, "No tools are available.")...)
		conversationalConfig = append(conversationalConfig, WriteMessageField(10, noTool)...)

		var additional []byte
		additional = append(additional, WriteVarintField(1, 1)...)
		additional = append(additional, WriteStringField(2, noToolAdditionalPrompt)...)
		conversationalConfig = append(conversationalConfig, WriteMessageField(12, additional)...)

		var comm []byte
		comm = append(comm, WriteVarintField(1, 1)...)
		comm = append(comm, WriteStringField(2, communicationNoTools)...)
		conversationalConfig = append(conversationalConfig, WriteMessageField(13, comm)...)
	}

	var plannerConfig []byte
	plannerConfig = append(plannerConfig, WriteMessageField(2, conversationalConfig)...)
	if modelUID != "" {
		plannerConfig = append(plannerConfig, WriteStringField(35, modelUID)...)
		plannerConfig = append(plannerConfig, WriteStringField(34, modelUID)...)
	}
	if modelEnum > 0 {
		plannerConfig = append(plannerConfig, WriteMessageField(15, WriteVarintField(1, uint64(modelEnum)))...)
		plannerConfig = append(plannerConfig, WriteVarintField(1, uint64(modelEnum))...)
	}
	plannerConfig = append(plannerConfig, WriteVarintField(6, 32768)...)
	if !hasPreamble {
		var empty []byte
		empty = append(empty, WriteVarintField(1, 1)...)
		empty = append(empty, WriteStringField(2, "")...)
		plannerConfig = append(plannerConfig, WriteMessageField(11, empty)...)
	}
	if options.NativeMode {
		plannerConfig = append(plannerConfig, WriteMessageField(13, BuildNativeCascadeToolConfig(options.NativeAllowlist))...)
	}

	memoryConfig := WriteVarintField(1, 0)
	var brainConfig []byte
	brainConfig = append(brainConfig, WriteVarintField(1, 1)...)
	brainConfig = append(brainConfig, WriteMessageField(6, WriteLenFieldAllowEmpty(6, nil))...)

	var out []byte
	out = append(out, WriteMessageField(1, plannerConfig)...)
	out = append(out, WriteMessageField(5, memoryConfig)...)
	out = append(out, WriteMessageField(7, brainConfig)...)
	return out, nil
}

// ParseStartCascadeResponse 解析 StartCascade 响应，返回 cascade_id
func ParseStartCascadeResponse(buf []byte) (string, error) {
	fields, err := ParseFields(buf)
	if err != nil {
		return "", err
	}
	s, ok := GetString(fields, 1)
	if !ok || s == "" {
		return "", fmt.Errorf("StartCascade response missing cascade_id")
	}
	return s, nil
}

// ExtractUserStatusBytes 从 GetUserStatus 响应中提取 user_status 字节
func ExtractUserStatusBytes(buf []byte) []byte {
	fields, err := ParseFields(buf)
	if err != nil {
		return nil
	}
	b, ok := GetBytes(fields, 1)
	if !ok || len(b) == 0 {
		return nil
	}
	return b
}

// ParseTrajectoryStatus 解析轨迹状态（1=idle/done）
func ParseTrajectoryStatus(buf []byte) uint64 {
	fields, err := ParseFields(buf)
	if err != nil {
		return 0
	}
	v, ok := GetVarint(fields, 2)
	if !ok {
		return 0
	}
	return v
}

// ParseTrajectorySteps 解析轨迹步骤
func ParseTrajectorySteps(buf []byte) ([]CascadeStep, error) {
	fields, err := ParseFields(buf)
	if err != nil {
		return nil, err
	}
	var steps []CascadeStep
	for _, stepField := range GetAllFields(fields, 1) {
		if stepField.WireType != WireTypeLen {
			continue
		}
		stepFields, err := ParseFields(stepField.Bytes)
		if err != nil {
			return nil, err
		}
		stepType, _ := GetVarint(stepFields, 1)
		status, _ := GetVarint(stepFields, 4)
		step := CascadeStep{
			StepType: stepType,
			Status:   status,
		}

		// 解析 planner 字段 (field 20)
		if plannerField := GetField(stepFields, 20); plannerField != nil && plannerField.WireType == WireTypeLen {
			plannerFields, err := ParseFields(plannerField.Bytes)
			if err == nil {
				step.ResponseText, _ = GetString(plannerFields, 1)
				step.Thinking, _ = GetString(plannerFields, 3)
				step.ModifiedText, _ = GetString(plannerFields, 8)
				if step.ModifiedText != "" {
					step.Text = step.ModifiedText
				} else {
					step.Text = step.ResponseText
				}
			}
		}

		// 解析 usage (field 5 -> field 9)
		step.Usage = parseStepUsage(stepFields)

		// 解析原生工具
		step.NativeTool = parseNativeToolStep(stepFields, stepType)

		// 解析错误文本
		step.ErrorText = parseStepErrorText(stepFields)

		steps = append(steps, step)
	}
	return steps, nil
}

// ParseGeneratorMetadata 解析生成器元数据获取 token 用量
func ParseGeneratorMetadata(buf []byte) (*CascadeUsage, error) {
	fields, err := ParseFields(buf)
	if err != nil {
		return nil, err
	}
	entries := GetAllFields(fields, 1)
	if len(entries) == 0 {
		return nil, nil
	}

	usage := &CascadeUsage{EntryCount: uint64(len(entries))}
	for _, entry := range entries {
		if entry.WireType != WireTypeLen {
			continue
		}
		genFields, err := ParseFields(entry.Bytes)
		if err != nil {
			continue
		}
		chatModel := GetField(genFields, 1)
		if chatModel == nil || chatModel.WireType != WireTypeLen {
			continue
		}
		chatModelFields, err := ParseFields(chatModel.Bytes)
		if err != nil {
			continue
		}
		usageField := GetField(chatModelFields, 4)
		if usageField == nil || usageField.WireType != WireTypeLen {
			continue
		}
		usageFields, err := ParseFields(usageField.Bytes)
		if err != nil {
			continue
		}
		if v, ok := GetVarint(usageFields, 2); ok {
			usage.InputTokens += v
		}
		if v, ok := GetVarint(usageFields, 3); ok {
			usage.OutputTokens += v
		}
		if v, ok := GetVarint(usageFields, 4); ok {
			usage.CacheWriteTokens += v
		}
		if v, ok := GetVarint(usageFields, 5); ok {
			usage.CacheReadTokens += v
		}
	}
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.CacheWriteTokens == 0 && usage.CacheReadTokens == 0 {
		return nil, nil
	}
	return usage, nil
}

// BuildNativeCascadeToolConfig 构建原生工具配置
func BuildNativeCascadeToolConfig(allowlist []string) []byte {
	defaultTools := []string{"view_file", "run_command", "grep_search_v2", "find", "list_dir"}
	list := allowlist
	if len(list) == 0 {
		list = defaultTools
	}
	contains := func(name string) bool {
		for _, v := range list {
			if strings.TrimSpace(v) == name {
				return true
			}
		}
		return false
	}

	var out []byte
	if contains("run_command") {
		out = append(out, WriteLenFieldAllowEmpty(8, nil)...)
	}
	if contains("view_file") {
		out = append(out, WriteLenFieldAllowEmpty(10, nil)...)
	}
	if contains("list_dir") || contains("list_directory") {
		out = append(out, WriteLenFieldAllowEmpty(19, nil)...)
	}
	if contains("grep_search_v2") || contains("grep_search") {
		out = append(out, WriteLenFieldAllowEmpty(33, nil)...)
	}
	if contains("find") {
		out = append(out, WriteLenFieldAllowEmpty(5, nil)...)
	}
	for _, name := range list {
		out = append(out, WriteStringField(32, strings.TrimSpace(name))...)
	}
	return out
}

func parseStepUsage(stepFields []Field) *CascadeUsage {
	metaField := GetField(stepFields, 5)
	if metaField == nil || metaField.WireType != WireTypeLen {
		return nil
	}
	metaFields, err := ParseFields(metaField.Bytes)
	if err != nil {
		return nil
	}
	usageField := GetField(metaFields, 9)
	if usageField == nil || usageField.WireType != WireTypeLen {
		return nil
	}
	usageFields, err := ParseFields(usageField.Bytes)
	if err != nil {
		return nil
	}
	usage := &CascadeUsage{EntryCount: 1}
	usage.InputTokens, _ = GetVarint(usageFields, 2)
	usage.OutputTokens, _ = GetVarint(usageFields, 3)
	usage.CacheWriteTokens, _ = GetVarint(usageFields, 4)
	usage.CacheReadTokens, _ = GetVarint(usageFields, 5)
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.CacheWriteTokens == 0 && usage.CacheReadTokens == 0 {
		return nil
	}
	return usage
}

func parseNativeToolStep(stepFields []Field, stepType uint64) *CascadeNativeToolStep {
	kind := nativeKindForStepType(stepType)
	if kind == "" {
		return nil
	}
	// 原生工具步骤的参数在对应的 oneof 字段中
	return &CascadeNativeToolStep{Kind: kind}
}

func nativeKindForStepType(stepType uint64) string {
	switch stepType {
	case 13:
		return "grep_search"
	case 14:
		return "view_file"
	case 15:
		return "list_directory"
	case 23:
		return "write_to_file"
	case 28:
		return "run_command"
	case 32:
		return "propose_code"
	case 34:
		return "find"
	case 40:
		return "read_url_content"
	case 42:
		return "search_web"
	case 105:
		return "grep_search_v2"
	default:
		return ""
	}
}

func parseStepErrorText(stepFields []Field) string {
	// step_type 17 的错误文本在 field 20 的子字段中
	if plannerField := GetField(stepFields, 20); plannerField != nil && plannerField.WireType == WireTypeLen {
		plannerFields, err := ParseFields(plannerField.Bytes)
		if err == nil {
			if errText, ok := GetString(plannerFields, 5); ok {
				return errText
			}
		}
	}
	return ""
}

const toolReinforcement = `The functions listed above are available and callable. When the user's request can be answered by calling a function, emit a <tool_call> block as described. Use this exact format: <tool_call>{"name":"...","arguments":{...}}</tool_call>`

const communicationWithTools = "You are accessed via API. When asked about your identity, describe your actual underlying model name and provider accurately. STRICTLY respond in the exact same language the user used in their latest message (Chinese -> Chinese, English -> English, Japanese -> Japanese; never switch mid-conversation). Use the functions above when relevant."

const communicationNoTools = "You are accessed via API. When asked about your identity, describe your actual underlying model name and provider accurately. Answer directly. STRICTLY respond in the exact same language the user used in their latest message (Chinese -> Chinese, English -> English, Japanese -> Japanese; never switch mid-conversation)."

const noToolAdditionalPrompt = `CRITICAL OPERATING CONSTRAINT - READ BEFORE ANY RESPONSE:
You are being accessed as a plain chat API. You have NO tools, NO file access, NO shell, NO code execution, NO repository awareness, NO ability to list or read anything on the user's machine or any sandbox. You cannot "check", "look at", "open", "view", "inspect", "run", "glob", "grep", "list", or "edit" anything.

OUTPUT RULES:
1. Never narrate tool-like actions ("Let me check X", "I'll look at Y", "Looking at the file...", "I see in main.py...", "Based on the codebase...").
2. Never reference file paths, directory structures, line numbers, or repository contents that were not explicitly pasted into the current conversation by the user.
3. If the user asks about their code or project but hasn't pasted the relevant file content, respond: "I don't see that file in our conversation - please paste it and I'll help." Do NOT invent file contents.
4. For general questions, answer directly from your training knowledge. No preambles.
5. Match the user's language (Chinese -> Chinese, English -> English; never switch mid-conversation).

Violating these rules will produce broken output for the end user. Stay in chat-API mode at all times.`
