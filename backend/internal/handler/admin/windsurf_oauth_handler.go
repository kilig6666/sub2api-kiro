package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// WindsurfOAuthHandler 处理 Windsurf 账号认证相关请求
type WindsurfOAuthHandler struct {
	windsurfOAuthService *service.WindsurfOAuthService
}

// NewWindsurfOAuthHandler 创建 Windsurf OAuth Handler
func NewWindsurfOAuthHandler(windsurfOAuthService *service.WindsurfOAuthService) *WindsurfOAuthHandler {
	return &WindsurfOAuthHandler{windsurfOAuthService: windsurfOAuthService}
}

type WindsurfImportAPIKeyRequest struct {
	Name   string `json:"name"`
	APIKey string `json:"api_key" binding:"required"`
}

// ImportAPIKey 导入 API Key
func (h *WindsurfOAuthHandler) ImportAPIKey(c *gin.Context) {
	var req WindsurfImportAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求无效: "+err.Error())
		return
	}
	creds, err := h.windsurfOAuthService.ImportAPIKey(&service.WindsurfImportAPIKeyInput{
		Name:   req.Name,
		APIKey: req.APIKey,
	})
	if err != nil {
		response.BadRequest(c, "导入 API Key 失败: "+err.Error())
		return
	}
	response.Success(c, creds)
}

type WindsurfPasswordLoginRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
	ProxyURL string `json:"proxy_url"`
}

// LoginWithPassword 邮箱密码登录
func (h *WindsurfOAuthHandler) LoginWithPassword(c *gin.Context) {
	var req WindsurfPasswordLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求无效: "+err.Error())
		return
	}
	creds, err := h.windsurfOAuthService.LoginWithPassword(c.Request.Context(), &service.WindsurfPasswordLoginInput{
		Name:     req.Name,
		Email:    req.Email,
		Password: req.Password,
		ProxyURL: req.ProxyURL,
	})
	if err != nil {
		response.BadRequest(c, "登录失败: "+err.Error())
		return
	}
	response.Success(c, creds)
}

type WindsurfTokenImportRequest struct {
	Name     string `json:"name"`
	Token    string `json:"token" binding:"required"`
	ProxyURL string `json:"proxy_url"`
}

// ImportToken 导入 Token
func (h *WindsurfOAuthHandler) ImportToken(c *gin.Context) {
	var req WindsurfTokenImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求无效: "+err.Error())
		return
	}
	creds, err := h.windsurfOAuthService.RegisterWithToken(c.Request.Context(), &service.WindsurfTokenImportInput{
		Name:     req.Name,
		Token:    req.Token,
		ProxyURL: req.ProxyURL,
	})
	if err != nil {
		response.BadRequest(c, "Token 导入失败: "+err.Error())
		return
	}
	response.Success(c, creds)
}

type WindsurfOAuthURLRequest struct {
	State string `json:"state" binding:"required"`
}

// GenerateOAuthURL 生成浏览器 OAuth URL
func (h *WindsurfOAuthHandler) GenerateOAuthURL(c *gin.Context) {
	var req WindsurfOAuthURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求无效: "+err.Error())
		return
	}
	result, err := h.windsurfOAuthService.GenerateBrowserOAuthURL(&service.WindsurfOAuthURLInput{
		State: req.State,
	})
	if err != nil {
		response.BadRequest(c, "生成 OAuth URL 失败: "+err.Error())
		return
	}
	response.Success(c, result)
}
