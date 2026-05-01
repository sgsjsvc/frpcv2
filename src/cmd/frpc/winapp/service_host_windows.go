//go:build windows

package winapp

import (
	"fmt"
	"net"
	"net/rpc"
	"os"
	"sync"
	"time"

	"github.com/fatedier/frp/cmd/frpc/runtime"
)

type runningProxy struct {
	bundle *runtime.ServiceBundle
	done   chan error
}

type serviceHost struct {
	mu            sync.Mutex
	configPath    string
	listener      net.Listener
	rpcServer     *rpc.Server
	current       *runningProxy
	lastError     string
	lastOperation string
}

func newServiceHost() *serviceHost {
	return &serviceHost{
		configPath: machineConfigPath(),
		rpcServer:  rpc.NewServer(),
	}
}

func (h *serviceHost) start() error {
	if err := ensureParentDir(h.configPath); err != nil {
		return err
	}
	if _, err := os.Stat(h.configPath); os.IsNotExist(err) {
		if err := os.WriteFile(h.configPath, runtime.DefaultClientConfigContent(), 0o644); err != nil {
			return err
		}
	}

	listener, err := newPipeListener()
	if err != nil {
		return err
	}
	h.listener = listener
	if err := h.rpcServer.RegisterName("FrpcService", &HostRPC{host: h}); err != nil {
		_ = listener.Close()
		return err
	}

	go h.acceptLoop()
	if err := h.startProxy(); err != nil {
		h.setLastError(err, "服务已启动，但自动启动代理失败")
		return nil
	}
	return nil
}

func (h *serviceHost) acceptLoop() {
	for {
		conn, err := h.listener.Accept()
		if err != nil {
			return
		}
		go servePipeConnection(h.rpcServer, conn)
	}
}

func (h *serviceHost) shutdown() {
	if h.listener != nil {
		_ = h.listener.Close()
	}
	_ = h.stopProxy(5 * time.Second)
}

func (h *serviceHost) status() StatusPayload {
	h.mu.Lock()
	defer h.mu.Unlock()
	return StatusPayload{
		ConfigPath:    h.configPath,
		ProxyRunning:  h.current != nil,
		LastError:     h.lastError,
		LastOperation: h.lastOperation,
	}
}

func (h *serviceHost) readConfig() (string, error) {
	b, err := os.ReadFile(h.configPath)
	if os.IsNotExist(err) {
		return string(runtime.DefaultClientConfigContent()), nil
	}
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (h *serviceHost) validateConfig(content string) error {
	if err := runtime.ValidateClientConfigContent(h.configPath, []byte(content), true, nil); err != nil {
		h.setLastError(err, "配置校验失败")
		return err
	}
	h.setLastOperation("配置校验通过")
	return nil
}

func (h *serviceHost) updateConfig(content string) error {
	if err := runtime.WriteValidatedClientConfig(h.configPath, []byte(content), true, nil); err != nil {
		h.setLastError(err, "保存配置失败")
		return err
	}
	h.setLastOperation("配置已保存")

	h.mu.Lock()
	running := h.current != nil
	h.mu.Unlock()
	if running {
		return h.reloadConfig()
	}
	return nil
}

func (h *serviceHost) startProxy() error {
	h.mu.Lock()
	if h.current != nil {
		h.lastError = ""
		h.lastOperation = "代理已在运行"
		h.mu.Unlock()
		return nil
	}
	h.mu.Unlock()

	bundle, err := runtime.PrepareClientService(h.configPath, true, nil)
	if err != nil {
		h.setLastError(err, "启动代理失败")
		return err
	}

	proxy := &runningProxy{
		bundle: bundle,
		done:   make(chan error, 1),
	}

	h.mu.Lock()
	h.current = proxy
	h.lastError = ""
	h.lastOperation = "正在启动代理"
	h.mu.Unlock()

	go func(proc *runningProxy) {
		err := runtime.RunPreparedService(proc.bundle)
		proc.done <- err
		close(proc.done)
		h.onProxyExit(proc, err)
	}(proxy)

	return nil
}

func (h *serviceHost) onProxyExit(proc *runningProxy, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.current != proc {
		return
	}
	h.current = nil
	if err != nil {
		h.lastError = err.Error()
		h.lastOperation = "代理异常退出"
		return
	}
	h.lastError = ""
	h.lastOperation = "代理已停止"
}

func (h *serviceHost) stopProxy(wait time.Duration) error {
	h.mu.Lock()
	proxy := h.current
	if proxy == nil {
		h.lastError = ""
		h.lastOperation = "代理未运行"
		h.mu.Unlock()
		return nil
	}
	h.lastError = ""
	h.lastOperation = "正在停止代理"
	h.mu.Unlock()

	proxy.bundle.Service.GracefulClose(500 * time.Millisecond)
	select {
	case <-proxy.done:
		return nil
	case <-time.After(wait):
		err := fmt.Errorf("timeout waiting for proxy shutdown")
		h.setLastError(err, "停止代理超时")
		return err
	}
}

func (h *serviceHost) reloadConfig() error {
	h.mu.Lock()
	proxy := h.current
	h.mu.Unlock()

	if proxy == nil {
		content, err := h.readConfig()
		if err != nil {
			h.setLastError(err, "读取配置失败")
			return err
		}
		return h.validateConfig(content)
	}
	if err := h.stopProxy(5 * time.Second); err != nil {
		h.setLastError(err, "重载配置失败")
		return err
	}
	if err := h.startProxy(); err != nil {
		return err
	}
	h.setLastOperation("代理已重载")
	return nil
}

func (h *serviceHost) setLastError(err error, op string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastOperation = op
	if err != nil {
		h.lastError = err.Error()
	} else {
		h.lastError = ""
	}
}

func (h *serviceHost) setLastOperation(op string) {
	h.setLastError(nil, op)
}

type HostRPC struct {
	host *serviceHost
}

func (r *HostRPC) GetStatus(_ EmptyRequest, resp *StatusPayload) error {
	*resp = r.host.status()
	return nil
}

func (r *HostRPC) GetConfig(_ EmptyRequest, resp *ConfigPayload) error {
	content, err := r.host.readConfig()
	if err != nil {
		return err
	}
	resp.Content = content
	return nil
}

func (r *HostRPC) ValidateConfig(req ConfigPayload, _ *EmptyRequest) error {
	return r.host.validateConfig(req.Content)
}

func (r *HostRPC) UpdateConfig(req ConfigPayload, _ *EmptyRequest) error {
	return r.host.updateConfig(req.Content)
}

func (r *HostRPC) StartProxy(_ EmptyRequest, _ *EmptyRequest) error {
	return r.host.startProxy()
}

func (r *HostRPC) StopProxy(_ EmptyRequest, _ *EmptyRequest) error {
	return r.host.stopProxy(5 * time.Second)
}

func (r *HostRPC) ReloadConfig(_ EmptyRequest, _ *EmptyRequest) error {
	return r.host.reloadConfig()
}
