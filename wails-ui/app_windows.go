//go:build windows

package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"os/exec"
	"path/filepath"
	rt "runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Microsoft/go-winio"
	frpruntime "github.com/fatedier/frp/cmd/frpc/runtime"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/getlantern/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

//go:embed build/windows/icon.ico
var trayIcon []byte

const (
	serviceName        = "frpc"
	serviceDisplayName = "frpc Service"
	pipeName           = `\\.\pipe\frpc-service-ipc`
	defaultServerAddr  = "154.12.16.190"
	defaultServerPort  = 7000
	defaultAuthToken   = "sk-962464@zap"
	defaultLocalPort   = 3389
	defaultRemotePort  = 13389
)

type EmptyRequest struct{}

type ConfigPayload struct {
	Content string
}

type StatusPayload struct {
	ConfigPath    string
	ProxyRunning  bool
	LastError     string
	LastOperation string
}

type pipeClient struct{}

type guiSettings struct {
	SilentStartAtLogin bool `json:"silentStartAtLogin"`
}

type scmStatus struct {
	Installed bool `json:"installed"`
	Running   bool `json:"running"`
	AutoStart bool `json:"autoStart"`
	PID       uint32
}

type ProxyEntry struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	LocalPort  int    `json:"localPort"`
	RemotePort int    `json:"remotePort"`
}

type ConnectionSettings struct {
	ServerAddr string       `json:"serverAddr"`
	ServerPort int          `json:"serverPort"`
	AuthToken  string       `json:"authToken"`
	Proxies    []ProxyEntry `json:"proxies"`
}

type StatusView struct {
	ServiceInstalled   bool   `json:"serviceInstalled"`
	ServiceRunning     bool   `json:"serviceRunning"`
	ServiceAutoStart   bool   `json:"serviceAutoStart"`
	ProxyRunning       bool   `json:"proxyRunning"`
	HeaderText         string `json:"headerText"`
	HeaderTone         string `json:"headerTone"`
	ServiceText        string `json:"serviceText"`
	ServiceDetail      string `json:"serviceDetail"`
	ProxyText          string `json:"proxyText"`
	ProxyDetail        string `json:"proxyDetail"`
	SilentStartAtLogin bool   `json:"silentStartAtLogin"`
	ConfigPath         string `json:"configPath"`
}

type Snapshot struct {
	Settings ConnectionSettings `json:"settings"`
	Status   StatusView         `json:"status"`
	Logs     []string           `json:"logs"`
}

type App struct {
	ctx         context.Context
	mu          sync.Mutex
	logs        []string
	silentStart bool
	trayOnce    sync.Once
	trayToggle  *systray.MenuItem
	trayShow    *systray.MenuItem
	trayQuit    *systray.MenuItem
}

func NewApp() *App {
	return &App{
		logs: make([]string, 0, 64),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.silentStart = hasArg("--silent")
	a.startTray()
	a.appendLog("Wails 界面已启动")
}

func (a *App) domReady(_ context.Context) {
	if a.silentStart {
		runtime.Hide(a.ctx)
	}
}

func (a *App) shutdown(context.Context) {
	systray.Quit()
}

func (a *App) beforeClose(context.Context) bool {
	return false
}

func (a *App) startHidden() bool {
	return hasArg("--silent")
}

func (a *App) GetSnapshot() (Snapshot, error) {
	settings, err := loadConnectionSettings()
	if err != nil {
		return Snapshot{}, err
	}
	status, err := getStatusView()
	if err != nil {
		return Snapshot{}, err
	}

	a.mu.Lock()
	logs := append([]string(nil), a.logs...)
	a.mu.Unlock()

	return Snapshot{
		Settings: settings,
		Status:   status,
		Logs:     logs,
	}, nil
}

func (a *App) SaveConnection(input ConnectionSettings) (Snapshot, error) {
	content, err := buildConfigContent(input)
	if err != nil {
		return Snapshot{}, err
	}
	if err := saveConfigContent(content); err != nil {
		return Snapshot{}, err
	}
	a.appendLog("连接配置已保存到 %s", machineConfigPath())
	return a.GetSnapshot()
}

func (a *App) ValidateConnection(input ConnectionSettings) (Snapshot, error) {
	content, err := buildConfigContent(input)
	if err != nil {
		return Snapshot{}, err
	}
	if err := validateConfigContent(content); err != nil {
		return Snapshot{}, err
	}
	a.appendLog("配置校验通过")
	return a.GetSnapshot()
}

func (a *App) ToggleProxy(input ConnectionSettings) (Snapshot, error) {
	running, err := proxyRunning()
	if err != nil {
		return Snapshot{}, err
	}
	if running {
		if err := stopProxy(); err != nil {
			return Snapshot{}, err
		}
		a.appendLog("代理停止请求已发送")
		return a.GetSnapshot()
	}

	content, err := buildConfigContent(input)
	if err != nil {
		return Snapshot{}, err
	}
	if err := ensureServiceReady(true); err != nil {
		return Snapshot{}, err
	}
	client := pipeClient{}
	if err := client.call("UpdateConfig", ConfigPayload{Content: string(content)}, &EmptyRequest{}); err != nil {
		return Snapshot{}, err
	}
	if err := client.call("StartProxy", EmptyRequest{}, &EmptyRequest{}); err != nil {
		return Snapshot{}, err
	}
	a.appendLog("代理启动请求已发送")
	return a.GetSnapshot()
}

func (a *App) ToggleService() (Snapshot, error) {
	status, err := querySCMStatus()
	if err != nil {
		return Snapshot{}, err
	}
	if status.Installed {
		if err := runElevatedServiceHost("--uninstall-service"); err != nil {
			return Snapshot{}, err
		}
		a.appendLog("Windows 服务已卸载")
		return a.GetSnapshot()
	}
	if err := runElevatedServiceHost("--install-service"); err != nil {
		return Snapshot{}, err
	}
	a.appendLog("Windows 服务已安装")
	return a.GetSnapshot()
}

func (a *App) SetServiceAutoStart(enabled bool) (Snapshot, error) {
	mode := "manual"
	if enabled {
		mode = "auto"
	}
	if err := runElevatedServiceHost("--set-service-startup=" + mode); err != nil {
		return Snapshot{}, err
	}
	if enabled {
		a.appendLog("已启用“代理随系统启动”")
	} else {
		a.appendLog("已关闭“代理随系统启动”")
	}
	return a.GetSnapshot()
}

func (a *App) SetSilentStart(enabled bool) (Snapshot, error) {
	if err := setLoginSilentStart(enabled); err != nil {
		return Snapshot{}, err
	}
	if enabled {
		a.appendLog("已启用“GUI 登录后静默启动”")
	} else {
		a.appendLog("已关闭“GUI 登录后静默启动”")
	}
	return a.GetSnapshot()
}

func (a *App) HideWindow() {
	if a.ctx != nil {
		runtime.Hide(a.ctx)
	}
}

func (a *App) startTray() {
	a.trayOnce.Do(func() {
		go func() {
			rt.LockOSThread()
			defer rt.UnlockOSThread()
			systray.Run(a.onTrayReady, func() {})
		}()
	})
}

func (a *App) onTrayReady() {
	systray.SetIcon(trayIcon)
	systray.SetTooltip("FRPC 中文客户端")

	showItem := systray.AddMenuItem("打开主窗口", "显示主界面")
	toggleItem := systray.AddMenuItem("启动代理", "启动或关闭代理")
	systray.AddSeparator()
	quitItem := systray.AddMenuItem("退出", "退出客户端")

	a.mu.Lock()
	a.trayShow = showItem
	a.trayToggle = toggleItem
	a.trayQuit = quitItem
	a.mu.Unlock()

	go func() {
		for range showItem.ClickedCh {
			a.showWindow()
		}
	}()

	go func() {
		for range toggleItem.ClickedCh {
			if err := a.toggleProxyFromTray(); err != nil {
				a.appendLog("托盘操作失败: %v", err)
			}
			a.refreshTray()
		}
	}()

	go func() {
		for range quitItem.ClickedCh {
			runtime.Quit(a.ctx)
		}
	}()

	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.refreshTray()
		}
	}()

	a.refreshTray()
}

func (a *App) showWindow() {
	if a.ctx == nil {
		return
	}
	runtime.Show(a.ctx)
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowCenter(a.ctx)
}

func (a *App) toggleProxyFromTray() error {
	settings, err := loadConnectionSettings()
	if err != nil {
		return err
	}
	_, err = a.ToggleProxy(settings)
	return err
}

func (a *App) refreshTray() {
	status, err := getStatusView()
	if err != nil {
		return
	}

	a.mu.Lock()
	toggleItem := a.trayToggle
	a.mu.Unlock()

	if toggleItem == nil {
		return
	}

	if status.ProxyRunning {
		toggleItem.SetTitle("关闭代理")
		systray.SetTooltip("FRPC 中文客户端 - 代理运行中")
		return
	}

	toggleItem.SetTitle("启动代理")
	if status.ServiceRunning {
		systray.SetTooltip("FRPC 中文客户端 - 服务已就绪")
	} else {
		systray.SetTooltip("FRPC 中文客户端 - 等待服务")
	}
}

func (a *App) appendLog(format string, args ...any) {
	line := fmt.Sprintf("[%s] %s", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
	a.mu.Lock()
	a.logs = append(a.logs, line)
	if len(a.logs) > 200 {
		a.logs = a.logs[len(a.logs)-200:]
	}
	a.mu.Unlock()
}

func hasArg(target string) bool {
	for _, arg := range os.Args[1:] {
		if strings.EqualFold(arg, target) {
			return true
		}
	}
	return false
}

func machineConfigPath() string {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	return filepath.Join(programData, "frp", "frpc.toml")
}

func userSettingsPath() string {
	appData := os.Getenv("AppData")
	if appData == "" {
		appData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
	}
	return filepath.Join(appData, "frp", "gui-settings.json")
}

func ensureParentDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

func loadGUISettings() (guiSettings, error) {
	path := userSettingsPath()
	if err := ensureParentDir(path); err != nil {
		return guiSettings{}, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return guiSettings{}, nil
	}
	if err != nil {
		return guiSettings{}, err
	}
	var settings guiSettings
	if err := json.Unmarshal(b, &settings); err != nil {
		return guiSettings{}, err
	}
	return settings, nil
}

func saveGUISettings(settings guiSettings) error {
	path := userSettingsPath()
	if err := ensureParentDir(path); err != nil {
		return err
	}
	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func loadConnectionSettings() (ConnectionSettings, error) {
	content, _, err := loadConfigText()
	if err != nil {
		return ConnectionSettings{
			ServerAddr: defaultServerAddr,
			ServerPort: defaultServerPort,
			AuthToken:  defaultAuthToken,
			Proxies:    []ProxyEntry{{Name: "default-tcp", Type: "tcp", LocalPort: defaultLocalPort, RemotePort: defaultRemotePort}},
		}, nil
	}
	cfg, err := frpruntime.ParseClientConfigContent([]byte(content), true)
	if err != nil {
		return ConnectionSettings{
			ServerAddr: defaultServerAddr,
			ServerPort: defaultServerPort,
			AuthToken:  defaultAuthToken,
			Proxies:    []ProxyEntry{{Name: "default-tcp", Type: "tcp", LocalPort: defaultLocalPort, RemotePort: defaultRemotePort}},
		}, nil
	}
	form := frpruntime.ExtractCommonFormData(cfg)
	proxies := extractAllProxies(cfg)
	if len(proxies) == 0 {
		proxies = []ProxyEntry{{Name: "default-tcp", Type: "tcp", LocalPort: defaultLocalPort, RemotePort: defaultRemotePort}}
	}
	settings := ConnectionSettings{
		ServerAddr: strings.TrimSpace(form.ServerAddr),
		ServerPort: form.ServerPort,
		AuthToken:  strings.TrimSpace(form.AuthToken),
		Proxies:    proxies,
	}
	if settings.ServerAddr == "" {
		settings.ServerAddr = defaultServerAddr
	}
	if settings.ServerPort == 0 {
		settings.ServerPort = defaultServerPort
	}
	if settings.AuthToken == "" {
		settings.AuthToken = defaultAuthToken
	}
	return settings, nil
}

func loadConfigText() (string, string, error) {
	var payload ConfigPayload
	if err := (pipeClient{}).call("GetConfig", EmptyRequest{}, &payload); err == nil {
		return payload.Content, "Windows 鏈嶅姟", nil
	}
	content, err := os.ReadFile(machineConfigPath())
	if err == nil {
		return string(content), "本地机器级配置文件", nil
	}
	if os.IsNotExist(err) {
		return string(frpruntime.DefaultClientConfigContent()), "内置默认模板", nil
	}
	return string(frpruntime.DefaultClientConfigContent()), "内置默认模板", err
}

func buildConfigContent(input ConnectionSettings) ([]byte, error) {
	existing, _, _ := loadConfigText()
	if strings.TrimSpace(existing) == "" {
		existing = string(frpruntime.DefaultClientConfigContent())
	}
	cfg, err := frpruntime.ParseClientConfigContent([]byte(existing), true)
	if err != nil {
		return nil, err
	}
	form := frpruntime.CommonFormData{
		ServerAddr: strings.TrimSpace(defaultIfEmpty(input.ServerAddr, defaultServerAddr)),
		ServerPort: normalizedPort(input.ServerPort),
		AuthMethod: "token",
		AuthToken:  strings.TrimSpace(defaultIfEmpty(input.AuthToken, defaultAuthToken)),
	}
	if err := frpruntime.ApplyCommonFormData(cfg, form); err != nil {
		return nil, err
	}
	applyAllProxies(cfg, input.Proxies)
	return frpruntime.MarshalClientConfigToTOML(cfg)
}

func validateConfigContent(content []byte) error {
	if err := ensureServiceReady(false); err == nil {
		return (pipeClient{}).call("ValidateConfig", ConfigPayload{Content: string(content)}, &EmptyRequest{})
	}
	return frpruntime.ValidateClientConfigContent(machineConfigPath(), content, true, nil)
}

func saveConfigContent(content []byte) error {
	if err := ensureServiceReady(true); err == nil {
		return (pipeClient{}).call("UpdateConfig", ConfigPayload{Content: string(content)}, &EmptyRequest{})
	}
	return frpruntime.WriteValidatedClientConfig(machineConfigPath(), content, true, nil)
}

func getStatusView() (StatusView, error) {
	scm, scmErr := querySCMStatus()
	settings, _ := loadGUISettings()
	status := StatusView{
		ServiceInstalled:   scm.Installed,
		ServiceRunning:     scm.Running,
		ServiceAutoStart:   scm.AutoStart,
		SilentStartAtLogin: settings.SilentStartAtLogin,
		ConfigPath:         machineConfigPath(),
		HeaderTone:         "muted",
	}

	switch {
	case scmErr != nil:
		status.HeaderText = "状态检测失败"
		status.HeaderTone = "danger"
		status.ServiceText = "检测失败"
		status.ServiceDetail = scmErr.Error()
	case !scm.Installed:
		status.HeaderText = "等待安装服务"
		status.HeaderTone = "warning"
		status.ServiceText = "未安装"
		status.ServiceDetail = "安装服务后即可在后台持续托管代理。"
	case scm.Running:
		status.HeaderText = "服务已就绪"
		status.HeaderTone = "success"
		status.ServiceText = "运行中"
		status.ServiceDetail = "Windows 服务已运行，可直接控制代理。"
	default:
		status.HeaderText = "服务已安装"
		status.HeaderTone = "primary"
		status.ServiceText = "已安装"
		status.ServiceDetail = "服务存在但未运行，启动代理时会自动拉起。"
	}

	var pipeStatus StatusPayload
	if err := (pipeClient{}).call("GetStatus", EmptyRequest{}, &pipeStatus); err == nil {
		status.ProxyRunning = pipeStatus.ProxyRunning
		switch {
		case pipeStatus.ProxyRunning:
			status.HeaderText = "代理运行中"
			status.HeaderTone = "success"
			status.ProxyText = "运行中"
			status.ProxyDetail = "客户端已连接，并由服务持续托管。"
		case pipeStatus.LastError != "":
			status.ProxyText = "异常"
			status.ProxyDetail = pipeStatus.LastError
		case pipeStatus.LastOperation != "":
			status.ProxyText = "已停止"
			status.ProxyDetail = pipeStatus.LastOperation
		default:
			status.ProxyText = "未连接"
			status.ProxyDetail = "尚未启动代理。"
		}
	} else if scm.Installed && scm.Running {
		status.ProxyText = "待连接"
		status.ProxyDetail = "服务正在运行，等待启动指令。"
	} else {
		status.ProxyText = "未连接"
		status.ProxyDetail = "服务未运行。"
	}

	return status, nil
}

func querySCMStatus() (scmStatus, error) {
	service, err := openServiceWithAccess(windows.SERVICE_QUERY_CONFIG | windows.SERVICE_QUERY_STATUS)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) || errors.Is(err, syscall.Errno(1060)) {
			return scmStatus{}, nil
		}
		return scmStatus{}, err
	}
	defer service.Close()

	cfg, err := service.Config()
	if err != nil {
		return scmStatus{}, err
	}
	status, err := service.Query()
	if err != nil {
		return scmStatus{}, err
	}
	return scmStatus{
		Installed: true,
		Running:   status.State == svc.Running,
		AutoStart: cfg.StartType == mgr.StartAutomatic,
		PID:       status.ProcessId,
	}, nil
}

func installService() error {
	exePath, err := serviceHostPath()
	if err != nil {
		return err
	}
	if err := ensureParentDir(machineConfigPath()); err != nil {
		return err
	}
	if _, err := os.Stat(machineConfigPath()); os.IsNotExist(err) {
		if err := os.WriteFile(machineConfigPath(), frpruntime.DefaultClientConfigContent(), 0o644); err != nil {
			return err
		}
	}

	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()

	service, err := manager.OpenService(serviceName)
	if err == nil {
		defer service.Close()
		cfg, cfgErr := service.Config()
		if cfgErr != nil {
			return cfgErr
		}
		cfg.DisplayName = serviceDisplayName
		cfg.Description = "frpc Windows service host"
		cfg.BinaryPathName = fmt.Sprintf("\"%s\" --service", exePath)
		cfg.ServiceStartName = "LocalSystem"
		if err := service.UpdateConfig(cfg); err != nil {
			return err
		}
		return nil
	}

	service, err = manager.CreateService(serviceName, exePath, mgr.Config{
		DisplayName:      serviceDisplayName,
		Description:      "frpc Windows service host",
		StartType:        mgr.StartManual,
		ServiceStartName: "LocalSystem",
	}, "--service")
	if err != nil {
		return err
	}
	defer service.Close()
	return nil
}

func uninstallService() error {
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()

	service, err := manager.OpenService(serviceName)
	if err != nil {
		return nil
	}
	defer service.Close()

	status, err := service.Query()
	if err == nil && status.State == svc.Running {
		_, _ = service.Control(svc.Stop)
		if waitErr := waitForServiceState(service, svc.Stopped, 10*time.Second); waitErr != nil {
			return waitErr
		}
	}
	return service.Delete()
}

func setServiceStartup(mode string) error {
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()

	service, err := manager.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer service.Close()

	cfg, err := service.Config()
	if err != nil {
		return err
	}
	switch mode {
	case "auto":
		cfg.StartType = mgr.StartAutomatic
	case "manual":
		cfg.StartType = mgr.StartManual
	default:
		return fmt.Errorf("unsupported startup mode %q", mode)
	}
	return service.UpdateConfig(cfg)
}

func setLoginSilentStart(enabled bool) error {
	settings, err := loadGUISettings()
	if err != nil {
		return err
	}
	settings.SilentStartAtLogin = enabled
	if err := saveGUISettings(settings); err != nil {
		return err
	}

	exePath, err := loginStartupExecutablePath()
	if err != nil {
		return err
	}
	runKey := `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	valueName := "frpc-wails"
	if enabled {
		cmdValue := fmt.Sprintf("\"%s\" --silent", filepath.Clean(exePath))
		return exec.Command("reg", "add", runKey, "/v", valueName, "/t", "REG_SZ", "/d", cmdValue, "/f").Run()
	}

	command := exec.Command("reg", "delete", runKey, "/v", valueName, "/f")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	err = command.Run()
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(stderr.String()), "unable to find") {
		return nil
	}
	return err
}

func loginStartupExecutablePath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}

	lower := strings.ToLower(exePath)
	if strings.Contains(lower, `\go-build`) && strings.HasSuffix(lower, ".test.exe") {
		_, currentFile, _, ok := rt.Caller(0)
		if ok {
			candidate := filepath.Join(filepath.Dir(currentFile), "build", "bin", "wails-ui.exe")
			candidate = filepath.Clean(candidate)
			if _, statErr := os.Stat(candidate); statErr == nil {
				return candidate, nil
			}
		}
	}

	return exePath, nil
}

func ensureServiceReady(startIfStopped bool) error {
	status, err := querySCMStatus()
	if err != nil {
		return err
	}
	if !status.Installed {
		return fmt.Errorf("请先安装 Windows 服务")
	}
	if !status.Running {
		if !startIfStopped {
			return fmt.Errorf("Windows 服务未运行")
		}
		if err := startServiceProcess(); err != nil {
			errText := strings.ToLower(err.Error())
			if strings.Contains(errText, "access is denied") {
				if elevateErr := runElevatedServiceHost("--start-service"); elevateErr != nil {
					return elevateErr
				}
			} else {
				return err
			}
		}
	}
	return waitForPipeReady(10 * time.Second)
}

func proxyRunning() (bool, error) {
	var status StatusPayload
	if err := (pipeClient{}).call("GetStatus", EmptyRequest{}, &status); err == nil {
		return status.ProxyRunning, nil
	}
	scm, err := querySCMStatus()
	if err != nil {
		return false, err
	}
	return scm.Installed && scm.Running && status.ProxyRunning, nil
}

func stopProxy() error {
	if err := ensureServiceReady(false); err != nil {
		return err
	}
	return (pipeClient{}).call("StopProxy", EmptyRequest{}, &EmptyRequest{})
}

func waitForPipeReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := pipeClient{}
	for time.Now().Before(deadline) {
		var status StatusPayload
		if err := client.call("GetStatus", EmptyRequest{}, &status); err == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("Windows 鏈嶅姟宸插惎鍔紝浣嗘帶鍒堕€氶亾灏氭湭灏辩华")
}

func startServiceProcess() error {
	service, err := openServiceWithAccess(windows.SERVICE_START | windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return err
	}
	defer service.Close()

	status, err := service.Query()
	if err == nil && status.State == svc.Running {
		return nil
	}
	if err := service.Start(); err != nil && !strings.Contains(strings.ToLower(err.Error()), "already running") {
		return err
	}
	return waitForServiceState(service, svc.Running, 10*time.Second)
}

func openServiceWithAccess(access uint32) (*mgr.Service, error) {
	managerHandle, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return nil, err
	}
	serviceHandle, err := windows.OpenService(managerHandle, syscall.StringToUTF16Ptr(serviceName), access)
	_ = windows.CloseServiceHandle(managerHandle)
	if err != nil {
		return nil, err
	}
	return &mgr.Service{Name: serviceName, Handle: serviceHandle}, nil
}

func waitForServiceState(service *mgr.Service, desired svc.State, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := service.Query()
		if err != nil {
			return err
		}
		if status.State == desired {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("service did not reach state %v", desired)
}

func serviceHostPath() (string, error) {
	exePath, err := os.Executable()
	if err == nil {
		baseDir := filepath.Dir(exePath)
		candidates := []string{
			filepath.Join(baseDir, "frpc-service.exe"),
			filepath.Join(baseDir, "frpc.exe"),
			filepath.Join(baseDir, "..", "src", "frpc.exe"),
		}
		for _, candidate := range candidates {
			candidate = filepath.Clean(candidate)
			if _, statErr := os.Stat(candidate); statErr == nil {
				return candidate, nil
			}
		}
	}

	_, currentFile, _, ok := rt.Caller(0)
	if ok {
		baseDir := filepath.Dir(currentFile)
		candidates := []string{
			filepath.Join(baseDir, "build", "bin", "frpc-service.exe"),
			filepath.Join(baseDir, "..", "src", "frpc.exe"),
		}
		for _, candidate := range candidates {
			candidate = filepath.Clean(candidate)
			if _, statErr := os.Stat(candidate); statErr == nil {
				return candidate, nil
			}
		}
	}

	return "", fmt.Errorf("找不到服务宿主 frpc-service.exe 或 frpc.exe")
}

func defaultIfEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func normalizedLocalPort(value int) int {
	if value <= 0 {
		return defaultLocalPort
	}
	return value
}

func normalizedRemotePort(value int) int {
	if value <= 0 {
		return defaultRemotePort
	}
	return value
}

func normalizedPort(value int) int {
	if value <= 0 {
		return defaultServerPort
	}
	return value
}

func extractAllProxies(cfg *v1.ClientConfig) []ProxyEntry {
	if cfg == nil || len(cfg.Proxies) == 0 {
		return nil
	}
	entries := make([]ProxyEntry, 0, len(cfg.Proxies))
	for _, p := range cfg.Proxies {
		base := p.GetBaseConfig()
		if base == nil {
			continue
		}
		entry := ProxyEntry{
			Name:      base.Name,
			Type:      p.Type,
			LocalPort: base.LocalPort,
		}
		switch conf := p.ProxyConfigurer.(type) {
		case *v1.TCPProxyConfig:
			entry.RemotePort = conf.RemotePort
		case *v1.UDPProxyConfig:
			entry.RemotePort = conf.RemotePort
		}
		entries = append(entries, entry)
	}
	return entries
}

func applyAllProxies(cfg *v1.ClientConfig, proxies []ProxyEntry) {
	if cfg == nil {
		return
	}
	if len(proxies) == 0 {
		proxies = []ProxyEntry{{Name: "default-tcp", Type: "tcp", LocalPort: defaultLocalPort, RemotePort: defaultRemotePort}}
	}
	result := make([]v1.TypedProxyConfig, 0, len(proxies))
	for i, entry := range proxies {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			name = fmt.Sprintf("proxy-%d", i+1)
		}
		proxyType := strings.ToLower(strings.TrimSpace(entry.Type))
		if proxyType != "udp" {
			proxyType = "tcp"
		}
		localPort := normalizedLocalPort(entry.LocalPort)
		remotePort := normalizedRemotePort(entry.RemotePort)
		localIP := "127.0.0.1"

		base := v1.ProxyBaseConfig{
			Name: name,
			Type: proxyType,
			ProxyBackend: v1.ProxyBackend{
				LocalIP:   localIP,
				LocalPort: localPort,
			},
		}

		var configurer v1.ProxyConfigurer
		if proxyType == "udp" {
			configurer = &v1.UDPProxyConfig{
				ProxyBaseConfig: base,
				RemotePort:      remotePort,
			}
		} else {
			configurer = &v1.TCPProxyConfig{
				ProxyBaseConfig: base,
				RemotePort:      remotePort,
			}
		}
		result = append(result, v1.TypedProxyConfig{
			Type:            proxyType,
			ProxyConfigurer: configurer,
		})
	}
	cfg.Proxies = result
}

func runElevatedServiceHost(args ...string) error {
	exePath, err := serviceHostPath()
	if err != nil {
		return err
	}
	escapedArgs := make([]string, 0, len(args))
	for _, arg := range args {
		escapedArgs = append(escapedArgs, "'"+strings.ReplaceAll(arg, "'", "''")+"'")
	}
	ps := fmt.Sprintf(
		"Start-Process -Verb RunAs -FilePath '%s' -ArgumentList @(%s) -Wait",
		strings.ReplaceAll(exePath, "'", "''"),
		strings.Join(escapedArgs, ","),
	)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			return fmt.Errorf("%w: %s", err, trimmed)
		}
		if errors.Is(err, exec.ErrNotFound) {
			return err
		}
		return err
	}
	return nil
}
func (pipeClient) call(method string, req any, resp any) error {
	timeout := time.Second
	conn, err := winio.DialPipe(pipeName, &timeout)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := rpc.NewClientWithCodec(jsonrpc.NewClientCodec(conn))
	defer client.Close()
	return client.Call("FrpcService."+method, req, resp)
}

func servePipeConnection(server *rpc.Server, conn io.ReadWriteCloser) {
	server.ServeCodec(jsonrpc.NewServerCodec(conn))
}

func isPipeUnavailable(err error) bool {
	if err == nil {
		return false
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) || errors.Is(err, winio.ErrTimeout)
}
