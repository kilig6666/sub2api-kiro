package service

import (
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/windsurf"
	"github.com/gin-gonic/gin"
)

func (s *AccountTestService) testWindsurfAccountConnection(c *gin.Context, account *Account, modelID string) error {
	if s.windsurfRuntime == nil {
		return s.sendErrorAndEnd(c, "Windsurf runtime not configured")
	}
	if s.windsurfRuntime.ResolveBinaryPath() == "" {
		return s.sendErrorAndEnd(c, "Windsurf language server binary not found")
	}

	apiKey := account.GetCredential("api_key")
	if apiKey == "" {
		return s.sendErrorAndEnd(c, "Windsurf account missing api_key credential")
	}

	testModelID := strings.TrimSpace(modelID)
	if testModelID == "" {
		testModelID = "claude-sonnet-4.6"
	}
	mappedModel := testModelID
	if m := account.GetMappedModel(testModelID); m != "" {
		mappedModel = m
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	s.sendEvent(c, TestEvent{Type: "test_start", Model: testModelID})

	ctx := c.Request.Context()
	events := make(chan windsurf.PollEvent, 64)

	var result *windsurf.CascadeResult
	var execErr error

	go func() {
		defer close(events)
		result, execErr = s.windsurfRuntime.ExecuteStream(
			ctx, apiKey, mappedModel, "Hello, please respond with a short greeting.", nil, events,
		)
	}()

	for event := range events {
		switch event.Type {
		case windsurf.PollEventTextDelta:
			s.sendEvent(c, TestEvent{Type: "content", Text: event.TextDelta})
		case windsurf.PollEventHeartbeat:
			fmt.Fprintf(c.Writer, ": ping\n\n")
			c.Writer.Flush()
		}
	}

	if execErr != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("Cascade execution failed: %s", execErr.Error()))
	}

	_ = result
	s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
	return nil
}
