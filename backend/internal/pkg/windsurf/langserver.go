package windsurf

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	defaultLSPort    = 42100
	lsReadyTimeout   = 25 * time.Second
	lsReadyPollDelay = 200 * time.Millisecond
)

// LSHandle 表示一个可用的 Language Server 连接句柄
type LSHandle struct {
	PoolKey       string
	Port          uint16
	CSRFToken     string
	SessionID     string
	WorkspacePath string
}

type lsProcessEntry struct {
	port          uint16
	csrfToken     string
	sessionID     string
	workspacePath string
	cmd           *exec.Cmd
	startedAt     time.Time
}

// LSPool 管理 Windsurf Language Server 进程池
type LSPool struct {
	mu      sync.Mutex
	entries map[string]*lsProcessEntry
}

// NewLSPool 创建新的进程池
func NewLSPool() *LSPool {
	return &LSPool{entries: make(map[string]*lsProcessEntry)}
}

// PoolKeyFromAPIKey 从 API Key 生成进程池 key
func PoolKeyFromAPIKey(apiKey string) string {
	h := sha256.Sum256([]byte(apiKey))
	return fmt.Sprintf("%x", h[:8])
}

// Ensure 确保指定 poolKey 的 LS 进程正在运行，返回句柄
func (p *LSPool) Ensure(ctx context.Context, poolKey, binaryPath, workspacePath string) (*LSHandle, error) {
	p.mu.Lock()
	if entry, ok := p.entries[poolKey]; ok {
		if isProcessAlive(entry.cmd) && isPortReachable(entry.port) {
			handle := handleFromEntry(poolKey, entry)
			p.mu.Unlock()
			return handle, nil
		}
		// 进程已死或端口不可达，清理
		killProcess(entry.cmd)
		delete(p.entries, poolKey)
	}
	p.mu.Unlock()

	// 启动新进程
	port, err := findFreePort()
	if err != nil {
		return nil, fmt.Errorf("find free port for Windsurf LS: %w", err)
	}

	dataDir := lsDataDir(poolKey)
	if err := os.MkdirAll(filepath.Join(dataDir, "db"), 0o755); err != nil {
		return nil, fmt.Errorf("create LS data dir: %w", err)
	}
	if workspacePath != "" {
		_ = os.MkdirAll(workspacePath, 0o755)
	}

	cmd := exec.CommandContext(ctx, binaryPath,
		fmt.Sprintf("--api_server_url=%s", DefaultCodeiumAPIURL),
		fmt.Sprintf("--server_port=%d", port),
		fmt.Sprintf("--csrf_token=%s", DefaultCSRFToken),
		fmt.Sprintf("--register_user_url=%s", DefaultRegisterURL),
		fmt.Sprintf("--codeium_dir=%s", dataDir),
		fmt.Sprintf("--database_dir=%s", filepath.Join(dataDir, "db")),
		"--detect_proxy=false",
	)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Env = lsEnv()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Windsurf LS %s: %w", binaryPath, err)
	}

	if err := waitPortReady(ctx, port, lsReadyTimeout); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("Windsurf LS not ready on port %d: %w", port, err)
	}

	sessionID := uuid.New().String()
	entry := &lsProcessEntry{
		port:          port,
		csrfToken:     DefaultCSRFToken,
		sessionID:     sessionID,
		workspacePath: workspacePath,
		cmd:           cmd,
		startedAt:     time.Now(),
	}

	p.mu.Lock()
	// 双重检查：另一个 goroutine 可能已经创建了
	if existing, ok := p.entries[poolKey]; ok && isProcessAlive(existing.cmd) {
		p.mu.Unlock()
		_ = cmd.Process.Kill()
		return handleFromEntry(poolKey, existing), nil
	}
	p.entries[poolKey] = entry
	p.mu.Unlock()

	return handleFromEntry(poolKey, entry), nil
}

// Invalidate 使指定句柄失效并终止进程
func (p *LSPool) Invalidate(handle *LSHandle) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, ok := p.entries[handle.PoolKey]; ok {
		killProcess(entry.cmd)
		delete(p.entries, handle.PoolKey)
	}
}

// ResetSession 重置 session ID（不重启进程）
func (p *LSPool) ResetSession(handle *LSHandle) *LSHandle {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, ok := p.entries[handle.PoolKey]; ok {
		entry.sessionID = uuid.New().String()
		return handleFromEntry(handle.PoolKey, entry)
	}
	newHandle := *handle
	newHandle.SessionID = uuid.New().String()
	return &newHandle
}

// Shutdown 关闭所有 LS 进程
func (p *LSPool) Shutdown() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, entry := range p.entries {
		killProcess(entry.cmd)
		delete(p.entries, key)
	}
}

func handleFromEntry(poolKey string, entry *lsProcessEntry) *LSHandle {
	return &LSHandle{
		PoolKey:       poolKey,
		Port:          entry.port,
		CSRFToken:     entry.csrfToken,
		SessionID:     entry.sessionID,
		WorkspacePath: entry.workspacePath,
	}
}

func findFreePort() (uint16, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	listener.Close()
	return port, nil
}

func waitPortReady(ctx context.Context, port uint16, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if isPortReachable(port) {
			return nil
		}
		time.Sleep(lsReadyPollDelay)
	}
	return fmt.Errorf("timeout waiting for port %d", port)
}

func isPortReachable(port uint16) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func isProcessAlive(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}
	// ProcessState 非 nil 表示进程已退出
	return cmd.ProcessState == nil
}

func killProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}

func lsDataDir(poolKey string) string {
	return filepath.Join(os.TempDir(), "windsurf-ls", poolKey)
}

func lsEnv() []string {
	return []string{
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + os.TempDir(),
	}
}
