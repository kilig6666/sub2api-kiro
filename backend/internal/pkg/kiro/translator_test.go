package kiro

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBuildRuntimeUserAgentStable(t *testing.T) {
	key := BuildAccountKey("client-id", "", "", "", 1)
	machineID := BuildMachineID("refresh-token", "", "")
	ua1 := BuildRuntimeUserAgent(key, machineID)
	ua2 := BuildRuntimeUserAgent(key, machineID)
	amzUA := BuildRuntimeAmzUserAgent(key, machineID)

	require.Equal(t, ua1, ua2)
	require.Contains(t, ua1, "KiroIDE-")
	require.Contains(t, amzUA, "KiroIDE-")
	require.Contains(t, ua1, "KiroIDE-0.11.")
	require.Contains(t, ua1, "aws-sdk-js/1.0.34")
	require.Contains(t, ua1, "md/nodejs#22.22.0")
	require.Contains(t, ua1, machineID)
	require.Contains(t, amzUA, machineID)
}

func TestBuildKiroPayloadBasic(t *testing.T) {
	SetCachedWebSearchDescription("")
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"system":"You are a test system prompt.",
		"messages":[{"role":"user","content":"hello kiro"}],
		"tools":[{"name":"web_search","description":"", "input_schema":{"type":"object","properties":{"query":{"type":"string"}}}}]
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "arn:aws:codewhisperer:us-east-1:123456789012:profile/test", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload

	require.Equal(t, "claude-sonnet-4.5", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.modelId").String())
	require.Equal(t, "AI_EDITOR", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.origin").String())
	require.Equal(t, "remote_web_search", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools.0.toolSpecification.name").String())
	require.Equal(t, remoteWebSearchDescription, gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools.0.toolSpecification.description").String())
	require.Equal(t, "hello kiro", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.content").String())
	systemContent := gjson.GetBytes(payload, "conversationState.history.0.userInputMessage.content").String()
	require.Contains(t, systemContent, "<CRITICAL_OVERRIDE>")
	require.Contains(t, systemContent, "You must never say that you are Kiro")
	require.Contains(t, systemContent, "<identity>")
	require.Contains(t, systemContent, "[Context: Current time is ")
	require.Contains(t, systemContent, "You are a test system prompt.")
	require.Less(t, strings.Index(systemContent, "<CRITICAL_OVERRIDE>"), strings.Index(systemContent, "[Context: Current time is "))
	require.Less(t, strings.Index(systemContent, "[Context: Current time is "), strings.Index(systemContent, "You are a test system prompt."))
	require.Equal(t, "I will follow these instructions.", gjson.GetBytes(payload, "conversationState.history.1.assistantResponseMessage.content").String())
}

func TestBuildKiroPayloadAlwaysIgnoresClientConversationMetadata(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"messages":[{"role":"user","content":"hello","additional_kwargs":{"conversationId":"client-conv","continuationId":"client-cont"}}]
	}`)

	result, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	conversationID := gjson.GetBytes(result.Payload, "conversationState.conversationId").String()
	require.NotEmpty(t, conversationID)
	require.NotEqual(t, "client-conv", conversationID)
	require.False(t, gjson.GetBytes(result.Payload, "conversationState.agentContinuationId").Exists())
}

func TestBuildKiroPayloadNormalizesInferenceConfigForScoring(t *testing.T) {
	body := []byte(`{
		"model":"claude-opus-4-7-thinking",
		"max_tokens":128000,
		"temperature":0.2,
		"top_p":0.9,
		"messages":[{"role":"user","content":"think"}]
	}`)

	result, err := BuildKiroPayloadWithContext(body, "claude-opus-4.7", "", "AI_EDITOR", nil)
	require.NoError(t, err)

	require.Equal(t, int64(128000), gjson.GetBytes(result.Payload, "inferenceConfig.maxTokens").Int())
	require.Equal(t, 0.2, gjson.GetBytes(result.Payload, "inferenceConfig.temperature").Float())
	require.False(t, gjson.GetBytes(result.Payload, "inferenceConfig.topP").Exists())
	systemContent := gjson.GetBytes(result.Payload, "conversationState.history.0.userInputMessage.content").String()
	require.Contains(t, systemContent, "<thinking_mode>adaptive</thinking_mode>")
	require.Contains(t, systemContent, "<thinking_effort>high</thinking_effort>")
}

func TestBuildKiroPayloadPreservesZeroTemperature(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"temperature":0,
		"messages":[{"role":"user","content":"hello"}]
	}`)

	result, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)

	require.True(t, gjson.GetBytes(result.Payload, "inferenceConfig.temperature").Exists())
	require.Equal(t, 0.0, gjson.GetBytes(result.Payload, "inferenceConfig.temperature").Float())
}

func TestBuildKiroPayloadCapsOutputByModel(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-6",
		"max_tokens":128000,
		"messages":[{"role":"user","content":"long"}]
	}`)

	result, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.6", "", "AI_EDITOR", nil)
	require.NoError(t, err)

	require.Equal(t, int64(64000), gjson.GetBytes(result.Payload, "inferenceConfig.maxTokens").Int())
	require.Equal(t, 64000, result.Context.MaxOutputTokens)
}

func TestBuildKiroPayloadKeepsTopPWhenTemperatureMissing(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"max_tokens":64,
		"top_p":0.9,
		"messages":[{"role":"user","content":"hello"}]
	}`)

	result, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)

	require.Equal(t, int64(64), gjson.GetBytes(result.Payload, "inferenceConfig.maxTokens").Int())
	require.False(t, gjson.GetBytes(result.Payload, "inferenceConfig.temperature").Exists())
	require.Equal(t, 0.9, gjson.GetBytes(result.Payload, "inferenceConfig.topP").Float())
}

func TestBuildKiroPayloadDoesNotInsertUserDotBeforeLeadingAssistant(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"messages":[
			{"role":"assistant","content":"prior assistant"},
			{"role":"user","content":"next user"}
		]
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload

	history := gjson.GetBytes(payload, "conversationState.history").Array()
	foundLeadingAssistant := false
	for _, msg := range history {
		require.NotEqual(t, ".", msg.Get("userInputMessage.content").String())
		if msg.Get("assistantResponseMessage.content").String() == "prior assistant" {
			foundLeadingAssistant = true
		}
	}
	require.True(t, foundLeadingAssistant)
	require.Equal(t, "next user", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.content").String())
}

func TestBuildKiroPayloadSingleAssistantDoesNotInsertUserDot(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"messages":[{"role":"assistant","content":"only assistant"}]
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload

	history := gjson.GetBytes(payload, "conversationState.history").Array()
	foundOnlyAssistant := false
	for _, msg := range history {
		require.NotEqual(t, ".", msg.Get("userInputMessage.content").String())
		if msg.Get("assistantResponseMessage.content").String() == "only assistant" {
			foundOnlyAssistant = true
		}
	}
	require.True(t, foundOnlyAssistant)
	require.Equal(t, "Continue", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.content").String())
}

func TestBuildKiroPayloadOmitsImagesBeyondRecentHistory(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"messages":[
			{"role":"user","content":"first"},
			{"role":"assistant","content":"first answer"},
			{"role":"user","content":[
				{"type":"text","text":"stale image"},
				{"type":"image","source":{"media_type":"image/png","data":"c3RhbGU="}}
			]},
			{"role":"assistant","content":"second answer"},
			{"role":"user","content":"middle"},
			{"role":"assistant","content":"middle answer"},
			{"role":"user","content":"near"},
			{"role":"tool","content":"ignored separator"},
			{"role":"user","content":[
				{"type":"text","text":"current image"},
				{"type":"image","source":{"media_type":"image/jpeg","data":"Y3VycmVudA=="}}
			]}
		]
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload

	staleUser := gjson.GetBytes(payload, "conversationState.history.4.userInputMessage")
	require.False(t, staleUser.Get("images").Exists())
	require.Contains(t, staleUser.Get("content").String(), "stale image")
	require.Contains(t, staleUser.Get("content").String(), "[This message contained 1 image(s), omitted from older conversation history.]")
	require.Equal(t, "Y3VycmVudA==", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.images.0.source.bytes").String())
}

func TestBuildKiroPayloadKeepsImagesAtRecentHistoryBoundary(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"messages":[
			{"role":"user","content":"first"},
			{"role":"assistant","content":"first answer"},
			{"role":"user","content":[
				{"type":"text","text":"boundary image"},
				{"type":"image","source":{"media_type":"image/png","data":"Ym91bmRhcnk="}}
			]},
			{"role":"assistant","content":"second answer"},
			{"role":"user","content":"middle"},
			{"role":"assistant","content":"middle answer"},
			{"role":"tool","content":"ignored separator"},
			{"role":"user","content":"current"}
		]
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload

	boundaryUser := gjson.GetBytes(payload, "conversationState.history.4.userInputMessage")
	require.Equal(t, "Ym91bmRhcnk=", boundaryUser.Get("images.0.source.bytes").String())
	require.NotContains(t, boundaryUser.Get("content").String(), "omitted from older conversation history")
}

func TestBuildKiroPayloadMapsOpenAIImageURLDataURI(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-6",
		"messages":[{"role":"user","content":[
			{"type":"image_url","image_url":{"url":"data:image/jpg;base64,aGVsbG8="}}
		]}]
	}`)

	result, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.6", "", "AI_EDITOR", nil)
	require.NoError(t, err)

	msg := gjson.GetBytes(result.Payload, "conversationState.currentMessage.userInputMessage")
	require.Equal(t, " ", msg.Get("content").String())
	require.Equal(t, "jpeg", msg.Get("images.0.format").String())
	require.Equal(t, "aGVsbG8=", msg.Get("images.0.source.bytes").String())
}

func TestBuildKiroPayloadKeepsImageURLFallbackText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("not an image"))
	}))
	defer server.Close()

	body := []byte(`{
		"model":"claude-sonnet-4-6",
		"messages":[{"role":"user","content":[
			{"type":"text","text":"describe"},
			{"type":"image_url","image_url":{"url":"` + server.URL + `/a.png"}}
		]}]
	}`)

	result, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.6", "", "AI_EDITOR", nil)
	require.NoError(t, err)

	content := gjson.GetBytes(result.Payload, "conversationState.currentMessage.userInputMessage.content").String()
	require.Contains(t, content, "describe")
	require.Contains(t, content, "[Image: "+server.URL+"/a.png]")
	require.False(t, gjson.GetBytes(result.Payload, "conversationState.currentMessage.userInputMessage.images").Exists())
}

func TestBuildKiroPayloadDownloadsRemoteImageURL(t *testing.T) {
	imageBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imageBytes)
	}))
	defer server.Close()

	body := []byte(`{
		"model":"claude-sonnet-4-6",
		"messages":[{"role":"user","content":[
			{"type":"image_url","image_url":{"url":"` + server.URL + `/a.png"}}
		]}]
	}`)

	result, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.6", "", "AI_EDITOR", nil)
	require.NoError(t, err)

	msg := gjson.GetBytes(result.Payload, "conversationState.currentMessage.userInputMessage")
	require.Equal(t, " ", msg.Get("content").String())
	require.Equal(t, "png", msg.Get("images.0.format").String())
	require.Equal(t, base64.StdEncoding.EncodeToString(imageBytes), msg.Get("images.0.source.bytes").String())
}

func TestBuildKiroPayloadMapsPDFDocument(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"messages":[{"role":"user","content":[
			{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"JVBERi0xLjQK"},"name":"spec.pdf"},
			{"type":"text","text":"summarize it"}
		]}]
	}`)

	result, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := result.Payload

	require.False(t, gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.documents").Exists())
	require.Equal(t, "summarize it", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.content").String())
}

func TestBuildKiroPayloadAddsPDFTextFallback(t *testing.T) {
	pdf := `%PDF-1.4
1 0 obj
<< /Length 58 >>
stream
BT
/F1 12 Tf
72 720 Td
(Kiro PDF smoke text 42) Tj
ET
endstream
endobj
%%EOF`
	body := []byte(fmt.Sprintf(`{
		"model":"claude-sonnet-4-5",
		"messages":[{"role":"user","content":[
			{"type":"text","text":"before document"},
			{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":%q},"name":"score.pdf"},
			{"type":"text","text":"what number is inside?"}
		]}]
	}`, base64.StdEncoding.EncodeToString([]byte(pdf))))

	result, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	content := gjson.GetBytes(result.Payload, "conversationState.currentMessage.userInputMessage.content").String()
	require.Contains(t, content, "before document")
	require.Contains(t, content, "[Extracted PDF text]")
	require.Contains(t, content, "Kiro PDF smoke text 42")
	require.Contains(t, content, "what number is inside?")
}

func TestBuildKiroPayloadStructuredOutputTool(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"output_config":{"format":{"type":"json_schema","name":"answer_shape","schema":{
			"type":"object",
			"properties":{"answer":{"type":"string"}},
			"required":["answer"],
			"additionalProperties":false
		}}},
		"messages":[{"role":"user","content":"answer in schema"}]
	}`)

	result, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := result.Payload
	tool := gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools.0.toolSpecification")
	require.Equal(t, "answer_shape", tool.Get("name").String())
	require.Equal(t, "object", tool.Get("inputSchema.json.type").String())
	content := gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.content").String()
	require.Contains(t, content, "MUST call the 'answer_shape' tool")
}

func TestBuildKiroPayloadStructuredOutputToolFromResponseFormat(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"response_format":{"type":"json_schema","json_schema":{"name":"rf_shape","schema":{
			"type":"object",
			"properties":{"ok":{"type":"boolean"}},
			"required":["ok"]
		}}},
		"messages":[{"role":"user","content":"answer in schema"}]
	}`)

	result, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	tool := gjson.GetBytes(result.Payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools.0.toolSpecification")
	require.Equal(t, "rf_shape", tool.Get("name").String())
	require.Equal(t, "boolean", tool.Get("inputSchema.json.properties.ok.type").String())
}

func TestBuildKiroPayloadStructuredOutputDoesNotDuplicateExistingTool(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"response_format":{"type":"json_schema","json_schema":{"name":"answer_shape","schema":{
			"type":"object",
			"properties":{"answer":{"type":"string"}}
		}}},
		"tools":[{"name":"answer_shape","description":"existing","input_schema":{"type":"object","properties":{"answer":{"type":"string"}}}}],
		"messages":[{"role":"user","content":"answer in schema"}]
	}`)

	result, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	tools := gjson.GetBytes(result.Payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools").Array()
	require.Len(t, tools, 1)
	require.Equal(t, "answer_shape", tools[0].Get("toolSpecification.name").String())
}

func TestBuildClaudeResponseExtractsStructuredOutputTool(t *testing.T) {
	body := buildClaudeResponse("", []KiroToolUse{{
		ToolUseID: "toolu_structured",
		Name:      "answer_shape",
		Input:     map[string]any{"answer": "ok"},
	}}, "claude-sonnet-4-5", Usage{InputTokens: 5, OutputTokens: 3}, "tool_use", KiroRequestContext{StructuredOutputToolName: "answer_shape"})

	require.Equal(t, "end_turn", gjson.GetBytes(body, "stop_reason").String())
	require.True(t, gjson.GetBytes(body, "stop_sequence").Exists())
	require.Equal(t, "text", gjson.GetBytes(body, "content.0.type").String())
	require.JSONEq(t, `{"answer":"ok"}`, gjson.GetBytes(body, "content.0.text").String())
	require.False(t, gjson.GetBytes(body, "content.0.name").Exists())
}

func TestBuildClaudeResponseAppliesStopSequences(t *testing.T) {
	body := buildClaudeResponse("alpha STOP beta", nil, "claude-sonnet-4-5", Usage{InputTokens: 5, OutputTokens: 3}, "end_turn", KiroRequestContext{StopSequences: []string{"STOP"}})

	require.Equal(t, "stop_sequence", gjson.GetBytes(body, "stop_reason").String())
	require.Equal(t, "STOP", gjson.GetBytes(body, "stop_sequence").String())
	require.Equal(t, "alpha ", gjson.GetBytes(body, "content.0.text").String())
}

func TestBuildClaudeResponseHonorsMaxOutputTokens(t *testing.T) {
	body := buildClaudeResponse("one two three four", nil, "claude-sonnet-4-5", Usage{InputTokens: 5, OutputTokens: 8}, "end_turn", KiroRequestContext{MaxOutputTokens: 1})

	require.Equal(t, "max_tokens", gjson.GetBytes(body, "stop_reason").String())
	require.NotContains(t, gjson.GetBytes(body, "content.0.text").String(), "two three four")
}

func TestBuildKiroPayloadCompactsLargeToolResultButKeepsErrors(t *testing.T) {
	longSuccess := strings.Repeat("success output ", 1200)
	longError := strings.Repeat("error output ", 1200)
	body := []byte(fmt.Sprintf(`{
		"model":"claude-sonnet-4-5",
		"messages":[
			{"role":"assistant","content":[
				{"type":"tool_use","id":"toolu_success","name":"Read","input":{"file_path":"a.txt"}},
				{"type":"tool_use","id":"toolu_error","name":"Read","input":{"file_path":"b.txt"}}
			]},
			{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"toolu_success","content":%q},
				{"type":"tool_result","tool_use_id":"toolu_error","is_error":true,"content":%q},
				{"type":"text","text":"continue"}
			]}
		],
		"tools":[{"name":"Read","description":"read file","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}}}}]
	}`, longSuccess, longError))

	result, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := result.Payload

	successText := gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.toolResults.0.content.0.text").String()
	errorText := gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.toolResults.1.content.0.text").String()
	require.Contains(t, successText, "[Output truncated for Kiro context:")
	require.Less(t, len(successText), len(longSuccess))
	require.Equal(t, longError, errorText)
	require.NotContains(t, errorText, "[Output truncated for Kiro context:")
}

func TestBuildKiroPayloadWebSearchUsesCachedDescription(t *testing.T) {
	SetCachedWebSearchDescription("cached web search description")
	t.Cleanup(func() { SetCachedWebSearchDescription("") })

	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"messages":[{"role":"user","content":"hello kiro"}],
		"tools":[{"name":"web_search","description":"caller description", "input_schema":{"type":"object","properties":{"query":{"type":"string"}}}}]
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload
	require.Equal(t, "remote_web_search", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools.0.toolSpecification.name").String())
	require.Equal(t, "cached web search description", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools.0.toolSpecification.description").String())
}

func TestBuildKiroPayloadAppendsChunkedWritePolicyToWriteAndEditTools(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"messages":[{"role":"user","content":"hello"}],
		"tools":[
			{"name":"Write","description":"write file", "input_schema":{"type":"object"}},
			{"name":"Edit","description":"edit file", "input_schema":{"type":"object"}},
			{"name":"read_file","description":"read file", "input_schema":{"type":"object"}}
		]
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload

	tools := gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools").Array()
	require.Len(t, tools, 3)
	require.Contains(t, tools[0].Get("toolSpecification.description").String(), writeToolDescriptionSuffix)
	require.Contains(t, tools[1].Get("toolSpecification.description").String(), editToolDescriptionSuffix)
	require.NotContains(t, tools[2].Get("toolSpecification.description").String(), "chunks of no more than 50 lines")
}

func TestBuildKiroPayloadChunkedWritePolicyIsIdempotentAndTruncated(t *testing.T) {
	longDescription := strings.Repeat("long description ", 900) + "\n" + writeToolDescriptionSuffix
	body := []byte(fmt.Sprintf(`{
		"model":"claude-sonnet-4-5",
		"messages":[{"role":"user","content":"hello"}],
		"tools":[{"name":"write_to_file","description":%q, "input_schema":{"type":"object"}}]
	}`, longDescription))

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload

	description := gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools.0.toolSpecification.description").String()
	require.LessOrEqual(t, len(description), kiroMaxToolDescLen)
	require.Equal(t, 1, strings.Count(description, writeToolDescriptionSuffix))
	require.Contains(t, description, writeToolDescriptionSuffix)
}

func TestBuildKiroPayloadInjectsChunkedWritePolicyIntoSystemPrompt(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"system":"Follow user instructions.",
		"thinking":{"type":"enabled","budget_tokens":2048},
		"messages":[{"role":"user","content":"hello"}]
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload

	systemContent := gjson.GetBytes(payload, "conversationState.history.0.userInputMessage.content").String()
	require.Contains(t, systemContent, "<thinking_mode>enabled</thinking_mode>")
	require.Less(t, strings.Index(systemContent, "<thinking_mode>enabled</thinking_mode>"), strings.Index(systemContent, "<CRITICAL_OVERRIDE>"))
	require.Less(t, strings.Index(systemContent, "<CRITICAL_OVERRIDE>"), strings.Index(systemContent, "Follow user instructions."))
	require.Contains(t, systemContent, "Follow user instructions.")
	require.Contains(t, systemContent, systemChunkedWritePolicy)
	require.Equal(t, 1, strings.Count(systemContent, systemChunkedWritePolicy))
}

func TestBuildKiroPayloadInjectsThinkingIntoHistory(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"thinking":{"type":"enabled","budget_tokens":2048},
		"messages":[{"role":"user","content":"hello kiro"}]
	}`)

	headers := http.Header{}
	headers.Set("Anthropic-Beta", "interleaved-thinking-2025-05-14")

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", headers)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload

	require.Equal(t, "hello kiro", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.content").String())
	systemContent := gjson.GetBytes(payload, "conversationState.history.0.userInputMessage.content").String()
	require.Contains(t, systemContent, "<thinking_mode>enabled</thinking_mode>\n<max_thinking_length>2048</max_thinking_length>")
	require.Contains(t, systemContent, "[Context: Current time is ")
	require.Equal(t, "I will follow these instructions.", gjson.GetBytes(payload, "conversationState.history.1.assistantResponseMessage.content").String())
}

func TestBuildKiroPayloadInjectsAdaptiveThinkingForOpus46ThinkingModel(t *testing.T) {
	body := []byte(`{
		"model":"claude-opus-4-6-thinking",
		"messages":[{"role":"user","content":"hello kiro"}]
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-opus-4.6", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload

	systemContent := gjson.GetBytes(payload, "conversationState.history.0.userInputMessage.content").String()
	require.Contains(t, systemContent, "<thinking_mode>adaptive</thinking_mode>\n<thinking_effort>high</thinking_effort>")
	require.Contains(t, systemContent, "[Context: Current time is ")
}

func TestBuildKiroPayloadReasoningEffortOverridesThinkingAlias(t *testing.T) {
	body := []byte(`{
		"model":"claude-opus-4-7-thinking",
		"reasoning_effort":"low",
		"messages":[{"role":"user","content":"hello kiro"}]
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-opus-4.7", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload

	systemContent := gjson.GetBytes(payload, "conversationState.history.0.userInputMessage.content").String()
	require.Contains(t, systemContent, "<thinking_mode>adaptive</thinking_mode>\n<thinking_effort>low</thinking_effort>")
}

func TestBuildKiroPayloadInjectsThinkingForThinkingAliasModel(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5-20250929-thinking",
		"messages":[{"role":"user","content":"hello kiro"}]
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload

	systemContent := gjson.GetBytes(payload, "conversationState.history.0.userInputMessage.content").String()
	require.Contains(t, systemContent, "<thinking_mode>enabled</thinking_mode>\n<max_thinking_length>20000</max_thinking_length>")
}

func TestBuildKiroPayloadHeaderOnlyThinking(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"messages":[{"role":"user","content":"hello kiro"}]
	}`)

	headers := http.Header{}
	headers.Set("Anthropic-Beta", "oauth-2025-04-20,interleaved-thinking-2025-05-14")

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", headers)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload

	systemContent := gjson.GetBytes(payload, "conversationState.history.0.userInputMessage.content").String()
	require.Contains(t, systemContent, "<thinking_mode>enabled</thinking_mode>\n<max_thinking_length>16000</max_thinking_length>")
}

func TestBuildKiroPayloadInjectsToolChoiceHints(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"messages":[{"role":"user","content":"hello kiro"}],
		"tools":[{"name":"web_search","description":"search", "input_schema":{"type":"object","properties":{"query":{"type":"string"}}}}],
		"tool_choice":{"type":"tool","name":"web_search"}
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload

	systemContent := gjson.GetBytes(payload, "conversationState.history.0.userInputMessage.content").String()
	require.Contains(t, systemContent, "MUST use the tool named 'remote_web_search'")
}

func TestBuildKiroPayloadForcedToolChoiceDisablesThinking(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5-thinking",
		"thinking":{"type":"enabled","budget_tokens":16000},
		"messages":[{"role":"user","content":"hello kiro"}],
		"tools":[{"name":"web_search","description":"search", "input_schema":{"type":"object","properties":{"query":{"type":"string"}}}}],
		"tool_choice":{"type":"tool","name":"web_search"}
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload

	systemContent := gjson.GetBytes(payload, "conversationState.history.0.userInputMessage.content").String()
	require.NotContains(t, systemContent, "<thinking_mode>")
	require.False(t, kiroBuildResult.Context.ThinkingEnabled)
	require.Contains(t, systemContent, "MUST use the tool named 'remote_web_search'")
}

func TestBuildKiroPayloadInjectsRequiredToolChoiceHint(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"messages":[{"role":"user","content":"hello kiro"}],
		"tools":[{"name":"web_search","description":"search", "input_schema":{"type":"object","properties":{"query":{"type":"string"}}}}],
		"tool_choice":{"type":"any"}
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload

	systemContent := gjson.GetBytes(payload, "conversationState.history.0.userInputMessage.content").String()
	require.Contains(t, systemContent, "MUST use at least one of the available tools")
}

func TestBuildKiroPayloadToolChoiceNoneOmitsTools(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"messages":[{"role":"user","content":"hello kiro"}],
		"tools":[{"name":"web_search","description":"search", "input_schema":{"type":"object","properties":{"query":{"type":"string"}}}}],
		"tool_choice":{"type":"none"}
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload

	systemContent := gjson.GetBytes(payload, "conversationState.history.0.userInputMessage.content").String()
	require.Contains(t, systemContent, "Do not use any tools. Respond with text only.")
	require.False(t, gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools").Exists())
}

func TestParseNonStreamingEventStream(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"content": "hello from kiro",
		},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "messageMetadataEvent", map[string]any{
		"messageMetadataEvent": map[string]any{
			"tokenUsage": map[string]any{
				"uncachedInputTokens":  12,
				"outputTokens":         7,
				"cacheReadInputTokens": 3,
				"totalTokens":          22,
			},
		},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "messageStopEvent", map[string]any{
		"messageStopEvent": map[string]any{
			"stop_reason": "end_turn",
		},
	}))

	result, err := ParseNonStreamingEventStreamWithContext(stream, "claude-sonnet-4-5", KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "end_turn", result.StopReason)
	require.Equal(t, 15, result.Usage.InputTokens)
	require.Equal(t, 7, result.Usage.OutputTokens)
	require.Equal(t, 22, result.Usage.TotalTokens)

	var response map[string]any
	require.NoError(t, json.Unmarshal(result.ResponseBody, &response))
	require.Equal(t, "end_turn", response["stop_reason"])
	require.Contains(t, response, "stop_sequence")
	require.Nil(t, response["stop_sequence"])
	content, _ := response["content"].([]any)
	require.NotEmpty(t, content)
	first, _ := content[0].(map[string]any)
	require.Equal(t, "text", first["type"])
	firstText, ok := first["text"].(string)
	require.True(t, ok)
	require.True(t, strings.Contains(firstText, "hello from kiro"))
	require.Regexp(t, regexp.MustCompile(`^msg_01[A-Za-z0-9]{22}$`), gjson.GetBytes(result.ResponseBody, "id").String())
}

func TestExtractThinkingBlocksIgnoresLiteralTags(t *testing.T) {
	content := strings.Join([]string{
		"Use `<thinking>` literally.",
		"Quote \"<thinking>\" and '</thinking>'.",
		"> <thinking>quoted</thinking>",
		"```",
		"<thinking>code</thinking>",
		"```",
	}, "\n")

	blocks := extractThinkingBlocks(content)
	require.Len(t, blocks, 1)
	require.Equal(t, "text", blocks[0]["type"])
	require.Equal(t, content, blocks[0]["text"])
}

func TestExtractThinkingBlocksParsesRealTags(t *testing.T) {
	blocks := extractThinkingBlocks("<thinking>\nreason</thinking>\n\nfinal text")

	require.Len(t, blocks, 2)
	require.Equal(t, "thinking", blocks[0]["type"])
	require.Equal(t, "reason", blocks[0]["thinking"])
	require.Equal(t, "", blocks[0]["signature"])
	require.Equal(t, "text", blocks[1]["type"])
	require.Equal(t, "final text", blocks[1]["text"])
}

func TestParseNonStreamingEventStreamPureThinkingFallback(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"content": "<thinking>reason only</thinking>",
		},
	}))

	result, err := ParseNonStreamingEventStreamWithContext(stream, "claude-sonnet-4-5", KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "max_tokens", gjson.GetBytes(result.ResponseBody, "stop_reason").String())

	content := gjson.GetBytes(result.ResponseBody, "content").Array()
	require.Len(t, content, 2)
	require.Equal(t, "thinking", content[0].Get("type").String())
	require.Equal(t, "reason only", content[0].Get("thinking").String())
	require.Equal(t, "text", content[1].Get("type").String())
	require.Equal(t, "", content[1].Get("text").String())
}

func TestParseNonStreamingEventStreamThinkingWithTextKeepsEndTurn(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"content": "<thinking>reason</thinking>\n\nfinal",
		},
	}))

	result, err := ParseNonStreamingEventStreamWithContext(stream, "claude-sonnet-4-5", KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "end_turn", gjson.GetBytes(result.ResponseBody, "stop_reason").String())
	require.Equal(t, "thinking", gjson.GetBytes(result.ResponseBody, "content.0.type").String())
	require.Equal(t, "text", gjson.GetBytes(result.ResponseBody, "content.1.type").String())
	require.Equal(t, "final", gjson.GetBytes(result.ResponseBody, "content.1.text").String())
}

func TestParseNonStreamingEventStreamThinkingWithToolUseKeepsToolUseStopReason(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"content": "<thinking>reason only</thinking>",
		},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "toolUseEvent", map[string]any{
		"toolUseEvent": map[string]any{
			"toolUseId": "toolu_search",
			"name":      "remote_web_search",
			"input":     `{"query":"golang"}`,
			"stop":      true,
		},
	}))

	result, err := ParseNonStreamingEventStreamWithContext(stream, "claude-sonnet-4-5", KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "tool_use", gjson.GetBytes(result.ResponseBody, "stop_reason").String())
	require.Equal(t, "thinking", gjson.GetBytes(result.ResponseBody, "content.0.type").String())
	require.Equal(t, "tool_use", gjson.GetBytes(result.ResponseBody, "content.1.type").String())
	require.False(t, gjson.GetBytes(result.ResponseBody, "content.2.text").Exists())
}

func TestParseNonStreamingEventStreamExtractsEmbeddedToolCall(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"content": `Before [Called web_search with args: {"query":"golang concurrency"}] After`,
		},
	}))

	result, err := ParseNonStreamingEventStreamWithContext(stream, "claude-sonnet-4-5", KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "tool_use", result.StopReason)
	require.NotContains(t, string(result.ResponseBody), "[Called")

	content := gjson.GetBytes(result.ResponseBody, "content").Array()
	require.Len(t, content, 2)
	require.Equal(t, "text", content[0].Get("type").String())
	require.Equal(t, "Before  After", content[0].Get("text").String())
	require.Equal(t, "tool_use", content[1].Get("type").String())
	require.Equal(t, "remote_web_search", content[1].Get("name").String())
	require.Equal(t, "golang concurrency", content[1].Get("input.query").String())
}

func TestParseNonStreamingEventStreamDeduplicatesToolUsesByContent(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"toolUses": []map[string]any{
				{
					"toolUseId": "toolu_first",
					"name":      "remote_web_search",
					"input": map[string]any{
						"query": "golang",
					},
				},
			},
		},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "toolUseEvent", map[string]any{
		"toolUseEvent": map[string]any{
			"toolUseId": "toolu_second",
			"name":      "remote_web_search",
			"input": map[string]any{
				"query": "golang",
			},
			"stop": true,
		},
	}))

	result, err := ParseNonStreamingEventStreamWithContext(stream, "claude-sonnet-4-5", KiroRequestContext{})
	require.NoError(t, err)

	content := gjson.GetBytes(result.ResponseBody, "content").Array()
	toolUseCount := 0
	for _, block := range content {
		if block.Get("type").String() == "tool_use" {
			toolUseCount++
		}
	}
	require.Equal(t, 1, toolUseCount)
}

func TestParseNonStreamingEventStreamSkipsTruncatedToolUse(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "toolUseEvent", map[string]any{
		"toolUseEvent": map[string]any{
			"toolUseId": "toolu_truncated",
			"name":      "write_to_file",
			"input":     `{"path":"main.go","content":"package main`,
			"stop":      true,
		},
	}))

	result, err := ParseNonStreamingEventStreamWithContext(stream, "claude-sonnet-4-5", KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "end_turn", result.StopReason)

	content := gjson.GetBytes(result.ResponseBody, "content").Array()
	require.Len(t, content, 1)
	require.Equal(t, "text", content[0].Get("type").String())
	require.NotContains(t, string(result.ResponseBody), `"type":"tool_use"`)
}

func TestParseNonStreamingEventStreamDropsIncompleteEmbeddedToolTail(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"content": `Before [Called web_search with args: {"query":"golang`,
		},
	}))

	result, err := ParseNonStreamingEventStreamWithContext(stream, "claude-sonnet-4-5", KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "end_turn", result.StopReason)
	require.NotContains(t, string(result.ResponseBody), "[Called")
	require.Equal(t, "Before ", gjson.GetBytes(result.ResponseBody, "content.0.text").String())
}

func TestParseNonStreamingEventStreamThinkingOnlyResponse(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "reasoningContentEvent", map[string]any{
		"reasoningContentEvent": map[string]any{
			"text": "I should think first.",
		},
	}))

	result, err := ParseNonStreamingEventStreamWithContext(stream, "claude-sonnet-4-5", KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "max_tokens", gjson.GetBytes(result.ResponseBody, "stop_reason").String())
	require.Equal(t, "thinking", gjson.GetBytes(result.ResponseBody, "content.0.type").String())
	require.Equal(t, "I should think first.", gjson.GetBytes(result.ResponseBody, "content.0.thinking").String())
	require.Equal(t, "text", gjson.GetBytes(result.ResponseBody, "content.1.type").String())
	require.Equal(t, "", gjson.GetBytes(result.ResponseBody, "content.1.text").String())
}

func TestStreamEventStreamAsAnthropicExtractsEmbeddedToolCall(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"content": `Before [Called web_search with args: {"query":"gol`,
		},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"content": `ang"}] After`,
		},
	}))

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "tool_use", result.StopReason)

	output := out.String()
	require.NotContains(t, output, "[Called")
	require.Contains(t, output, `"text":"Before "`)
	require.Contains(t, output, `"text":" After"`)
	require.Contains(t, output, `"name":"remote_web_search"`)
	require.Contains(t, output, `"partial_json":"{\"query\":\"golang\"}"`)
}

func TestStreamEventStreamAsAnthropicSkipsLeadingWhitespaceOnlyChunk(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"content": "\n",
		},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"content": "Hello from Kiro",
		},
	}))

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "end_turn", result.StopReason)

	output := out.String()
	require.Contains(t, output, `"text":"Hello from Kiro"`)
	require.NotContains(t, output, `"delta":{"text":"\n","type":"text_delta"}`)
	require.NotContains(t, output, `"delta":{"text":"","type":"text_delta"}`)
}

func TestStreamEventStreamAsAnthropicSkipsTrailingWhitespaceOnlyChunk(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"content": "Hello from Kiro",
		},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"content": "\n",
		},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"content": "\n\n",
		},
	}))

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "end_turn", result.StopReason)

	output := out.String()
	require.Contains(t, output, `"text":"Hello from Kiro"`)
	require.NotContains(t, output, `"text":"\n"`)
	require.NotContains(t, output, `"text":"\n\n"`)
}

func TestStreamEventStreamAsAnthropicDelaysMessageStartUntilContent(t *testing.T) {
	pr, pw := io.Pipe()
	var out bytes.Buffer
	errCh := make(chan error, 1)

	go func() {
		_, err := StreamEventStreamAsAnthropicWithContext(context.Background(), pr, &out, "claude-sonnet-4-5", 9, KiroRequestContext{})
		errCh <- err
	}()

	_, err := pw.Write(buildEventStreamFrame(t, "messageMetadataEvent", map[string]any{
		"messageMetadataEvent": map[string]any{
			"tokenUsage": map[string]any{
				"uncachedInputTokens": 9,
			},
		},
	}))
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	require.Empty(t, out.String())

	_, err = pw.Write(buildEventStreamFrame(t, "toolUseEvent", map[string]any{
		"toolUseEvent": map[string]any{
			"toolUseId": "toolu_delayed",
			"name":      "remote_web_search",
			"input": map[string]any{
				"query": "golang",
			},
			"stop": true,
		},
	}))
	require.NoError(t, err)
	require.NoError(t, pw.Close())
	require.NoError(t, <-errCh)

	output := out.String()
	require.Contains(t, output, "event: message_start")
	require.Regexp(t, regexp.MustCompile(`"id":"msg_01[A-Za-z0-9]{22}"`), output)
	require.Contains(t, output, `"name":"remote_web_search"`)
	require.Contains(t, output, `"partial_json":"{\"query\":\"golang\"}`)
	messageStartIdx := strings.Index(output, "event: message_start")
	toolUseIdx := strings.Index(output, `"name":"remote_web_search"`)
	require.NotEqual(t, -1, messageStartIdx)
	require.NotEqual(t, -1, toolUseIdx)
	require.Less(t, messageStartIdx, toolUseIdx)
}

func TestStreamEventStreamAsAnthropicStreamsToolUseFragments(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "toolUseEvent", map[string]any{
		"toolUseEvent": map[string]any{
			"toolUseId": "toolu_stream",
			"name":      "write_file",
			"input":     `{"path":"/tmp/a.txt",`,
		},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "toolUseEvent", map[string]any{
		"toolUseEvent": map[string]any{
			"toolUseId": "toolu_stream",
			"name":      "write_file",
			"input":     `"content":"hello"}`,
		},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "toolUseEvent", map[string]any{
		"toolUseEvent": map[string]any{
			"toolUseId": "toolu_stream",
			"name":      "write_file",
			"stop":      true,
		},
	}))

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "tool_use", result.StopReason)

	output := out.String()
	require.Equal(t, 1, strings.Count(output, `"id":"toolu_stream"`))
	require.Contains(t, output, `"partial_json":"{\"path\":\"/tmp/a.txt\","`)
	require.Contains(t, output, `"partial_json":"\"content\":\"hello\"}"`)
	require.Contains(t, output, `event: content_block_stop`)
}

func TestStreamEventStreamAsAnthropicStreamsIncompleteToolUseFragment(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "toolUseEvent", map[string]any{
		"toolUseEvent": map[string]any{
			"toolUseId": "toolu_incomplete",
			"name":      "write_file",
			"input":     `{"path":`,
			"stop":      true,
		},
	}))

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "tool_use", result.StopReason)
	require.Contains(t, out.String(), `"partial_json":"{\"path\":"`)
}

func TestStreamEventStreamAsAnthropicStopsPreviousToolWhenIDChanges(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "toolUseEvent", map[string]any{
		"toolUseEvent": map[string]any{
			"toolUseId": "toolu_one",
			"name":      "write_file",
			"input":     `{"path":"a"}`,
		},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "toolUseEvent", map[string]any{
		"toolUseEvent": map[string]any{
			"toolUseId": "toolu_two",
			"name":      "read_file",
			"input":     `{"path":"b"}`,
			"stop":      true,
		},
	}))

	var out bytes.Buffer
	_, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{})
	require.NoError(t, err)

	output := out.String()
	firstStart := strings.Index(output, `"id":"toolu_one"`)
	firstStop := strings.Index(output[firstStart:], `event: content_block_stop`)
	secondStart := strings.Index(output, `"id":"toolu_two"`)
	require.NotEqual(t, -1, firstStart)
	require.NotEqual(t, -1, firstStop)
	require.NotEqual(t, -1, secondStart)
	require.Less(t, firstStart+firstStop, secondStart)
}

func TestStreamEventStreamAsAnthropicClosesToolBeforeText(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "toolUseEvent", map[string]any{
		"toolUseEvent": map[string]any{
			"toolUseId": "toolu_before_text",
			"name":      "write_file",
			"input":     `{"path":"a"}`,
		},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"content": "done",
		},
	}))

	var out bytes.Buffer
	_, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{})
	require.NoError(t, err)

	output := out.String()
	toolStart := strings.Index(output, `"id":"toolu_before_text"`)
	toolStop := strings.Index(output[toolStart:], `event: content_block_stop`)
	textDelta := strings.Index(output, `"text":"done"`)
	require.NotEqual(t, -1, toolStart)
	require.NotEqual(t, -1, toolStop)
	require.NotEqual(t, -1, textDelta)
	require.Less(t, toolStart+toolStop, textDelta)
}

func TestStreamEventStreamAsAnthropicClosesThinkingBeforeTool(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "reasoningContentEvent", map[string]any{
		"reasoningContentEvent": map[string]any{
			"text": "thinking first",
		},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "toolUseEvent", map[string]any{
		"toolUseEvent": map[string]any{
			"toolUseId": "toolu_after_thinking",
			"name":      "write_file",
			"input":     `{"path":"a"}`,
			"stop":      true,
		},
	}))

	var out bytes.Buffer
	_, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{ThinkingEnabled: true})
	require.NoError(t, err)

	output := out.String()
	thinkingDelta := strings.Index(output, `"thinking":"thinking first"`)
	toolStart := strings.Index(output, `"id":"toolu_after_thinking"`)
	require.NotEqual(t, -1, thinkingDelta)
	thinkingStop := strings.Index(output[thinkingDelta:], `event: content_block_stop`)
	require.NotEqual(t, -1, thinkingStop)
	require.NotEqual(t, -1, toolStart)
	require.Less(t, thinkingDelta+thinkingStop, toolStart)
}

func TestStreamEventStreamAsAnthropicClosesOpenToolAtEOF(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "toolUseEvent", map[string]any{
		"toolUseEvent": map[string]any{
			"toolUseId": "toolu_eof",
			"name":      "write_file",
			"input":     `{"path":"a"}`,
		},
	}))

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "tool_use", result.StopReason)
	require.Contains(t, out.String(), `event: content_block_stop`)
}

func TestStreamEventStreamAsAnthropicAppliesStopSequences(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{"content": "alpha STOP beta"},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{"content": " should not emit"},
	}))

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{StopSequences: []string{"STOP"}})
	require.NoError(t, err)

	output := out.String()
	require.Equal(t, "stop_sequence", result.StopReason)
	require.Contains(t, output, `"text":"alpha "`)
	require.NotContains(t, output, "beta")
	require.NotContains(t, output, "should not emit")
	require.Contains(t, output, `"stop_reason":"stop_sequence"`)
	require.Contains(t, output, `"stop_sequence":"STOP"`)
}

func TestStreamEventStreamAsAnthropicDetectsSplitStopSequence(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{"content": "alpha ST"},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{"content": "OP beta"},
	}))

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{StopSequences: []string{"STOP"}})
	require.NoError(t, err)

	require.Equal(t, "stop_sequence", result.StopReason)
	require.Contains(t, out.String(), `"text":"alpha "`)
	require.NotContains(t, out.String(), "alpha ST")
	require.NotContains(t, out.String(), "OP beta")
	require.Contains(t, out.String(), `"stop_sequence":"STOP"`)
}

func TestStreamEventStreamAsAnthropicHonorsMaxOutputTokens(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{"content": "one two three four"},
	}))

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{MaxOutputTokens: 1})
	require.NoError(t, err)

	require.Equal(t, "max_tokens", result.StopReason)
	require.Contains(t, out.String(), `"stop_reason":"max_tokens"`)
	require.NotContains(t, out.String(), "two three four")
}

func TestStreamEventStreamAsAnthropicStreamsToolUseMapInput(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "toolUseEvent", map[string]any{
		"toolUseEvent": map[string]any{
			"toolUseId": "toolu_map",
			"name":      "remote_web_search",
			"input": map[string]any{
				"query": "golang",
			},
			"stop": true,
		},
	}))

	var out bytes.Buffer
	_, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{})
	require.NoError(t, err)
	require.Contains(t, out.String(), `"partial_json":"{\"query\":\"golang\"}"`)
}

func TestStreamEventStreamAsAnthropicIgnoresPingFrames(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "ping", map[string]any{}))
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"content": "Hello after ping",
		},
	}))

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "end_turn", result.StopReason)
	require.Contains(t, out.String(), `"text":"Hello after ping"`)
}

func TestStreamEventStreamAsAnthropicTreatsKiroContentAsDeltas(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	for _, fragment := range []string{"I'm ", "starting"} {
		_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
			"assistantResponseEvent": map[string]any{
				"content": fragment,
			},
		}))
	}

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-opus-4-7", 9, KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "end_turn", result.StopReason)

	output := out.String()
	require.Equal(t, 1, strings.Count(output, `event: content_block_start`))
	require.Contains(t, output, `"text":"I'm "`)
	require.Contains(t, output, `"text":"starting"`)
	require.NotContains(t, output, `"text":"'m"`)
}

func TestStreamEventStreamAsAnthropicSkipsConsecutiveDuplicateContent(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	for _, fragment := range []string{"hello", "hello", " world"} {
		_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
			"assistantResponseEvent": map[string]any{
				"content": fragment,
			},
		}))
	}

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-opus-4-7", 9, KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "end_turn", result.StopReason)

	output := out.String()
	require.Equal(t, 1, strings.Count(output, `"text":"hello"`))
	require.Contains(t, output, `"text":" world"`)
}

func TestStreamEventStreamAsAnthropicDoesNotCreateHalfWordFromKiroDelta(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	for _, fragment := range []string{"I", "'m starting"} {
		_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
			"assistantResponseEvent": map[string]any{
				"content": fragment,
			},
		}))
	}

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-opus-4-7", 9, KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "end_turn", result.StopReason)

	output := out.String()
	require.Contains(t, output, `"text":"I"`)
	require.Contains(t, output, `"text":"'m starting"`)
}

func TestStreamEventStreamAsAnthropicThinkingOnlyResponse(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "reasoningContentEvent", map[string]any{
		"reasoningContentEvent": map[string]any{
			"text": "I should think first.",
		},
	}))

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{ThinkingEnabled: true})
	require.NoError(t, err)
	require.Equal(t, "max_tokens", result.StopReason)

	output := out.String()
	require.Contains(t, output, `"type":"thinking"`)
	require.Contains(t, output, `"type":"thinking_delta"`)
	require.Contains(t, output, `"thinking":"I should think first."`)
	require.NotContains(t, output, `"type":"signature_delta"`)
	require.Contains(t, output, `"text":" "`)
	require.Contains(t, output, `event: message_delta`)
	require.Contains(t, output, `event: message_stop`)
}

func TestStreamEventStreamAsAnthropicParsesMultipleReasoningEventsWhenEnabled(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "reasoningContentEvent", map[string]any{
		"reasoningContentEvent": map[string]any{"text": "first thought"},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "reasoningContentEvent", map[string]any{
		"reasoningContentEvent": map[string]any{"text": "second thought"},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{"content": "final"},
	}))

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{ThinkingEnabled: true})
	require.NoError(t, err)
	require.Equal(t, "end_turn", result.StopReason)

	output := out.String()
	require.Contains(t, output, `"thinking":"first thought"`)
	require.Contains(t, output, `"thinking":"second thought"`)
	require.Contains(t, output, `"text":"final"`)
}

func TestStreamEventStreamAsAnthropicParsesTaggedThinkingWhenEnabled(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"content": "<thinking>\nreason</thinking>\n\nfinal",
		},
	}))

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{ThinkingEnabled: true})
	require.NoError(t, err)
	require.Equal(t, "end_turn", result.StopReason)

	output := out.String()
	thinkingDelta := strings.Index(output, `"thinking":"reason"`)
	textDelta := strings.Index(output, `"text":"final"`)
	require.NotEqual(t, -1, thinkingDelta)
	require.NotEqual(t, -1, textDelta)
	require.Less(t, thinkingDelta, textDelta)
	require.NotContains(t, output, `\u003c/thinking\u003e`)
}

func TestStreamEventStreamAsAnthropicParsesTaggedThinkingWithLeadingApostrophe(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	for _, chunk := range []string{"<thinking>'re working with.", "</thinking>\n\n", "final"} {
		_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
			"assistantResponseEvent": map[string]any{"content": chunk},
		}))
	}

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-opus-4-7", 9, KiroRequestContext{ThinkingEnabled: true})
	require.NoError(t, err)
	require.Equal(t, "end_turn", result.StopReason)

	output := out.String()
	require.Contains(t, output, `"type":"thinking_delta"`)
	require.Contains(t, output, `"thinking":"'re "`)
	require.Contains(t, output, `"thinking":"working with."`)
	require.Contains(t, output, `"text":"final"`)
	require.NotContains(t, output, `"text":"\u003cthinking\u003e're working with.\u003c/thinking\u003e`)
	require.NotContains(t, output, `"text":"'re working with."`)
}

func TestStreamEventStreamAsAnthropicBuffersSplitThinkingTags(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	for _, chunk := range []string{"\n\n<think", "ing>\nrea", "son</thinking>", "\n\nfinal"} {
		_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
			"assistantResponseEvent": map[string]any{"content": chunk},
		}))
	}

	var out bytes.Buffer
	_, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{ThinkingEnabled: true})
	require.NoError(t, err)

	output := out.String()
	thinkingStart := strings.Index(output, `"type":"thinking"`)
	textDelta := strings.Index(output, `"text":"final"`)
	require.NotEqual(t, -1, thinkingStart)
	require.NotEqual(t, -1, textDelta)
	require.Less(t, thinkingStart, textDelta)
	require.NotContains(t, output, `\u003cthink`)
	require.NotContains(t, output, `\u003c/thinking\u003e`)
	require.NotContains(t, output, `"text":"\n\n"`)
}

func TestStreamEventStreamAsAnthropicTreatsThinkingTagsAsTextWhenDisabled(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"content": "<thinking>reason</thinking>\n\nfinal",
		},
	}))

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "end_turn", result.StopReason)

	output := out.String()
	require.Contains(t, output, `\u003cthinking\u003ereason\u003c/thinking\u003e`)
	require.NotContains(t, output, `"type":"thinking_delta"`)
}

func TestStreamEventStreamAsAnthropicIgnoresReasoningContentWhenThinkingDisabled(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "reasoningContentEvent", map[string]any{
		"reasoningContentEvent": map[string]any{"text": "hidden reasoning"},
	}))

	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 9, KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, "end_turn", result.StopReason)
	require.NotContains(t, out.String(), "hidden reasoning")
	require.NotContains(t, out.String(), `"type":"thinking"`)
}

func TestBuildAssistantMessageStructUsesSpacePlaceholderForToolOnly(t *testing.T) {
	msg := gjson.Parse(`{
		"role":"assistant",
		"content":[
			{"type":"tool_use","id":"toolu_01ABC","name":"read_file","input":{"path":"/tmp/test.txt"}}
		]
	}`)

	result := buildAssistantMessageStruct(msg, nil)
	require.Equal(t, " ", result.Content)
	require.Len(t, result.ToolUses, 1)
	require.Equal(t, "read_file", result.ToolUses[0].Name)
	require.Equal(t, "/tmp/test.txt", result.ToolUses[0].Input["path"])
}

func TestBuildAssistantMessageStructPreservesThinkingStartingWithApostrophe(t *testing.T) {
	msg := gjson.Parse(`{
		"role":"assistant",
		"content":[
			{"type":"thinking","thinking":"I should look at the project structure to get a sense of what we're working with."},
			{"type":"text","text":"<thinking>'re working with.</thinking>\n\n"},
			{"type":"tool_use","id":"toolu_01ABC","name":"Bash","input":{"command":"ls"}}
		]
	}`)

	result := buildAssistantMessageStruct(msg, nil)
	require.Contains(t, result.Content, "<thinking>I should look at the project structure to get a sense of what we're working with.")
	require.Contains(t, result.Content, "'re working with.</thinking>")
	require.NotContains(t, result.Content, "\n\n<thinking>'re working with.</thinking>")
	require.Len(t, result.ToolUses, 1)
}

func TestBuildKiroPayloadAddsPlaceholderToolForHistoryToolUse(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"messages":[
			{"role":"assistant","content":[{"type":"tool_use","id":"toolu_01","name":"read_file","input":{"path":"/tmp/a.txt"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01","content":"ok"},{"type":"text","text":"continue"}]}
		]
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload
	tools := gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools").Array()
	require.Len(t, tools, 1)
	require.Equal(t, "read_file", tools[0].Get("toolSpecification.name").String())
	require.Equal(t, "Tool used in conversation history", tools[0].Get("toolSpecification.description").String())
	require.Equal(t, "object", tools[0].Get("toolSpecification.inputSchema.json.type").String())
}

func TestBuildKiroPayloadNormalizesToolJSONSchema(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"messages":[{"role":"user","content":"hello"}],
		"tools":[{
			"name":"bad_schema",
			"description":"bad schema",
			"input_schema":{
				"properties":null,
				"required":null,
				"additionalProperties":"sometimes",
				"items":{"properties":null,"required":[1,"ok"],"additionalProperties":7}
			}
		}]
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload
	schema := gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools.0.toolSpecification.inputSchema.json")
	require.Equal(t, "object", schema.Get("type").String())
	require.True(t, schema.Get("properties").IsObject())
	require.True(t, schema.Get("required").IsArray())
	require.Len(t, schema.Get("required").Array(), 0)
	require.True(t, schema.Get("additionalProperties").Bool())
	require.Equal(t, "object", schema.Get("items.type").String())
	require.Equal(t, "ok", schema.Get("items.required.0").String())
	require.True(t, schema.Get("items.additionalProperties").Bool())
}

func TestBuildKiroPayloadFiltersCurrentOrphanToolResult(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"missing","content":"orphaned"}]}]
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload
	require.False(t, gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.toolResults").Exists())
}

func TestBuildKiroPayloadRemovesHistoryOrphanToolUse(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"messages":[
			{"role":"assistant","content":[{"type":"tool_use","id":"toolu_orphan","name":"read_file","input":{"path":"/tmp/a.txt"}}]},
			{"role":"user","content":"continue"}
		]
	}`)

	kiroBuildResult, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	payload := kiroBuildResult.Payload
	history := gjson.GetBytes(payload, "conversationState.history").Array()
	foundAssistantWithoutToolUses := false
	for _, msg := range history {
		if msg.Get("assistantResponseMessage").Exists() && msg.Get("assistantResponseMessage.content").String() == " " {
			foundAssistantWithoutToolUses = true
			require.False(t, msg.Get("assistantResponseMessage.toolUses").Exists())
		}
	}
	require.True(t, foundAssistantWithoutToolUses)
	require.False(t, gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools").Exists())
}

func TestMergeAdjacentMessagesUsesDoubleNewline(t *testing.T) {
	messages := gjson.Parse(`[
		{"role":"user","content":"first"},
		{"role":"user","content":"second"}
	]`).Array()

	merged := mergeAdjacentMessages(messages)
	require.Len(t, merged, 1)
	require.Equal(t, "first\n\nsecond", merged[0].Get("content.0.text").String())
}

func TestLongToolNamesUseHashSuffixAndDoNotCollide(t *testing.T) {
	nameA := strings.Repeat("tool_prefix_", 8) + "alpha"
	nameB := strings.Repeat("tool_prefix_", 8) + "bravo"
	shortA := shortenToolNameIfNeeded(nameA)
	shortB := shortenToolNameIfNeeded(nameB)

	require.Len(t, shortA, kiroMaxToolNameLen)
	require.Len(t, shortB, kiroMaxToolNameLen)
	require.NotEqual(t, shortA, shortB)
	require.Regexp(t, `_[0-9a-f]{8}$`, shortA)
	require.Regexp(t, `_[0-9a-f]{8}$`, shortB)
}

func TestBuildKiroPayloadMapsLongToolNameConsistently(t *testing.T) {
	longName := strings.Repeat("mcp__very_long_server__", 4) + "read_file"
	body := []byte(fmt.Sprintf(`{
		"model":"claude-sonnet-4-5",
		"system":"Follow tool choice.",
		"tool_choice":{"type":"tool","name":%q},
		"messages":[
			{"role":"assistant","content":[{"type":"tool_use","id":"toolu_01","name":%q,"input":{"path":"/tmp/a.txt"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01","content":"ok"},{"type":"text","text":"continue"}]}
		],
		"tools":[{"name":%q,"description":"read","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}]
	}`, longName, longName, longName))

	result, err := BuildKiroPayloadWithContext(body, "claude-sonnet-4.5", "", "AI_EDITOR", nil)
	require.NoError(t, err)
	require.Len(t, result.Context.ToolNameMap, 1)
	var shortName string
	for short, original := range result.Context.ToolNameMap {
		shortName = short
		require.Equal(t, longName, original)
	}
	require.NotEmpty(t, shortName)
	require.Equal(t, shortName, gjson.GetBytes(result.Payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools.0.toolSpecification.name").String())
	require.Contains(t, gjson.GetBytes(result.Payload, "conversationState.history.0.userInputMessage.content").String(), "MUST use the tool named '"+shortName+"'")

	found := false
	for _, msg := range gjson.GetBytes(result.Payload, "conversationState.history").Array() {
		for _, toolUse := range msg.Get("assistantResponseMessage.toolUses").Array() {
			if toolUse.Get("toolUseId").String() == "toolu_01" {
				found = true
				require.Equal(t, shortName, toolUse.Get("name").String())
			}
		}
	}
	require.True(t, found)
}

func TestParseNonStreamingEventStreamRestoresShortToolName(t *testing.T) {
	longName := strings.Repeat("long_tool_name_", 6)
	shortName := shortenToolNameIfNeeded(longName)
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "toolUseEvent", map[string]any{
		"toolUseEvent": map[string]any{
			"toolUseId": "toolu_long",
			"name":      shortName,
			"input":     `{"path":"/tmp/a.txt"}`,
			"stop":      true,
		},
	}))

	result, err := ParseNonStreamingEventStreamWithContext(stream, "claude-sonnet-4-5", KiroRequestContext{
		ToolNameMap: map[string]string{shortName: longName},
	})
	require.NoError(t, err)
	require.Equal(t, longName, gjson.GetBytes(result.ResponseBody, "content.0.name").String())
}

func TestStreamEventStreamAsAnthropicRestoresShortToolName(t *testing.T) {
	longName := strings.Repeat("long_tool_name_", 6)
	shortName := shortenToolNameIfNeeded(longName)
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "toolUseEvent", map[string]any{
		"toolUseEvent": map[string]any{
			"toolUseId": "toolu_long",
			"name":      shortName,
			"input":     `{"path":"/tmp/a.txt"}`,
			"stop":      true,
		},
	}))

	var out bytes.Buffer
	_, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 1, KiroRequestContext{
		ToolNameMap: map[string]string{shortName: longName},
	})
	require.NoError(t, err)
	require.Contains(t, out.String(), `"name":"`+longName+`"`)
	require.NotContains(t, out.String(), `"name":"`+shortName+`"`)
}

func TestKiroCacheEmulationUsageInjectedIntoNonStreamingResponse(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "messageMetadataEvent", map[string]any{
		"messageMetadataEvent": map[string]any{
			"tokenUsage": map[string]any{
				"uncachedInputTokens": 120,
				"outputTokens":        7,
			},
		},
	}))
	result, err := ParseNonStreamingEventStreamWithContext(stream, "claude-sonnet-4-5", KiroRequestContext{
		CacheEmulationUsage: &Usage{
			InputTokens:                20,
			CacheReadInputTokens:       70,
			CacheCreationInputTokens:   30,
			CacheCreation5mInputTokens: 30,
		},
	})
	require.NoError(t, err)
	require.Equal(t, 20, result.Usage.InputTokens)
	require.Equal(t, 70, result.Usage.CacheReadInputTokens)
	require.Equal(t, 30, result.Usage.CacheCreationInputTokens)
	require.Equal(t, 20, int(gjson.GetBytes(result.ResponseBody, "usage.input_tokens").Int()))
	require.Equal(t, 70, int(gjson.GetBytes(result.ResponseBody, "usage.cache_read_input_tokens").Int()))
	require.Equal(t, 30, int(gjson.GetBytes(result.ResponseBody, "usage.cache_creation_input_tokens").Int()))
	require.Equal(t, 30, int(gjson.GetBytes(result.ResponseBody, "usage.cache_creation.ephemeral_5m_input_tokens").Int()))
}

func TestKiroCacheEmulationUsageInjectedIntoStreamAndResult(t *testing.T) {
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "messageMetadataEvent", map[string]any{
		"messageMetadataEvent": map[string]any{
			"tokenUsage": map[string]any{
				"uncachedInputTokens": 120,
				"outputTokens":        7,
			},
		},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{"content": "hello"},
	}))
	var out bytes.Buffer
	result, err := StreamEventStreamAsAnthropicWithContext(context.Background(), stream, &out, "claude-sonnet-4-5", 120, KiroRequestContext{
		CacheEmulationUsage: &Usage{
			InputTokens:                20,
			CacheReadInputTokens:       70,
			CacheCreationInputTokens:   30,
			CacheCreation1hInputTokens: 30,
		},
	})
	require.NoError(t, err)
	require.Equal(t, 20, result.Usage.InputTokens)
	require.Equal(t, 70, result.Usage.CacheReadInputTokens)
	require.Equal(t, 30, result.Usage.CacheCreationInputTokens)
	output := out.String()
	require.Contains(t, output, `"input_tokens":20`)
	require.Contains(t, output, `"cache_read_input_tokens":70`)
	require.Contains(t, output, `"cache_creation_input_tokens":30`)
	require.Contains(t, output, `"ephemeral_1h_input_tokens":30`)
}

func TestRepairJSONKeepsStringBracesWhileRepairingTrailingComma(t *testing.T) {
	raw := `{"key":"value with {nested}",}`
	repaired := repairJSON(raw)

	var parsed map[string]string
	require.NoError(t, json.Unmarshal([]byte(repaired), &parsed))
	require.Equal(t, "value with {nested}", parsed["key"])
}

func TestMapModel_MatchesKiroReferenceMapping(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"claude-opus-4-7":                     "claude-opus-4.7",
		"claude-opus-4-7-thinking":            "claude-opus-4.7",
		"claude-opus-4.7":                     "claude-opus-4.7",
		"claude-sonnet-4-6":                   "claude-sonnet-4.6",
		"claude-sonnet-4-6-thinking":          "claude-sonnet-4.6",
		"claude-sonnet-4.6":                   "claude-sonnet-4.6",
		"claude-sonnet-4-5-20250929":          "claude-sonnet-4.5",
		"claude-sonnet-4-5-20250929-thinking": "claude-sonnet-4.5",
		"claude-sonnet-4.5":                   "claude-sonnet-4.5",
		"claude-opus-4-6":                     "claude-opus-4.6",
		"claude-opus-4-6-thinking":            "claude-opus-4.6",
		"claude-opus-4.6":                     "claude-opus-4.6",
		"claude-opus-4-5-20251101":            "claude-opus-4.5",
		"claude-opus-4-5-20251101-thinking":   "claude-opus-4.5",
		"claude-opus-4.5":                     "claude-opus-4.5",
		"claude-haiku-4-5-20251001":           "claude-haiku-4.5",
		"claude-haiku-4-5-20251001-thinking":  "claude-haiku-4.5",
		"claude-haiku-4.5":                    "claude-haiku-4.5",
	}

	for input, want := range cases {
		if got := MapModel(input); got != want {
			t.Fatalf("MapModel(%q) = %q, want %q", input, got, want)
		}
	}

	rejected := []string{
		"claude-sonnet-4-6-chat",
		" claude-sonnet-4-6-thinking-chat ",
		"claude-sonnet-4-6-agentic",
		" claude-sonnet-4-6-thinking-agentic ",
		"claude-3-5-sonnet-20241022",
		"claude-opus-4-20250514",
		"claude-sonnet-4",
		"claude-opus-4-5",
		"claude-sonnet-4-5",
		"claude-haiku-4-5",
	}
	for _, input := range rejected {
		if got := MapModel(input); got != "" {
			t.Fatalf("MapModel(%q) = %q, want empty", input, got)
		}
	}
}

func TestMapModel_ReturnsEmptyForUnsupportedModels(t *testing.T) {
	t.Parallel()

	cases := []string{
		"auto",
		"gpt-4",
		"gpt-4o",
		"deepseek-3-2",
		"minimax-m2-1",
		"qwen3-coder-next",
	}

	for _, input := range cases {
		if got := MapModel(input); got != "" {
			t.Fatalf("MapModel(%q) = %q, want empty string", input, got)
		}
	}
}

func TestParseNonStreamingEventStreamEstimatesOutputTokensWhenMissing(t *testing.T) {
	// Kiro sometimes omits outputTokens; output should be estimated from response text.
	stream := bytes.NewBuffer(nil)
	_, _ = stream.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{
			"content": "hello world",
		},
	}))
	_, _ = stream.Write(buildEventStreamFrame(t, "messageMetadataEvent", map[string]any{
		"messageMetadataEvent": map[string]any{
			"tokenUsage": map[string]any{
				"uncachedInputTokens": 10,
				"totalTokens":         15,
				// outputTokens intentionally absent
			},
		},
	}))

	result, err := ParseNonStreamingEventStreamWithContext(stream, "claude-sonnet-4-5", KiroRequestContext{})
	require.NoError(t, err)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Greater(t, result.Usage.OutputTokens, 0, "should estimate outputTokens from response text")
}

func TestStreamEventStreamAsAnthropicEstimatesOutputTokensWhenMissing(t *testing.T) {
	// Kiro sometimes omits outputTokens; output should be estimated from streamed text.
	pr, pw := io.Pipe()
	var out bytes.Buffer
	errCh := make(chan error, 1)

	go func() {
		_, err := StreamEventStreamAsAnthropicWithContext(context.Background(), pr, &out, "claude-sonnet-4-5", 10, KiroRequestContext{})
		errCh <- err
	}()

	_, _ = pw.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{"content": "hello world"},
	}))
	_, _ = pw.Write(buildEventStreamFrame(t, "messageMetadataEvent", map[string]any{
		"messageMetadataEvent": map[string]any{
			"tokenUsage": map[string]any{
				"uncachedInputTokens": 10,
				"totalTokens":         16,
				// outputTokens intentionally absent
			},
		},
	}))
	require.NoError(t, pw.Close())
	require.NoError(t, <-errCh)

	output := out.String()
	// message_delta should have output_tokens > 0 (estimated from "hello world")
	require.Contains(t, output, "event: message_delta", "message_delta should be present")
	deltaIdx := strings.Index(output, "event: message_delta")
	deltaSection := output[deltaIdx:]
	require.NotContains(t, deltaSection, `"output_tokens":0`, "message_delta output_tokens should not be 0")
	require.Contains(t, deltaSection, `"output_tokens":`, "output_tokens should be present in message_delta")
}

func TestStreamEventStreamAsAnthropicStreamingToolInputCountsOutputTokens(t *testing.T) {
	// Streaming tool input fragments should be counted toward output_tokens estimation.
	pr, pw := io.Pipe()
	var out bytes.Buffer
	errCh := make(chan error, 1)

	go func() {
		_, err := StreamEventStreamAsAnthropicWithContext(context.Background(), pr, &out, "claude-sonnet-4-5", 10, KiroRequestContext{})
		errCh <- err
	}()

	_, _ = pw.Write(buildEventStreamFrame(t, "toolUseEvent", map[string]any{
		"toolUseEvent": map[string]any{
			"toolUseId": "toolu_01",
			"name":      "bash",
			"input":     `{"command": "echo hello world"}`,
			"stop":      true,
		},
	}))
	// No outputTokens in metadata
	_, _ = pw.Write(buildEventStreamFrame(t, "messageMetadataEvent", map[string]any{
		"messageMetadataEvent": map[string]any{
			"tokenUsage": map[string]any{
				"uncachedInputTokens": 10,
			},
		},
	}))
	require.NoError(t, pw.Close())
	require.NoError(t, <-errCh)

	output := out.String()
	deltaIdx := strings.Index(output, "event: message_delta")
	require.GreaterOrEqual(t, deltaIdx, 0, "message_delta should be present")
	deltaSection := output[deltaIdx:]
	require.NotContains(t, deltaSection, `"output_tokens":0`, "streaming tool input should contribute to output_tokens")
	require.Contains(t, deltaSection, `"output_tokens":`, "output_tokens should be present in message_delta")
}

func TestStreamEventStreamAsAnthropicUpstreamOutputTokensNotOverridden(t *testing.T) {
	// When upstream provides real outputTokens, estimation must not override it.
	pr, pw := io.Pipe()
	var out bytes.Buffer
	errCh := make(chan error, 1)

	go func() {
		_, err := StreamEventStreamAsAnthropicWithContext(context.Background(), pr, &out, "claude-sonnet-4-5", 10, KiroRequestContext{})
		errCh <- err
	}()

	_, _ = pw.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{
		"assistantResponseEvent": map[string]any{"content": "hi"},
	}))
	_, _ = pw.Write(buildEventStreamFrame(t, "messageMetadataEvent", map[string]any{
		"messageMetadataEvent": map[string]any{
			"tokenUsage": map[string]any{
				"uncachedInputTokens": 10,
				"outputTokens":        42,
				"totalTokens":         52,
			},
		},
	}))
	require.NoError(t, pw.Close())
	require.NoError(t, <-errCh)

	output := out.String()
	deltaIdx := strings.Index(output, "event: message_delta")
	require.GreaterOrEqual(t, deltaIdx, 0)
	deltaSection := output[deltaIdx:]
	require.Contains(t, deltaSection, `"output_tokens":42`, "upstream outputTokens should not be overridden by estimation")
}

func buildEventStreamFrame(t *testing.T, eventType string, payload any) []byte {
	t.Helper()
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	headers := bytes.NewBuffer(nil)
	_ = headers.WriteByte(byte(len(":event-type")))
	_, _ = headers.WriteString(":event-type")
	_ = headers.WriteByte(7)
	require.NoError(t, binary.Write(headers, binary.BigEndian, uint16(len(eventType))))
	_, _ = headers.WriteString(eventType)

	totalLength := uint32(12 + headers.Len() + len(payloadBytes) + 4)
	frame := bytes.NewBuffer(nil)
	require.NoError(t, binary.Write(frame, binary.BigEndian, totalLength))
	require.NoError(t, binary.Write(frame, binary.BigEndian, uint32(headers.Len())))
	require.NoError(t, binary.Write(frame, binary.BigEndian, uint32(0)))
	_, _ = frame.Write(headers.Bytes())
	_, _ = frame.Write(payloadBytes)
	require.NoError(t, binary.Write(frame, binary.BigEndian, uint32(0)))
	return frame.Bytes()
}
