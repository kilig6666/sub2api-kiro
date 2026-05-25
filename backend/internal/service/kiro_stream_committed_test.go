package service

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestKiroStreamingErrorBeforeOutputReturnsFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &GatewayService{rateLimitService: &RateLimitService{}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: kiroSSEBody(
			"event: error\n" + `data: {"type":"error","error":{"type":"upstream_error","message":"kiro boom"}}`,
		),
		Header: http.Header{},
	}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformKiro}, time.Now(), "model", "model", false)
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Contains(t, string(failoverErr.ResponseBody), "kiro boom")
	require.False(t, c.Writer.Written())
}

func TestKiroStreamingErrorAfterOutputStaysCommitted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &GatewayService{rateLimitService: &RateLimitService{}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: kiroSSEBody(
			"event: message_start\n"+`data: {"type":"message_start","message":{"id":"msg_fixed","type":"message","role":"assistant","content":[],"model":"model","usage":{"input_tokens":1,"output_tokens":0}}}`,
			"event: content_block_start\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			"event: content_block_delta\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
			"event: error\n"+`data: {"type":"error","error":{"type":"upstream_error","message":"kiro broke after output"}}`,
		),
		Header: http.Header{},
	}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformKiro}, time.Now(), "model", "model", false)
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.True(t, c.Writer.Written())
	require.Contains(t, rec.Body.String(), `"text":"hello"`)
	require.Contains(t, rec.Body.String(), "kiro broke after output")
	require.Contains(t, rec.Body.String(), "event: error")
}

func kiroSSEBody(events ...string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(strings.Join(events, "\n\n") + "\n\n"))
}
