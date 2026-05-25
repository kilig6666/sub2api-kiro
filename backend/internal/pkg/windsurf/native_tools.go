package windsurf

import "encoding/json"

// NativeToolStepType 映射
var nativeToolStepTypes = map[string]struct {
	TypeEnum   uint64
	OneofField uint32
}{
	"grep_search":      {13, 13},
	"view_file":        {14, 14},
	"list_directory":   {15, 15},
	"list_dir":         {15, 15},
	"write_to_file":    {23, 23},
	"run_command":      {28, 28},
	"propose_code":     {32, 32},
	"find":             {34, 34},
	"read_url_content": {40, 40},
	"search_web":       {42, 42},
	"grep_search_v2":   {105, 105},
}

// WindsurfToolCall 表示一个解析后的工具调用
type WindsurfToolCall struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ArgumentsJSON string `json:"arguments"`
}

// BuildAdditionalStep 将工具结果编码为 protobuf 步骤（用于下一轮对话的 additional_steps）
func BuildAdditionalStep(kind string, args map[string]any) []byte {
	meta, ok := nativeToolStepTypes[kind]
	if !ok {
		return nil
	}
	body := buildNativeStepBody(kind, args)
	if body == nil {
		return nil
	}
	var out []byte
	out = append(out, WriteVarintField(1, meta.TypeEnum)...)
	out = append(out, WriteVarintField(4, 3)...) // status = completed
	out = append(out, WriteLenFieldAllowEmpty(meta.OneofField, body)...)
	return out
}

// NativeToolCallFromStep 从 CascadeStep 提取 OpenAI 格式的工具调用
func NativeToolCallFromStep(step *CascadeStep) *WindsurfToolCall {
	if step.NativeTool == nil {
		return nil
	}
	argsJSON := "{}"
	if step.NativeTool.Arguments != nil {
		if b, err := json.Marshal(step.NativeTool.Arguments); err == nil {
			argsJSON = string(b)
		}
	}
	return &WindsurfToolCall{
		ID:            generateToolCallID(),
		Name:          step.NativeTool.Kind,
		ArgumentsJSON: argsJSON,
	}
}

func generateToolCallID() string {
	return "call_ws_" + randomHex(12)
}

func randomHex(n int) string {
	const hex = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = hex[i%16]
	}
	return string(b)
}

func buildNativeStepBody(kind string, args map[string]any) []byte {
	switch kind {
	case "view_file":
		return buildViewFileBody(args)
	case "run_command":
		return buildRunCommandBody(args)
	case "grep_search", "grep_search_v2":
		return buildGrepSearchBody(args)
	case "find":
		return buildFindBody(args)
	case "list_directory", "list_dir":
		return buildListDirBody(args)
	case "write_to_file":
		return buildWriteToFileBody(args)
	default:
		return nil
	}
}

func buildViewFileBody(args map[string]any) []byte {
	var out []byte
	if v := argStr(args, "absolute_path_uri"); v != "" {
		out = append(out, WriteStringField(1, v)...)
	}
	if v := argUint(args, "offset"); v > 0 {
		out = append(out, WriteVarintField(11, v)...)
	}
	if v := argUint(args, "limit"); v > 0 {
		out = append(out, WriteVarintField(12, v)...)
	}
	if v := argStr(args, "content"); v != "" {
		out = append(out, WriteStringField(4, v)...)
	}
	return out
}

func buildRunCommandBody(args map[string]any) []byte {
	var out []byte
	if v := argStr(args, "command_line"); v != "" {
		out = append(out, WriteStringField(23, v)...)
	}
	if v := argStr(args, "cwd"); v != "" {
		out = append(out, WriteStringField(2, v)...)
	}
	if argBool(args, "blocking") {
		out = append(out, WriteBoolField(11, true)...)
	}
	if v := argStr(args, "stdout"); v != "" {
		out = append(out, WriteStringField(4, v)...)
	}
	if v := argStr(args, "stderr"); v != "" {
		out = append(out, WriteStringField(5, v)...)
	}
	if v := argUint(args, "exit_code"); v > 0 {
		out = append(out, WriteVarintField(6, v)...)
	}
	if v := argStr(args, "full_output"); v != "" {
		inner := WriteStringField(1, v)
		out = append(out, WriteMessageField(21, inner)...)
	}
	return out
}

func buildGrepSearchBody(args map[string]any) []byte {
	var out []byte
	if v := argStr(args, "pattern"); v != "" {
		out = append(out, WriteStringField(2, v)...)
	}
	if v := argStr(args, "path"); v != "" {
		out = append(out, WriteStringField(3, v)...)
	}
	if v := argStr(args, "glob"); v != "" {
		out = append(out, WriteStringField(4, v)...)
	}
	if argBool(args, "case_insensitive") {
		out = append(out, WriteBoolField(10, true)...)
	}
	return out
}

func buildFindBody(args map[string]any) []byte {
	var out []byte
	if v := argStr(args, "pattern"); v != "" {
		out = append(out, WriteStringField(1, v)...)
	}
	if v := argStr(args, "search_directory"); v != "" {
		out = append(out, WriteStringField(10, v)...)
	}
	if v := argStr(args, "raw_output"); v != "" {
		out = append(out, WriteStringField(11, v)...)
	}
	return out
}

func buildListDirBody(args map[string]any) []byte {
	var out []byte
	if v := argStr(args, "directory_path_uri"); v != "" {
		out = append(out, WriteStringField(1, v)...)
	}
	return out
}

func buildWriteToFileBody(args map[string]any) []byte {
	var out []byte
	if v := argStr(args, "target_file_uri"); v != "" {
		out = append(out, WriteStringField(1, v)...)
	}
	if argBool(args, "file_created") {
		out = append(out, WriteBoolField(4, true)...)
	}
	return out
}

func argStr(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func argUint(args map[string]any, key string) uint64 {
	if args == nil {
		return 0
	}
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return uint64(n)
	case int:
		return uint64(n)
	case int64:
		return uint64(n)
	case uint64:
		return n
	default:
		return 0
	}
}

func argBool(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	v, ok := args[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}
