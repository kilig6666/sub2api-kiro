package windsurf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	WindsurfSigninURL       = "https://windsurf.com/windsurf/signin"
	WindsurfClientID        = "3GUryQ7ldAeKEuD2obYnppsnmj58eP5u"
	WindsurfRedirectURI     = "http://localhost:8086/callback"
	auth1PasswordLoginURL   = "https://windsurf.com/_devin-auth/password/login"
	windsurfPostAuthURL     = "https://windsurf.com/_backend/exa.seat_management_pb.SeatManagementService/WindsurfPostAuth"
	windsurfPostAuthLegacy  = "https://server.self-serve.windsurf.com/exa.seat_management_pb.SeatManagementService/WindsurfPostAuth"
	windsurfRegisterUserURL = "https://register.windsurf.com/exa.seat_management_pb.SeatManagementService/RegisterUser"
	windsurfRegisterLegacy  = "https://api.codeium.com/register_user/"
)

// WindsurfTokenInfo 认证结果
type WindsurfTokenInfo struct {
	APIKey     string `json:"api_key"`
	Email      string `json:"email,omitempty"`
	AuthMethod string `json:"auth_method"`
	AccountID  string `json:"account_id,omitempty"`
	PlanName   string `json:"plan_name,omitempty"`
}

// ImportAPIKey 直接导入 API Key
func ImportAPIKey(apiKey string) (*WindsurfTokenInfo, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("windsurf api_key is required")
	}
	return &WindsurfTokenInfo{
		APIKey:     apiKey,
		AuthMethod: "api_key",
	}, nil
}

// LoginWithPassword 使用邮箱密码登录获取 session token
func LoginWithPassword(ctx context.Context, email, password, proxyURL string) (*WindsurfTokenInfo, error) {
	email = strings.TrimSpace(email)
	password = strings.TrimSpace(password)
	if email == "" || password == "" {
		return nil, fmt.Errorf("windsurf email and password are required")
	}

	client := oauthHTTPClient(proxyURL)

	// Step 1: POST password login → auth1 token
	loginBody := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
	loginReq, err := http.NewRequestWithContext(ctx, http.MethodPost, auth1PasswordLoginURL, strings.NewReader(loginBody))
	if err != nil {
		return nil, fmt.Errorf("build login request: %w", err)
	}
	loginReq.Header.Set("content-type", "application/json")
	loginReq.Header.Set("accept", "application/json")
	loginReq.Header.Set("origin", "https://windsurf.com")

	loginResp, err := client.Do(loginReq)
	if err != nil {
		return nil, fmt.Errorf("login request failed: %w", err)
	}
	defer loginResp.Body.Close()
	loginRespBody, _ := io.ReadAll(loginResp.Body)

	if loginResp.StatusCode < 200 || loginResp.StatusCode >= 300 {
		return nil, fmt.Errorf("login failed HTTP %d: %s", loginResp.StatusCode, truncateBytes(loginRespBody, 200))
	}

	var loginPayload map[string]any
	if err := json.Unmarshal(loginRespBody, &loginPayload); err != nil {
		return nil, fmt.Errorf("login response not json: %w", err)
	}
	auth1Token := jsonString(loginPayload, "token", "access_token", "accessToken")
	if auth1Token == "" {
		return nil, fmt.Errorf("login response missing token")
	}

	// Step 2: POST WindsurfPostAuth → session token
	for _, url := range []string{windsurfPostAuthURL, windsurfPostAuthLegacy} {
		postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(""))
		if err != nil {
			continue
		}
		postReq.Header.Set("content-type", "application/proto")
		postReq.Header.Set("connect-protocol-version", "1")
		postReq.Header.Set("x-devin-auth1-token", auth1Token)

		postResp, err := client.Do(postReq)
		if err != nil {
			continue
		}
		postRespBody, _ := io.ReadAll(postResp.Body)
		postResp.Body.Close()

		if postResp.StatusCode < 200 || postResp.StatusCode >= 300 {
			continue
		}

		sessionToken := extractSessionToken(postRespBody)
		if sessionToken != "" {
			return &WindsurfTokenInfo{
				APIKey:     sessionToken,
				Email:      email,
				AuthMethod: "email_password",
			}, nil
		}
	}
	return nil, fmt.Errorf("WindsurfPostAuth failed: could not obtain session token")
}

// RegisterWithToken 使用 Firebase ID Token 注册获取 API Key
func RegisterWithToken(ctx context.Context, token, proxyURL string) (*WindsurfTokenInfo, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("windsurf token is required")
	}

	client := oauthHTTPClient(proxyURL)
	reqBody := fmt.Sprintf(`{"firebase_id_token":%q}`, token)

	for _, url := range []string{windsurfRegisterUserURL, windsurfRegisterLegacy} {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(reqBody))
		if err != nil {
			continue
		}
		req.Header.Set("content-type", "application/json")
		req.Header.Set("accept", "application/json")
		req.Header.Set("connect-protocol-version", "1")

		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			continue
		}

		var payload map[string]any
		if err := json.Unmarshal(respBody, &payload); err != nil {
			continue
		}
		apiKey := jsonString(payload, "api_key", "apiKey")
		if apiKey != "" {
			return &WindsurfTokenInfo{
				APIKey:     apiKey,
				Email:      jsonString(payload, "email"),
				AuthMethod: "token",
				AccountID:  jsonString(payload, "account_id", "accountId", "user_id", "userId"),
				PlanName:   jsonString(payload, "plan_name", "planName", "plan"),
			}, nil
		}
	}
	return nil, fmt.Errorf("RegisterUser failed: could not obtain api_key")
}

// BuildBrowserOAuthURL 构建浏览器 OAuth 登录 URL
func BuildBrowserOAuthURL(state string) string {
	return fmt.Sprintf("%s?response_type=token&client_id=%s&redirect_uri=%s&state=%s&prompt=login&redirect_parameters_type=query&workflow=",
		WindsurfSigninURL, WindsurfClientID, WindsurfRedirectURI, state)
}

func oauthHTTPClient(proxyURL string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// proxyURL 暂时保留接口，后续可接入代理配置
	_ = proxyURL
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}
}

func extractSessionToken(body []byte) string {
	// 尝试 JSON 解析
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err == nil {
		if token := jsonString(payload, "sessionToken", "session_token", "api_key", "apiKey"); token != "" {
			return token
		}
	}
	// 尝试 protobuf 解析（WindsurfPostAuth 可能返回 proto）
	fields, err := ParseFields(body)
	if err == nil {
		if s, ok := GetString(fields, 1); ok && s != "" {
			return s
		}
	}
	return ""
}

func jsonString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}
