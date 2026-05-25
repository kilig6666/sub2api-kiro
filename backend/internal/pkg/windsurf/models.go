package windsurf

import "strings"

// WindsurfModel 表示 Windsurf 支持的一个模型
type WindsurfModel struct {
	CanonicalName    string
	EnumValue        uint32
	ModelUID         string
	CreditMultiplier float32
	Provider         string
	Deprecated       bool
}

var models = []WindsurfModel{
	// Anthropic
	{CanonicalName: "claude-3.5-sonnet", EnumValue: 166, CreditMultiplier: 2.0, Provider: "anthropic", Deprecated: true},
	{CanonicalName: "claude-3.7-sonnet", EnumValue: 226, CreditMultiplier: 2.0, Provider: "anthropic", Deprecated: true},
	{CanonicalName: "claude-3.7-sonnet-thinking", EnumValue: 227, CreditMultiplier: 3.0, Provider: "anthropic", Deprecated: true},
	{CanonicalName: "claude-4-sonnet", EnumValue: 281, ModelUID: "MODEL_CLAUDE_4_SONNET", CreditMultiplier: 2.0, Provider: "anthropic"},
	{CanonicalName: "claude-4-sonnet-thinking", EnumValue: 282, ModelUID: "MODEL_CLAUDE_4_SONNET_THINKING", CreditMultiplier: 3.0, Provider: "anthropic"},
	{CanonicalName: "claude-4-opus", EnumValue: 290, ModelUID: "MODEL_CLAUDE_4_OPUS", CreditMultiplier: 4.0, Provider: "anthropic"},
	{CanonicalName: "claude-4-opus-thinking", EnumValue: 291, ModelUID: "MODEL_CLAUDE_4_OPUS_THINKING", CreditMultiplier: 5.0, Provider: "anthropic"},
	{CanonicalName: "claude-4.1-opus", EnumValue: 328, ModelUID: "MODEL_CLAUDE_4_1_OPUS", CreditMultiplier: 4.0, Provider: "anthropic"},
	{CanonicalName: "claude-4.1-opus-thinking", EnumValue: 329, ModelUID: "MODEL_CLAUDE_4_1_OPUS_THINKING", CreditMultiplier: 5.0, Provider: "anthropic"},
	{CanonicalName: "claude-4.5-haiku", EnumValue: 0, ModelUID: "MODEL_PRIVATE_11", CreditMultiplier: 1.0, Provider: "anthropic"},
	{CanonicalName: "claude-4.5-sonnet", EnumValue: 353, ModelUID: "MODEL_PRIVATE_2", CreditMultiplier: 2.0, Provider: "anthropic"},
	{CanonicalName: "claude-4.5-sonnet-thinking", EnumValue: 354, ModelUID: "MODEL_PRIVATE_3", CreditMultiplier: 3.0, Provider: "anthropic"},
	{CanonicalName: "claude-4.5-opus", EnumValue: 391, ModelUID: "MODEL_CLAUDE_4_5_OPUS", CreditMultiplier: 4.0, Provider: "anthropic"},
	{CanonicalName: "claude-4.5-opus-thinking", EnumValue: 392, ModelUID: "MODEL_CLAUDE_4_5_OPUS_THINKING", CreditMultiplier: 5.0, Provider: "anthropic"},
	{CanonicalName: "claude-sonnet-4.6", EnumValue: 0, ModelUID: "claude-sonnet-4-6", CreditMultiplier: 4.0, Provider: "anthropic"},
	{CanonicalName: "claude-sonnet-4.6-thinking", EnumValue: 0, ModelUID: "claude-sonnet-4-6-thinking", CreditMultiplier: 6.0, Provider: "anthropic"},
	{CanonicalName: "claude-sonnet-4.6-1m", EnumValue: 0, ModelUID: "claude-sonnet-4-6-1m", CreditMultiplier: 12.0, Provider: "anthropic"},
	{CanonicalName: "claude-sonnet-4.6-thinking-1m", EnumValue: 0, ModelUID: "claude-sonnet-4-6-thinking-1m", CreditMultiplier: 16.0, Provider: "anthropic"},
	{CanonicalName: "claude-opus-4.6", EnumValue: 0, ModelUID: "claude-opus-4-6", CreditMultiplier: 6.0, Provider: "anthropic"},
	{CanonicalName: "claude-opus-4.6-thinking", EnumValue: 0, ModelUID: "claude-opus-4-6-thinking", CreditMultiplier: 8.0, Provider: "anthropic"},
	{CanonicalName: "claude-opus-4-7-medium", EnumValue: 0, ModelUID: "claude-opus-4-7-medium", CreditMultiplier: 8.0, Provider: "anthropic"},
	{CanonicalName: "claude-opus-4-7-low", EnumValue: 0, ModelUID: "claude-opus-4-7-low", CreditMultiplier: 6.0, Provider: "anthropic"},
	{CanonicalName: "claude-opus-4-7-high", EnumValue: 0, ModelUID: "claude-opus-4-7-high", CreditMultiplier: 10.0, Provider: "anthropic"},
	{CanonicalName: "claude-opus-4-7-xhigh", EnumValue: 0, ModelUID: "claude-opus-4-7-xhigh", CreditMultiplier: 12.0, Provider: "anthropic"},
	{CanonicalName: "claude-opus-4-7-medium-thinking", EnumValue: 0, ModelUID: "claude-opus-4-7-medium-thinking", CreditMultiplier: 10.0, Provider: "anthropic"},
	{CanonicalName: "claude-opus-4-7-high-thinking", EnumValue: 0, ModelUID: "claude-opus-4-7-high-thinking", CreditMultiplier: 12.0, Provider: "anthropic"},
	{CanonicalName: "claude-opus-4-7-xhigh-thinking", EnumValue: 0, ModelUID: "claude-opus-4-7-xhigh-thinking", CreditMultiplier: 16.0, Provider: "anthropic"},
	{CanonicalName: "claude-opus-4-7-max", EnumValue: 0, ModelUID: "claude-opus-4-7-max", CreditMultiplier: 16.0, Provider: "anthropic"},
	// OpenAI
	{CanonicalName: "gpt-4o", EnumValue: 109, ModelUID: "MODEL_CHAT_GPT_4O_2024_08_06", CreditMultiplier: 1.0, Provider: "openai"},
	{CanonicalName: "gpt-4o-mini", EnumValue: 113, CreditMultiplier: 0.5, Provider: "openai", Deprecated: true},
	{CanonicalName: "gpt-4.1", EnumValue: 259, ModelUID: "MODEL_CHAT_GPT_4_1_2025_04_14", CreditMultiplier: 1.0, Provider: "openai"},
	{CanonicalName: "gpt-4.1-mini", EnumValue: 260, CreditMultiplier: 0.5, Provider: "openai", Deprecated: true},
	{CanonicalName: "gpt-4.1-nano", EnumValue: 261, CreditMultiplier: 0.25, Provider: "openai", Deprecated: true},
	{CanonicalName: "gpt-5", EnumValue: 340, ModelUID: "MODEL_PRIVATE_6", CreditMultiplier: 0.5, Provider: "openai"},
	{CanonicalName: "gpt-5-medium", EnumValue: 0, ModelUID: "MODEL_PRIVATE_7", CreditMultiplier: 1.0, Provider: "openai"},
	{CanonicalName: "gpt-5-high", EnumValue: 0, ModelUID: "MODEL_PRIVATE_8", CreditMultiplier: 2.0, Provider: "openai"},
	{CanonicalName: "gpt-5-mini", EnumValue: 337, CreditMultiplier: 0.25, Provider: "openai", Deprecated: true},
	{CanonicalName: "gpt-5-codex", EnumValue: 346, ModelUID: "MODEL_CHAT_GPT_5_CODEX", CreditMultiplier: 0.5, Provider: "openai"},
	{CanonicalName: "gpt-5.1", EnumValue: 0, ModelUID: "MODEL_PRIVATE_12", CreditMultiplier: 0.5, Provider: "openai"},
	{CanonicalName: "gpt-5.5-low", EnumValue: 0, ModelUID: "gpt-5-5-low", CreditMultiplier: 1.0, Provider: "openai"},
	{CanonicalName: "gpt-5.5-medium", EnumValue: 0, ModelUID: "gpt-5-5-medium", CreditMultiplier: 2.0, Provider: "openai"},
	{CanonicalName: "gpt-5.5-high", EnumValue: 0, ModelUID: "gpt-5-5-high", CreditMultiplier: 4.0, Provider: "openai"},
	{CanonicalName: "gpt-5.5-xhigh", EnumValue: 0, ModelUID: "gpt-5-5-xhigh", CreditMultiplier: 8.0, Provider: "openai"},
	{CanonicalName: "gpt-5.5-none", EnumValue: 0, ModelUID: "gpt-5-5-none", CreditMultiplier: 1.0, Provider: "openai"},
	{CanonicalName: "gpt-5.2", EnumValue: 401, ModelUID: "MODEL_GPT_5_2_MEDIUM", CreditMultiplier: 2.0, Provider: "openai"},
	{CanonicalName: "gpt-5.2-low", EnumValue: 400, ModelUID: "MODEL_GPT_5_2_LOW", CreditMultiplier: 1.0, Provider: "openai"},
	{CanonicalName: "gpt-5.2-high", EnumValue: 402, ModelUID: "MODEL_GPT_5_2_HIGH", CreditMultiplier: 3.0, Provider: "openai"},
	{CanonicalName: "gpt-5.2-xhigh", EnumValue: 403, ModelUID: "MODEL_GPT_5_2_XHIGH", CreditMultiplier: 8.0, Provider: "openai"},
	// OpenAI reasoning
	{CanonicalName: "o3-mini", EnumValue: 207, CreditMultiplier: 0.5, Provider: "openai"},
	{CanonicalName: "o3", EnumValue: 218, ModelUID: "MODEL_CHAT_O3", CreditMultiplier: 1.0, Provider: "openai"},
	{CanonicalName: "o3-high", EnumValue: 0, ModelUID: "MODEL_CHAT_O3_HIGH", CreditMultiplier: 1.0, Provider: "openai"},
	{CanonicalName: "o3-pro", EnumValue: 294, CreditMultiplier: 4.0, Provider: "openai"},
	{CanonicalName: "o4-mini", EnumValue: 264, CreditMultiplier: 0.5, Provider: "openai"},
	// Google
	{CanonicalName: "gemini-2.5-pro", EnumValue: 246, ModelUID: "MODEL_GOOGLE_GEMINI_2_5_PRO", CreditMultiplier: 1.0, Provider: "google"},
	{CanonicalName: "gemini-2.5-flash", EnumValue: 312, ModelUID: "MODEL_GOOGLE_GEMINI_2_5_FLASH", CreditMultiplier: 0.5, Provider: "google"},
	{CanonicalName: "gemini-3.0-pro", EnumValue: 412, ModelUID: "MODEL_GOOGLE_GEMINI_3_0_PRO_LOW", CreditMultiplier: 1.0, Provider: "google"},
	{CanonicalName: "gemini-3.0-flash", EnumValue: 415, ModelUID: "MODEL_GOOGLE_GEMINI_3_0_FLASH_MEDIUM", CreditMultiplier: 1.0, Provider: "google"},
	// xAI
	{CanonicalName: "grok-3", EnumValue: 217, ModelUID: "MODEL_XAI_GROK_3", CreditMultiplier: 1.0, Provider: "xai"},
	{CanonicalName: "grok-3-mini-thinking", EnumValue: 0, ModelUID: "MODEL_XAI_GROK_3_MINI_REASONING", CreditMultiplier: 0.125, Provider: "xai"},
	{CanonicalName: "grok-code-fast-1", EnumValue: 0, ModelUID: "MODEL_PRIVATE_4", CreditMultiplier: 0.5, Provider: "xai"},
	// Moonshot
	{CanonicalName: "kimi-k2", EnumValue: 323, ModelUID: "MODEL_KIMI_K2", CreditMultiplier: 0.5, Provider: "moonshot"},
	{CanonicalName: "kimi-k2-thinking", EnumValue: 394, ModelUID: "MODEL_KIMI_K2_THINKING", CreditMultiplier: 1.0, Provider: "moonshot"},
	{CanonicalName: "kimi-k2.5", EnumValue: 0, ModelUID: "kimi-k2-5", CreditMultiplier: 1.0, Provider: "moonshot"},
	// Zhipu
	{CanonicalName: "glm-4.7", EnumValue: 417, ModelUID: "MODEL_GLM_4_7", CreditMultiplier: 0.25, Provider: "zhipu"},
	{CanonicalName: "glm-4.7-fast", EnumValue: 418, ModelUID: "MODEL_GLM_4_7_FAST", CreditMultiplier: 0.5, Provider: "zhipu"},
	// MiniMax
	{CanonicalName: "minimax-m2.5", EnumValue: 419, ModelUID: "MODEL_MINIMAX_M2_1", CreditMultiplier: 1.0, Provider: "minimax"},
	// Windsurf 自研
	{CanonicalName: "swe-1.5", EnumValue: 377, ModelUID: "MODEL_SWE_1_5_SLOW", CreditMultiplier: 0.5, Provider: "windsurf"},
	{CanonicalName: "swe-1.5-fast", EnumValue: 359, ModelUID: "MODEL_SWE_1_5", CreditMultiplier: 0.5, Provider: "windsurf"},
	{CanonicalName: "swe-1.5-thinking", EnumValue: 369, ModelUID: "MODEL_SWE_1_5_THINKING", CreditMultiplier: 0.75, Provider: "windsurf"},
	{CanonicalName: "swe-1.6", EnumValue: 420, ModelUID: "MODEL_SWE_1_6", CreditMultiplier: 0.5, Provider: "windsurf"},
	{CanonicalName: "swe-1.6-fast", EnumValue: 421, ModelUID: "MODEL_SWE_1_6_FAST", CreditMultiplier: 0.5, Provider: "windsurf"},
}

var aliases = map[string]string{
	"claude-3-5-sonnet-latest":      "claude-3.5-sonnet",
	"claude-3-7-sonnet-latest":      "claude-3.7-sonnet",
	"claude-haiku-4-5":              "claude-4.5-haiku",
	"claude-haiku-4-5-20251001":     "claude-4.5-haiku",
	"claude-haiku-4-5-latest":       "claude-4.5-haiku",
	"claude-sonnet-4-5":             "claude-4.5-sonnet",
	"claude-sonnet-4-5-20250929":    "claude-4.5-sonnet",
	"claude-sonnet-4-5-latest":      "claude-4.5-sonnet",
	"claude-sonnet-4-5-thinking":    "claude-4.5-sonnet-thinking",
	"claude-opus-4-5":               "claude-4.5-opus",
	"claude-opus-4-5-20251101":      "claude-4.5-opus",
	"claude-opus-4-5-latest":        "claude-4.5-opus",
	"claude-sonnet-4-0":             "claude-4-sonnet",
	"claude-sonnet-4-20250514":      "claude-4-sonnet",
	"claude-opus-4-0":               "claude-4-opus",
	"claude-opus-4-20250514":        "claude-4-opus",
	"claude-opus-4-1":               "claude-4.1-opus",
	"claude-opus-4-1-20250805":      "claude-4.1-opus",
	"claude-sonnet-4-6":             "claude-sonnet-4.6",
	"claude-sonnet-4-6-thinking":    "claude-sonnet-4.6-thinking",
	"claude-sonnet-4-6-1m":          "claude-sonnet-4.6-1m",
	"claude-sonnet-4-6-thinking-1m": "claude-sonnet-4.6-thinking-1m",
	"claude-opus-4-6":               "claude-opus-4.6",
	"claude-opus-4-6-thinking":      "claude-opus-4.6-thinking",
	"claude-4.6":                    "claude-sonnet-4.6",
	"claude-4.6-thinking":           "claude-sonnet-4.6-thinking",
	"claude-opus-4-7":               "claude-opus-4-7-medium",
	"claude-opus-4-7-latest":        "claude-opus-4-7-medium",
	"claude-opus-4-7-thinking":      "claude-opus-4-7-medium-thinking",
	"claude-opus-4.7":               "claude-opus-4-7-medium",
	"claude-opus-4.7-thinking":      "claude-opus-4-7-medium-thinking",
	"sonnet-4.6":                    "claude-sonnet-4.6",
	"sonnet-4.6-thinking":           "claude-sonnet-4.6-thinking",
	"opus-4.6":                      "claude-opus-4.6",
	"opus-4.6-thinking":             "claude-opus-4.6-thinking",
	"opus-4.7":                      "claude-opus-4-7-medium",
	"opus-4.7-thinking":             "claude-opus-4-7-medium-thinking",
	"ws-haiku":                      "claude-4.5-haiku",
	"ws-opus":                       "claude-opus-4.6",
	"ws-opus-thinking":              "claude-opus-4.6-thinking",
	"ws-sonnet":                     "claude-sonnet-4.6",
	"ws-sonnet-thinking":            "claude-sonnet-4.6-thinking",
	"gpt-5-2025-08-07":              "gpt-5",
	"gpt-5.2-medium":                "gpt-5.2",
	"gpt-5-2-medium":                "gpt-5.2",
	"gpt-5-5-low":                   "gpt-5.5-low",
	"gpt-5-5-medium":                "gpt-5.5-medium",
	"gpt-5-5-high":                  "gpt-5.5-high",
	"gpt-5-5-xhigh":                 "gpt-5.5-xhigh",
	"gpt-5-5-none":                  "gpt-5.5-none",
	"gpt-5.5":                       "gpt-5.5-medium",
	"gpt-5-5":                       "gpt-5.5-medium",
	"kimi-k2-5":                     "kimi-k2.5",
	"minimax-m2-5":                  "minimax-m2.5",
	"swe-1-6":                       "swe-1.6",
	"swe-1-6-fast":                  "swe-1.6-fast",
}

// Model 用于 API 响应的简化模型结构（与 kiro.Model 对齐）
type Model struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
}

// DefaultModels 无 model_mapping 时的默认测试模型列表
var DefaultModels = []Model{
	{ID: "claude-sonnet-4.6", Type: "model", DisplayName: "Claude Sonnet 4.6"},
	{ID: "claude-opus-4.6", Type: "model", DisplayName: "Claude Opus 4.6"},
	{ID: "claude-opus-4-7-medium", Type: "model", DisplayName: "Claude Opus 4.7 Medium"},
	{ID: "claude-4.5-sonnet", Type: "model", DisplayName: "Claude 4.5 Sonnet"},
	{ID: "claude-4.5-opus", Type: "model", DisplayName: "Claude 4.5 Opus"},
	{ID: "gpt-5", Type: "model", DisplayName: "GPT-5"},
	{ID: "gpt-5-medium", Type: "model", DisplayName: "GPT-5 Medium"},
	{ID: "gemini-2.5-pro", Type: "model", DisplayName: "Gemini 2.5 Pro"},
}

// WindsurfModels 返回所有支持的模型
func WindsurfModels() []WindsurfModel {
	return models
}

// ResolveWindsurfModel 根据名称解析模型（支持别名和大小写不敏感）
func ResolveWindsurfModel(name string) (*WindsurfModel, bool) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return nil, false
	}
	if canonical, ok := aliases[normalized]; ok {
		normalized = canonical
	}
	for i := range models {
		if strings.EqualFold(models[i].CanonicalName, normalized) {
			return &models[i], true
		}
		if models[i].ModelUID != "" && strings.EqualFold(models[i].ModelUID, normalized) {
			return &models[i], true
		}
	}
	return nil, false
}
