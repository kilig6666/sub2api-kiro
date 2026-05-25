package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/pkg/windsurf"
)

// WindsurfRuntime 管理 Windsurf Cascade 执行环境
type WindsurfRuntime struct {
	pool       *windsurf.LSPool
	binaryPath string
	once       sync.Once
}

// NewWindsurfRuntime 创建 Windsurf 运行时
func NewWindsurfRuntime() *WindsurfRuntime {
	return &WindsurfRuntime{
		pool: windsurf.NewLSPool(),
	}
}

// ResolveBinaryPath 查找 Windsurf LS 二进制路径
func (r *WindsurfRuntime) ResolveBinaryPath() string {
	r.once.Do(func() {
		r.binaryPath = resolveWindsurfBinary()
	})
	return r.binaryPath
}

// Execute 执行 Windsurf Cascade（同步模式）
func (r *WindsurfRuntime) Execute(ctx context.Context, apiKey, model, message string, options *windsurf.SendCascadeMessageOptions) (*windsurf.CascadeResult, error) {
	binary := r.ResolveBinaryPath()
	if binary == "" {
		return nil, fmt.Errorf("windsurf language server binary not found")
	}
	executor := &windsurf.CascadeExecutor{
		Pool:       r.pool,
		BinaryPath: binary,
	}
	workspacePath := defaultWorkspacePath(apiKey)
	return executor.Execute(ctx, apiKey, model, message, workspacePath, options)
}

// ExecuteStream 执行 Windsurf Cascade（流式模式）
func (r *WindsurfRuntime) ExecuteStream(ctx context.Context, apiKey, model, message string, options *windsurf.SendCascadeMessageOptions, events chan<- windsurf.PollEvent) (*windsurf.CascadeResult, error) {
	binary := r.ResolveBinaryPath()
	if binary == "" {
		return nil, fmt.Errorf("windsurf language server binary not found")
	}
	executor := &windsurf.CascadeExecutor{
		Pool:       r.pool,
		BinaryPath: binary,
	}
	workspacePath := defaultWorkspacePath(apiKey)
	return executor.ExecuteStream(ctx, apiKey, model, message, workspacePath, options, events)
}

// Shutdown 关闭所有 LS 进程
func (r *WindsurfRuntime) Shutdown() {
	if r.pool != nil {
		r.pool.Shutdown()
	}
}

func defaultWorkspacePath(apiKey string) string {
	poolKey := windsurf.PoolKeyFromAPIKey(apiKey)
	return filepath.Join(os.TempDir(), "windsurf-ws", poolKey)
}

func resolveWindsurfBinary() string {
	// 优先使用环境变量
	if path := os.Getenv("WINDSURF_LS_BINARY"); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	// 检查常见路径
	candidates := []string{
		"/usr/local/bin/windsurf-language-server",
		"/opt/windsurf/language-server",
		filepath.Join(os.Getenv("HOME"), ".windsurf", "language-server"),
		filepath.Join(os.Getenv("HOME"), ".codeium", "bin", "language_server"),
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}
