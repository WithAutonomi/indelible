package antd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/WithAutonomi/indelible/internal/config"

	antdsdk "github.com/WithAutonomi/ant-sdk/antd-go"
)

const (
	discoverPollInterval = 500 * time.Millisecond
	discoverTimeout      = 30 * time.Second
	healthRetries        = 5
	healthRetryDelay     = time.Second
	stopTimeout          = 10 * time.Second
	maxRestarts          = 3
)

// Manager spawns, monitors, and stops an antd child process.
type Manager struct {
	cfg    *config.Config
	cmd    *exec.Cmd
	url    string
	pid    int
	cancel context.CancelFunc
	done   chan struct{} // closed when process finally exits
	mu     sync.Mutex
}

// NewManager creates a new antd process manager.
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		cfg:  cfg,
		done: make(chan struct{}),
	}
}

// Start launches antd or attaches to an already-running instance.
func (m *Manager) Start(ctx context.Context) error {
	// Check if antd is already reachable via port file
	if url := antdsdk.DiscoverDaemonURL(); url != "" {
		if healthCheck(url) {
			m.url = url
			slog.Info("using existing antd", "url", url)
			return nil
		}
	}

	// Find the binary
	binPath, err := exec.LookPath(m.cfg.AntdBin)
	if err != nil {
		return fmt.Errorf("antd binary not found; install antd or set INDELIBLE_ANTD_BIN: %w", err)
	}

	mctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	if err := m.spawn(mctx, binPath); err != nil {
		cancel()
		return err
	}

	// Wait for port file discovery
	if err := m.waitForDiscovery(); err != nil {
		m.kill()
		cancel()
		return err
	}

	// Health check
	if err := m.waitForHealth(); err != nil {
		m.kill()
		cancel()
		return err
	}

	// Monitor goroutine — restart on unexpected exit
	go m.monitor(mctx, binPath)

	return nil
}

// spawn starts the antd process.
func (m *Manager) spawn(ctx context.Context, binPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	args := []string{
		"--rest-port", "0",
		"--grpc-port", "0",
	}

	m.cmd = exec.CommandContext(ctx, binPath, args...)

	// Pipe stdout/stderr through slog
	stdout, err := m.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("antd stdout pipe: %w", err)
	}
	stderr, err := m.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("antd stderr pipe: %w", err)
	}

	if err := m.cmd.Start(); err != nil {
		return fmt.Errorf("starting antd: %w", err)
	}

	m.pid = m.cmd.Process.Pid
	slog.Info("antd started", "pid", m.pid, "bin", binPath)

	go pipeLog(stdout, "stdout")
	go pipeLog(stderr, "stderr")

	return nil
}

// pipeLog reads lines from r and logs them with [antd] prefix.
func pipeLog(r io.Reader, stream string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		slog.Info("[antd] "+scanner.Text(), "stream", stream)
	}
}

// waitForDiscovery polls DiscoverDaemonURL until a URL is found or timeout.
func (m *Manager) waitForDiscovery() error {
	deadline := time.Now().Add(discoverTimeout)
	for time.Now().Before(deadline) {
		if url := antdsdk.DiscoverDaemonURL(); url != "" {
			m.url = url
			slog.Info("antd port discovered", "url", url)
			return nil
		}
		time.Sleep(discoverPollInterval)
	}
	return fmt.Errorf("antd did not write port file within %s", discoverTimeout)
}

// waitForHealth retries GET /health on the discovered URL.
func (m *Manager) waitForHealth() error {
	for i := range healthRetries {
		if healthCheck(m.url) {
			return nil
		}
		slog.Debug("antd health check failed, retrying", "attempt", i+1)
		time.Sleep(healthRetryDelay)
	}
	return fmt.Errorf("antd at %s failed health check after %d retries", m.url, healthRetries)
}

// healthCheck does a single GET /health and returns true on 2xx.
func healthCheck(baseURL string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(baseURL + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// monitor watches the process and restarts on unexpected exit.
func (m *Manager) monitor(ctx context.Context, binPath string) {
	defer close(m.done)

	restarts := 0
	for {
		err := m.cmd.Wait()
		if ctx.Err() != nil {
			// Context cancelled — intentional shutdown
			slog.Info("antd process stopped (managed shutdown)")
			return
		}

		slog.Error("antd exited unexpectedly", "error", err, "restarts", restarts)
		if restarts >= maxRestarts {
			slog.Error("antd max restarts reached, giving up", "max", maxRestarts)
			return
		}

		restarts++
		slog.Info("restarting antd", "attempt", restarts)

		if spawnErr := m.spawn(ctx, binPath); spawnErr != nil {
			slog.Error("failed to restart antd", "error", spawnErr)
			return
		}

		// Re-discover after restart
		if discErr := m.waitForDiscovery(); discErr != nil {
			slog.Error("antd restart: port discovery failed", "error", discErr)
			m.kill()
			return
		}
	}
}

// Stop gracefully shuts down the managed antd process.
func (m *Manager) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}

	m.mu.Lock()
	proc := m.cmd
	m.mu.Unlock()

	if proc == nil || proc.Process == nil {
		return nil
	}

	// Try interrupt first
	if err := proc.Process.Signal(os.Interrupt); err != nil {
		// On Windows os.Interrupt may not be supported; fall through to kill
		slog.Debug("antd interrupt failed, will force kill", "error", err)
	}

	// Wait for exit or timeout
	select {
	case <-m.done:
		slog.Info("antd stopped gracefully")
		return nil
	case <-time.After(stopTimeout):
		slog.Warn("antd did not stop in time, force killing")
		if err := proc.Process.Kill(); err != nil {
			return fmt.Errorf("killing antd: %w", err)
		}
		<-m.done
		return nil
	}
}

// kill forcefully terminates the antd process.
func (m *Manager) kill() {
	m.mu.Lock()
	proc := m.cmd
	m.mu.Unlock()

	if proc != nil && proc.Process != nil {
		_ = proc.Process.Kill()
	}
}

// URL returns the discovered antd REST URL.
func (m *Manager) URL() string {
	return m.url
}

// PID returns the process ID of the managed antd, or 0 if not running.
func (m *Manager) PID() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pid
}

// Running returns whether the managed process is still alive.
func (m *Manager) Running() bool {
	select {
	case <-m.done:
		return false
	default:
		return m.pid != 0
	}
}
