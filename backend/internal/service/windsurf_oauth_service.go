package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/windsurf"
)

// WindsurfOAuthService 管理 Windsurf 账号认证
type WindsurfOAuthService struct{}

// NewWindsurfOAuthService 创建 Windsurf OAuth 服务
func NewWindsurfOAuthService() *WindsurfOAuthService {
	return &WindsurfOAuthService{}
}

// WindsurfImportAPIKeyInput API Key 导入输入
type WindsurfImportAPIKeyInput struct {
	Name   string `json:"name"`
	APIKey string `json:"api_key"`
}

// WindsurfPasswordLoginInput 邮箱密码登录输入
type WindsurfPasswordLoginInput struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
	ProxyURL string `json:"proxy_url,omitempty"`
}

// WindsurfTokenImportInput Token 导入输入
type WindsurfTokenImportInput struct {
	Name     string `json:"name"`
	Token    string `json:"token"`
	ProxyURL string `json:"proxy_url,omitempty"`
}

// WindsurfOAuthURLInput 浏览器 OAuth URL 输入
type WindsurfOAuthURLInput struct {
	State string `json:"state"`
}

// WindsurfOAuthURLResult OAuth URL 结果
type WindsurfOAuthURLResult struct {
	AuthorizeURL string `json:"authorize_url"`
	State        string `json:"state"`
}

// ImportAPIKey 导入 API Key 创建账号凭证
func (s *WindsurfOAuthService) ImportAPIKey(input *WindsurfImportAPIKeyInput) (map[string]any, error) {
	info, err := windsurf.ImportAPIKey(input.APIKey)
	if err != nil {
		return nil, err
	}
	return buildWindsurfCredentials(info, input.Name), nil
}

// LoginWithPassword 使用邮箱密码登录
func (s *WindsurfOAuthService) LoginWithPassword(ctx context.Context, input *WindsurfPasswordLoginInput) (map[string]any, error) {
	info, err := windsurf.LoginWithPassword(ctx, input.Email, input.Password, input.ProxyURL)
	if err != nil {
		return nil, err
	}
	return buildWindsurfCredentials(info, input.Name), nil
}

// RegisterWithToken 使用 Token 注册
func (s *WindsurfOAuthService) RegisterWithToken(ctx context.Context, input *WindsurfTokenImportInput) (map[string]any, error) {
	info, err := windsurf.RegisterWithToken(ctx, input.Token, input.ProxyURL)
	if err != nil {
		return nil, err
	}
	return buildWindsurfCredentials(info, input.Name), nil
}

// GenerateBrowserOAuthURL 生成浏览器 OAuth URL
func (s *WindsurfOAuthService) GenerateBrowserOAuthURL(input *WindsurfOAuthURLInput) (*WindsurfOAuthURLResult, error) {
	if input.State == "" {
		return nil, fmt.Errorf("state is required")
	}
	url := windsurf.BuildBrowserOAuthURL(input.State)
	return &WindsurfOAuthURLResult{
		AuthorizeURL: url,
		State:        input.State,
	}, nil
}

func buildWindsurfCredentials(info *windsurf.WindsurfTokenInfo, name string) map[string]any {
	creds := map[string]any{
		"api_key":     info.APIKey,
		"auth_method": info.AuthMethod,
		"updated_at":  time.Now().Unix(),
	}
	if info.Email != "" {
		creds["email"] = info.Email
	}
	if info.AccountID != "" {
		creds["account_id"] = info.AccountID
	}
	if info.PlanName != "" {
		creds["plan_name"] = info.PlanName
	}
	if name != "" {
		creds["name"] = name
	}
	return creds
}
