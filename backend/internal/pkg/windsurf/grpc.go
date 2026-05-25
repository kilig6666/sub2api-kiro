package windsurf

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/http2"
)

// GRPCUnary 向本地 Windsurf Language Server 发送 gRPC unary 请求
func GRPCUnary(ctx context.Context, port uint16, csrfToken, method string, payload []byte, timeout time.Duration) ([]byte, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d%s/%s", port, LSServicePath, method)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body := GRPCFrame(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, io.NopCloser(bytesReader(body)))
	if err != nil {
		return nil, fmt.Errorf("windsurf gRPC %s: build request: %w", method, err)
	}
	req.Header.Set("content-type", "application/grpc")
	req.Header.Set("te", "trailers")
	req.Header.Set("user-agent", "grpc-node/1.108.2")
	req.Header.Set("x-codeium-csrf-token", csrfToken)
	req.ContentLength = int64(len(body))

	client := h2cClient(timeout)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("windsurf gRPC %s: request failed: %w", method, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("windsurf gRPC %s: read response: %w", method, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("windsurf gRPC %s: HTTP %d: %s", method, resp.StatusCode, truncateBytes(respBody, 200))
	}

	frames := ExtractGRPCFrames(respBody)
	if len(frames) == 0 {
		return respBody, nil
	}
	var result []byte
	for _, f := range frames {
		result = append(result, f...)
	}
	return result, nil
}

func h2cClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		},
	}
}

type bytesReaderWrapper struct {
	data []byte
	pos  int
}

func bytesReader(data []byte) *bytesReaderWrapper {
	return &bytesReaderWrapper{data: data}
}

func (r *bytesReaderWrapper) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func truncateBytes(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}
