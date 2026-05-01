//go:build windows

package winapp

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/fatedier/frp/cmd/frpc/runtime"
)

const (
	appTitle          = "FRPC 中文客户端"
	appSubtitle       = "连接、状态与代理控制"
	defaultServerAddr = "154.12.16.190"
	defaultServerPort = 7000
	defaultAuthToken  = "sk-962464@zap"
)

var (
	colorTitle       = walk.RGB(15, 23, 42)
	colorBody        = walk.RGB(51, 65, 85)
	colorMuted       = walk.RGB(100, 116, 139)
	colorPrimary     = walk.RGB(37, 99, 235)
	colorPrimarySoft = walk.RGB(219, 234, 254)
	colorSuccess     = walk.RGB(22, 163, 74)
	colorWarning     = walk.RGB(202, 138, 4)
	colorDanger      = walk.RGB(220, 38, 38)
	colorPanelTop    = walk.RGB(232, 240, 248)
	colorPanelBot    = walk.RGB(246, 249, 252)
	colorCardTop     = walk.RGB(250, 252, 255)
	colorCardBot     = walk.RGB(239, 245, 252)
	colorInsetTop    = walk.RGB(255, 255, 255)
	colorInsetBot    = walk.RGB(244, 248, 253)
	colorAccentBg    = walk.RGB(232, 244, 255)
)

type guiApp struct {
	mw               *walk.MainWindow
	notifyIcon       *walk.NotifyIcon
	appIcon          *walk.Icon
	serverAddrEdit   *walk.LineEdit
	serverPortEdit   *walk.NumberEdit
	authTokenEdit    *walk.LineEdit
	authToggleBtn    *walk.PushButton
	serviceAutoCheck *walk.CheckBox
	loginSilentCheck *walk.CheckBox
	headerStatus     *walk.Label
	serviceBadge     *walk.Label
	serviceDetail    *walk.Label
	proxyBadge       *walk.Label
	proxyDetail      *walk.Label
	logView          *walk.TextEdit
	serviceToggleBtn *walk.PushButton
	toggleProxyBtn   *walk.PushButton
	trayToggleAction *walk.Action
	silentStart      bool
	exiting          bool
	suppressCheckbox bool
	authTokenVisible bool
	done             chan struct{}
}

func RunGUI(silent bool) error {
	app := &guiApp{
		silentStart: silent,
		done:        make(chan struct{}),
	}
	return app.run()
}

func (a *guiApp) run() error {
	settings, err := loadGUISettings()
	if err != nil {
		return err
	}

	a.appIcon = walk.IconShield()
	window := MainWindow{
		AssignTo: &a.mw,
		Title:    appTitle,
		Icon:     a.appIcon,
		Visible:  false,
		Background: VerticalGradientBrush{Stops: []walk.GradientStop{
			{Offset: 0.0, Color: colorPanelTop},
			{Offset: 1.0, Color: colorPanelBot},
		}},
		MinSize: Size{1024, 720},
		Size:    Size{1140, 780},
		Layout:  VBox{Margins: Margins{Left: 22, Top: 22, Right: 22, Bottom: 22}, Spacing: 18},
		Children: []Widget{
			Composite{
				Border: true,
				Background: VerticalGradientBrush{Stops: []walk.GradientStop{
					{Offset: 0.0, Color: walk.RGB(255, 255, 255)},
					{Offset: 1.0, Color: walk.RGB(245, 249, 253)},
				}},
				Layout: HBox{Margins: Margins{Left: 28, Top: 24, Right: 24, Bottom: 24}, Spacing: 20},
				Children: []Widget{
					Composite{
						Layout: VBox{Spacing: 4},
						Children: []Widget{
							Label{
								Text:      appTitle,
								Font:      Font{Family: "Segoe UI", PointSize: 19, Bold: true},
								TextColor: colorTitle,
							},
							Label{
								Text:      "本地代理控制面板，保持连接配置简洁、状态清晰、操作直接。",
								Font:      Font{Family: "Segoe UI", PointSize: 10},
								TextColor: colorMuted,
							},
						},
					},
					HSpacer{},
					Composite{
						Background: VerticalGradientBrush{Stops: []walk.GradientStop{
							{Offset: 0.0, Color: walk.RGB(243, 249, 255)},
							{Offset: 1.0, Color: colorPrimarySoft},
						}},
						Layout: VBox{Margins: Margins{Left: 24, Top: 14, Right: 24, Bottom: 14}, SpacingZero: true},
						Children: []Widget{
							Label{
								AssignTo:      &a.headerStatus,
								Text:          "状态检测中",
								Font:          Font{Family: "Segoe UI", PointSize: 11, Bold: true},
								TextColor:     colorPrimary,
								TextAlignment: AlignCenter,
								MinSize:       Size{180, 28},
							},
						},
					},
				},
			},
			Composite{
				Layout: HBox{Spacing: 16},
				Children: []Widget{
					Composite{
						StretchFactor: 5,
						Layout:        VBox{Spacing: 14},
						Children: []Widget{
							Composite{
								Border: true,
								Background: VerticalGradientBrush{Stops: []walk.GradientStop{
									{Offset: 0.0, Color: colorCardTop},
									{Offset: 1.0, Color: colorCardBot},
								}},
								Layout: VBox{Margins: Margins{Left: 24, Top: 22, Right: 24, Bottom: 22}, Spacing: 16},
								Children: []Widget{
									Composite{
										Layout: VBox{Spacing: 3},
										Children: []Widget{
											Label{
												Text:      "连接配置",
												Font:      Font{Family: "Segoe UI", PointSize: 13, Bold: true},
												TextColor: colorTitle,
											},
											Label{
												Text:      "只保留连接所需的 3 个字段，避免界面噪音。",
												Font:      Font{Family: "Segoe UI", PointSize: 9},
												TextColor: colorMuted,
											},
										},
									},
									Composite{
										Background: VerticalGradientBrush{Stops: []walk.GradientStop{
											{Offset: 0.0, Color: colorInsetTop},
											{Offset: 1.0, Color: colorInsetBot},
										}},
										Layout: Grid{Columns: 4, Margins: Margins{Left: 18, Top: 18, Right: 18, Bottom: 18}, Spacing: 12},
										Children: []Widget{
											Label{
												Text:      "服务端地址",
												TextColor: colorBody,
												MinSize:   Size{92, 0},
											},
											LineEdit{
												AssignTo:   &a.serverAddrEdit,
												ColumnSpan: 2,
												CueBanner:  "例如 154.12.16.190",
											},
											NumberEdit{
												AssignTo:  &a.serverPortEdit,
												MinValue:  1,
												MaxValue:  65535,
												MinSize:   Size{120, 0},
												TextColor: colorBody,
											},
											Label{
												Text:      "访问密钥",
												TextColor: colorBody,
												MinSize:   Size{92, 0},
											},
											LineEdit{
												AssignTo:     &a.authTokenEdit,
												ColumnSpan:   2,
												CueBanner:    "请输入访问密钥",
												PasswordMode: true,
											},
											PushButton{
												AssignTo:  &a.authToggleBtn,
												Text:      "显示",
												MinSize:   Size{120, 0},
												OnClicked: a.toggleAuthVisibility,
											},
										},
									},
								},
							},
							Composite{
								Border: true,
								Background: VerticalGradientBrush{Stops: []walk.GradientStop{
									{Offset: 0.0, Color: walk.RGB(255, 255, 255)},
									{Offset: 1.0, Color: walk.RGB(244, 248, 253)},
								}},
								Layout: VBox{Margins: Margins{Left: 18, Top: 16, Right: 18, Bottom: 16}, Spacing: 12},
								Children: []Widget{
									Composite{
										Layout: VBox{Spacing: 3},
										Children: []Widget{
											Label{
												Text:      "快捷操作",
												Font:      Font{Family: "Segoe UI", PointSize: 11, Bold: true},
												TextColor: colorTitle,
											},
											Label{
												Text:      "优先处理代理启动，其余操作保留在同一块区域内。",
												Font:      Font{Family: "Segoe UI", PointSize: 9},
												TextColor: colorMuted,
											},
										},
									},
									Composite{
										Layout: Grid{Columns: 2, Spacing: 12},
										Children: []Widget{
											PushButton{
												AssignTo:   &a.toggleProxyBtn,
												Text:       "启动代理",
												Font:       Font{Family: "Segoe UI", PointSize: 12, Bold: true},
												Background: SolidColorBrush{Color: colorPrimarySoft},
												MinSize:    Size{0, 48},
												OnClicked:  a.toggleProxy,
											},
											PushButton{
												AssignTo:  &a.serviceToggleBtn,
												Text:      "安装服务",
												MinSize:   Size{0, 40},
												OnClicked: a.toggleService,
											},
											PushButton{
												Text:      "保存配置",
												MinSize:   Size{0, 40},
												OnClicked: a.saveConfig,
											},
											PushButton{
												Text:      "校验配置",
												MinSize:   Size{0, 40},
												OnClicked: a.validateConfig,
											},
										},
									},
								},
							},
							Composite{
								Border: true,
								Background: VerticalGradientBrush{Stops: []walk.GradientStop{
									{Offset: 0.0, Color: walk.RGB(255, 255, 255)},
									{Offset: 1.0, Color: walk.RGB(245, 248, 252)},
								}},
								Layout: VBox{Margins: Margins{Left: 20, Top: 18, Right: 20, Bottom: 18}, Spacing: 10},
								Children: []Widget{
									Composite{
										Layout: VBox{Spacing: 3},
										Children: []Widget{
											Label{
												Text:      "运行日志",
												Font:      Font{Family: "Segoe UI", PointSize: 10, Bold: true},
												TextColor: colorTitle,
											},
											Label{
												Text:      "最近的运行记录会持续显示在这里，便于快速排障。",
												Font:      Font{Family: "Segoe UI", PointSize: 9},
												TextColor: colorMuted,
											},
										},
									},
									TextEdit{
										AssignTo:  &a.logView,
										ReadOnly:  true,
										VScroll:   true,
										MinSize:   Size{0, 132},
										TextColor: colorBody,
									},
								},
							},
						},
					},
					Composite{
						Background: VerticalGradientBrush{Stops: []walk.GradientStop{
							{Offset: 0.0, Color: colorCardTop},
							{Offset: 1.0, Color: colorCardBot},
						}},
						StretchFactor: 2,
						Border:        true,
						Layout:        VBox{Margins: Margins{Left: 20, Top: 22, Right: 20, Bottom: 22}, Spacing: 16},
						Children: []Widget{
							Composite{
								Layout: VBox{Spacing: 3},
								Children: []Widget{
									Label{
										Text:      "运行状态",
										Font:      Font{Family: "Segoe UI", PointSize: 12, Bold: true},
										TextColor: colorTitle,
									},
									Label{
										Text:      "服务就绪后即可一键控制代理运行。",
										Font:      Font{Family: "Segoe UI", PointSize: 9},
										TextColor: colorMuted,
									},
								},
							},
							Composite{
								Background: VerticalGradientBrush{Stops: []walk.GradientStop{
									{Offset: 0.0, Color: colorInsetTop},
									{Offset: 1.0, Color: colorInsetBot},
								}},
								Layout: VBox{Margins: Margins{Left: 16, Top: 14, Right: 16, Bottom: 14}, Spacing: 5},
								Children: []Widget{
									Label{
										Text:      "服务状态",
										Font:      Font{Family: "Segoe UI", PointSize: 9},
										TextColor: colorBody,
									},
									Label{
										AssignTo:  &a.serviceBadge,
										Text:      "检测中",
										Font:      Font{Family: "Segoe UI", PointSize: 14, Bold: true},
										TextColor: colorPrimary,
									},
									Label{
										AssignTo:  &a.serviceDetail,
										Text:      "正在读取服务状态",
										Font:      Font{Family: "Segoe UI", PointSize: 9},
										TextColor: colorMuted,
									},
								},
							},
							Composite{
								Background: VerticalGradientBrush{Stops: []walk.GradientStop{
									{Offset: 0.0, Color: walk.RGB(252, 254, 255)},
									{Offset: 1.0, Color: walk.RGB(244, 248, 253)},
								}},
								Layout: VBox{Margins: Margins{Left: 16, Top: 14, Right: 16, Bottom: 14}, Spacing: 5},
								Children: []Widget{
									Label{
										Text:      "连接状态",
										Font:      Font{Family: "Segoe UI", PointSize: 9},
										TextColor: colorBody,
									},
									Label{
										AssignTo:  &a.proxyBadge,
										Text:      "检测中",
										Font:      Font{Family: "Segoe UI", PointSize: 14, Bold: true},
										TextColor: colorPrimary,
									},
									Label{
										AssignTo:  &a.proxyDetail,
										Text:      "等待状态同步",
										Font:      Font{Family: "Segoe UI", PointSize: 9},
										TextColor: colorMuted,
									},
								},
							},
							VSpacer{Size: 2},
							CheckBox{
								AssignTo:         &a.serviceAutoCheck,
								Text:             "代理随系统启动",
								OnCheckedChanged: a.onServiceAutoChanged,
							},
							CheckBox{
								AssignTo:         &a.loginSilentCheck,
								Text:             "GUI 登录后静默启动",
								Checked:          settings.SilentStartAtLogin,
								OnCheckedChanged: a.onLoginSilentChanged,
							},
						},
					},
				},
			},
		},
	}

	if err := window.Create(); err != nil {
		return err
	}
	if err := a.initAfterCreate(); err != nil {
		return err
	}
	a.loadInitialState()
	a.startRefreshLoop()

	if a.silentStart {
		a.hideToTray(false)
	} else {
		a.restoreWindow()
	}

	exitCode := a.mw.Run()
	close(a.done)
	if a.notifyIcon != nil {
		_ = a.notifyIcon.Dispose()
	}
	if exitCode != 0 {
		return fmt.Errorf("GUI exited with code %d", exitCode)
	}
	return nil
}

func (a *guiApp) initAfterCreate() error {
	ni, err := walk.NewNotifyIcon(a.mw)
	if err != nil {
		return err
	}
	a.notifyIcon = ni
	if a.appIcon != nil {
		if err := a.notifyIcon.SetIcon(a.appIcon); err != nil {
			return err
		}
	}
	if err := a.notifyIcon.SetToolTip(appTitle); err != nil {
		return err
	}
	if err := a.notifyIcon.SetVisible(true); err != nil {
		return err
	}
	a.notifyIcon.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			a.restoreWindow()
		}
	})
	if err := a.buildTrayMenu(); err != nil {
		return err
	}

	a.updateAuthVisibilityUI()

	a.mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		if a.exiting {
			return
		}
		*canceled = true
		a.hideToTray(true)
	})
	return nil
}

func (a *guiApp) buildTrayMenu() error {
	toggleAction := walk.NewAction()
	if err := toggleAction.SetText("启动代理"); err != nil {
		return err
	}
	toggleAction.Triggered().Attach(a.toggleProxy)
	a.trayToggleAction = toggleAction
	if err := a.notifyIcon.ContextMenu().Actions().Add(toggleAction); err != nil {
		return err
	}

	for _, item := range []struct {
		text string
		fn   func()
	}{
		{text: "打开面板", fn: a.restoreWindow},
		{text: "退出界面", fn: a.exitGUI},
	} {
		action := walk.NewAction()
		if err := action.SetText(item.text); err != nil {
			return err
		}
		action.Triggered().Attach(item.fn)
		if err := a.notifyIcon.ContextMenu().Actions().Add(action); err != nil {
			return err
		}
	}
	return nil
}

func (a *guiApp) loadInitialState() {
	content, sourceName, err := a.loadConfigText()
	if err != nil {
		a.appendLog("读取配置失败，已使用默认值: %v", err)
	}
	if err := a.syncFormFromConfigText(content); err != nil {
		a.applyDefaultFormData()
		a.appendLog("配置不完整，已回退到默认连接参数")
	} else {
		a.appendLog("已加载连接配置来源: %s", sourceName)
	}
	a.refreshStatus()
}

func (a *guiApp) loadConfigText() (string, string, error) {
	var payload ConfigPayload
	if err := (pipeClient{}).call("GetConfig", EmptyRequest{}, &payload); err == nil {
		return payload.Content, "Windows 服务", nil
	}

	content, err := os.ReadFile(machineConfigPath())
	if err == nil {
		return string(content), "本地机器级配置文件", nil
	}
	if os.IsNotExist(err) {
		return string(runtime.DefaultClientConfigContent()), "内置默认模板", nil
	}
	return string(runtime.DefaultClientConfigContent()), "内置默认模板", err
}

func (a *guiApp) syncFormFromConfigText(content string) error {
	cfg, err := runtime.ParseClientConfigContent([]byte(content), true)
	if err != nil {
		return err
	}
	form := runtime.ExtractCommonFormData(cfg)
	if strings.TrimSpace(form.ServerAddr) == "" {
		form.ServerAddr = defaultServerAddr
	}
	if form.ServerPort == 0 {
		form.ServerPort = defaultServerPort
	}
	if strings.TrimSpace(form.AuthToken) == "" {
		form.AuthToken = defaultAuthToken
	}
	_ = a.serverAddrEdit.SetText(form.ServerAddr)
	_ = a.serverPortEdit.SetValue(float64(form.ServerPort))
	_ = a.authTokenEdit.SetText(form.AuthToken)
	return nil
}

func (a *guiApp) applyDefaultFormData() {
	_ = a.serverAddrEdit.SetText(defaultServerAddr)
	_ = a.serverPortEdit.SetValue(float64(defaultServerPort))
	_ = a.authTokenEdit.SetText(defaultAuthToken)
}

func (a *guiApp) currentFormData() runtime.CommonFormData {
	return runtime.CommonFormData{
		ServerAddr: defaultIfEmpty(strings.TrimSpace(a.serverAddrEdit.Text()), defaultServerAddr),
		ServerPort: int(a.serverPortEdit.Value()),
		AuthMethod: "token",
		AuthToken:  defaultIfEmpty(strings.TrimSpace(a.authTokenEdit.Text()), defaultAuthToken),
	}
}

func (a *guiApp) buildConfigContentFromUI() ([]byte, error) {
	cfg, err := runtime.ParseClientConfigContent(runtime.DefaultClientConfigContent(), true)
	if err != nil {
		return nil, err
	}
	if err := runtime.ApplyCommonFormData(cfg, a.currentFormData()); err != nil {
		return nil, err
	}
	return runtime.MarshalClientConfigToTOML(cfg)
}

func (a *guiApp) validateConfig() {
	content, err := a.buildConfigContentFromUI()
	if err != nil {
		a.showError("校验配置失败", err)
		return
	}
	if err := a.validateContent(content); err != nil {
		a.showError("校验配置失败", err)
		return
	}
	a.appendLog("配置校验通过")
	a.refreshStatus()
}

func (a *guiApp) saveConfig() {
	content, err := a.buildConfigContentFromUI()
	if err != nil {
		a.showError("保存配置失败", err)
		return
	}
	if err := a.saveContent(content); err != nil {
		a.showError("保存配置失败", err)
		return
	}
	a.appendLog("连接配置已保存到 %s", machineConfigPath())
	a.refreshStatus()
}

func (a *guiApp) toggleProxy() {
	running, err := a.proxyRunning()
	if err != nil {
		a.showError("切换代理失败", err)
		return
	}
	if running {
		a.stopProxy()
		return
	}
	a.startProxy()
}

func (a *guiApp) startProxy() {
	content, err := a.buildConfigContentFromUI()
	if err != nil {
		a.showError("启动代理失败", err)
		return
	}
	if err := a.ensureServiceReady(true); err != nil {
		a.showError("启动代理失败", err)
		return
	}
	client := pipeClient{}
	if err := client.call("UpdateConfig", ConfigPayload{Content: string(content)}, &EmptyRequest{}); err != nil {
		a.showError("启动代理失败", err)
		return
	}
	if err := client.call("StartProxy", EmptyRequest{}, &EmptyRequest{}); err != nil {
		a.showError("启动代理失败", err)
		return
	}
	a.appendLog("代理启动请求已发送")
	a.refreshStatus()
}

func (a *guiApp) stopProxy() {
	if err := a.ensureServiceReady(false); err != nil {
		a.appendLog("关闭代理前检查服务状态: %v", err)
		a.refreshStatus()
		return
	}
	if err := (pipeClient{}).call("StopProxy", EmptyRequest{}, &EmptyRequest{}); err != nil {
		a.showError("关闭代理失败", err)
		return
	}
	a.appendLog("代理停止请求已发送")
	a.refreshStatus()
}

func (a *guiApp) proxyRunning() (bool, error) {
	var status StatusPayload
	if err := (pipeClient{}).call("GetStatus", EmptyRequest{}, &status); err == nil {
		return status.ProxyRunning, nil
	}

	scm, err := querySCMStatus()
	if err != nil {
		return false, err
	}
	if !scm.Installed || !scm.Running {
		return false, nil
	}
	return false, nil
}

func (a *guiApp) toggleService() {
	status, err := querySCMStatus()
	if err != nil {
		a.showError("检查服务状态失败", err)
		return
	}
	if status.Installed {
		a.uninstallService()
		return
	}
	a.installService()
}

func (a *guiApp) installService() {
	if err := runElevated("--install-service"); err != nil {
		a.showError("安装服务失败", err)
		return
	}
	if err := a.ensureServiceReady(true); err != nil {
		a.showError("安装服务后启动失败", err)
		return
	}
	content, err := a.buildConfigContentFromUI()
	if err == nil {
		_ = (pipeClient{}).call("UpdateConfig", ConfigPayload{Content: string(content)}, &EmptyRequest{})
	}
	a.appendLog("Windows 服务已安装")
	a.refreshStatus()
}

func (a *guiApp) uninstallService() {
	if walk.MsgBox(a.mw, "确认卸载", "卸载服务后，GUI 退出后将无法继续在后台保持代理运行。是否继续？", walk.MsgBoxYesNo|walk.MsgBoxIconWarning) != 6 {
		return
	}
	if err := runElevated("--uninstall-service"); err != nil {
		a.showError("卸载服务失败", err)
		return
	}
	a.appendLog("Windows 服务已卸载")
	a.refreshStatus()
}

func (a *guiApp) onServiceAutoChanged() {
	if a.suppressCheckbox {
		return
	}
	targetAuto := a.serviceAutoCheck.Checked()
	status, err := querySCMStatus()
	if err != nil {
		a.revertServiceAutoCheck()
		a.showError("修改启动项失败", err)
		return
	}
	if !status.Installed {
		a.revertServiceAutoCheck()
		a.showError("修改启动项失败", fmt.Errorf("请先安装 Windows 服务"))
		return
	}
	mode := "manual"
	if targetAuto {
		mode = "auto"
	}
	if err := runElevated("--set-service-startup=" + mode); err != nil {
		a.revertServiceAutoCheck()
		a.showError("修改启动项失败", err)
		return
	}
	if targetAuto {
		a.appendLog("已启用“代理随系统启动”")
	} else {
		a.appendLog("已关闭“代理随系统启动”")
	}
	a.refreshStatus()
}

func (a *guiApp) onLoginSilentChanged() {
	if a.suppressCheckbox {
		return
	}
	enabled := a.loginSilentCheck.Checked()
	if err := setLoginSilentStart(enabled); err != nil {
		a.suppressCheckbox = true
		a.loginSilentCheck.SetChecked(!enabled)
		a.suppressCheckbox = false
		a.showError("修改登录静默启动失败", err)
		return
	}
	if enabled {
		a.appendLog("已启用“GUI 登录后静默启动”")
	} else {
		a.appendLog("已关闭“GUI 登录后静默启动”")
	}
}

func (a *guiApp) revertServiceAutoCheck() {
	a.suppressCheckbox = true
	a.serviceAutoCheck.SetChecked(!a.serviceAutoCheck.Checked())
	a.suppressCheckbox = false
}

func (a *guiApp) validateContent(content []byte) error {
	client := pipeClient{}
	if err := a.ensureServiceReady(false); err == nil {
		return client.call("ValidateConfig", ConfigPayload{Content: string(content)}, &EmptyRequest{})
	}
	return runtime.ValidateClientConfigContent(machineConfigPath(), content, true, nil)
}

func (a *guiApp) saveContent(content []byte) error {
	client := pipeClient{}
	if err := a.ensureServiceReady(true); err == nil {
		return client.call("UpdateConfig", ConfigPayload{Content: string(content)}, &EmptyRequest{})
	}
	return runtime.WriteValidatedClientConfig(machineConfigPath(), content, true, nil)
}

func (a *guiApp) ensureServiceReady(startIfStopped bool) error {
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
				if elevateErr := runElevated("--start-service"); elevateErr != nil {
					return elevateErr
				}
			} else {
				return err
			}
		}
	}
	return waitForPipeReady(10 * time.Second)
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
	return fmt.Errorf("Windows 服务已启动，但控制通道尚未就绪")
}

func (a *guiApp) refreshStatus() {
	scm, scmErr := querySCMStatus()
	settings, settingsErr := loadGUISettings()
	var (
		headerText    string
		headerColor   walk.Color
		serviceText   string
		serviceDetail string
		serviceColor  walk.Color
		proxyText     string
		proxyDetail   string
		proxyColor    walk.Color
		pipeStatus    StatusPayload
		proxyRunning  bool
	)

	switch {
	case scmErr != nil:
		headerText = "状态检测失败"
		headerColor = colorDanger
		serviceText = "检测失败"
		serviceDetail = scmErr.Error()
		serviceColor = colorDanger
	case !scm.Installed:
		headerText = "等待安装服务"
		headerColor = colorWarning
		serviceText = "未安装"
		serviceDetail = "安装服务后可在后台保持代理运行"
		serviceColor = colorWarning
	case scm.Running:
		headerText = "服务已就绪"
		headerColor = colorPrimary
		serviceText = "运行中"
		serviceDetail = "Windows 服务已运行，可直接控制代理"
		serviceColor = colorSuccess
	default:
		headerText = "服务已安装"
		headerColor = colorPrimary
		serviceText = "已安装"
		serviceDetail = "服务存在但未运行，启动代理时会自动拉起"
		serviceColor = colorPrimary
	}

	if scmErr == nil && scm.AutoStart {
		serviceDetail += "，并将随系统自动启动"
	}

	if err := (pipeClient{}).call("GetStatus", EmptyRequest{}, &pipeStatus); err == nil {
		proxyRunning = pipeStatus.ProxyRunning
		switch {
		case pipeStatus.ProxyRunning:
			headerText = "代理运行中"
			headerColor = colorSuccess
			proxyText = "运行中"
			proxyDetail = "客户端已连接并由服务托管"
			proxyColor = colorSuccess
		case pipeStatus.LastError != "":
			proxyText = "异常"
			proxyDetail = pipeStatus.LastError
			proxyColor = colorDanger
		case pipeStatus.LastOperation != "":
			proxyText = "已停止"
			proxyDetail = pipeStatus.LastOperation
			proxyColor = colorMuted
		default:
			proxyText = "未连接"
			proxyDetail = "尚未启动代理"
			proxyColor = colorMuted
		}
	} else if scmErr == nil && scm.Installed && scm.Running {
		proxyText = "待连接"
		proxyDetail = "服务已运行，等待控制通道或启动指令"
		proxyColor = colorWarning
	} else if scmErr == nil && scm.Installed {
		proxyText = "未连接"
		proxyDetail = "服务未运行"
		proxyColor = colorMuted
	} else {
		proxyText = "不可用"
		proxyDetail = "请先完成服务安装与状态检查"
		proxyColor = colorMuted
	}

	a.mw.Synchronize(func() {
		a.setStatusText(a.headerStatus, headerText, headerColor)
		a.setStatusText(a.serviceBadge, serviceText, serviceColor)
		a.setStatusText(a.proxyBadge, proxyText, proxyColor)
		if a.serviceDetail != nil {
			_ = a.serviceDetail.SetText(serviceDetail)
		}
		if a.proxyDetail != nil {
			_ = a.proxyDetail.SetText(proxyDetail)
		}
		a.suppressCheckbox = true
		if a.serviceAutoCheck != nil && scmErr == nil {
			a.serviceAutoCheck.SetChecked(scm.AutoStart)
		}
		if a.loginSilentCheck != nil && settingsErr == nil {
			a.loginSilentCheck.SetChecked(settings.SilentStartAtLogin)
		}
		a.suppressCheckbox = false
		a.updateServiceToggleUI(scmErr == nil && scm.Installed, scmErr == nil)
		a.updateToggleProxyUI(proxyRunning)
	})
}

func (a *guiApp) updateServiceToggleUI(installed bool, enabled bool) {
	text := "安装服务"
	if installed {
		text = "卸载服务"
	}
	if a.serviceToggleBtn != nil {
		_ = a.serviceToggleBtn.SetText(text)
		a.serviceToggleBtn.SetEnabled(enabled)
	}
}

func (a *guiApp) updateToggleProxyUI(proxyRunning bool) {
	text := "启动代理"
	if proxyRunning {
		text = "关闭代理"
	}
	if a.toggleProxyBtn != nil {
		_ = a.toggleProxyBtn.SetText(text)
	}
	if a.trayToggleAction != nil {
		_ = a.trayToggleAction.SetText(text)
	}
}

func (a *guiApp) toggleAuthVisibility() {
	a.authTokenVisible = !a.authTokenVisible
	a.updateAuthVisibilityUI()
}

func (a *guiApp) updateAuthVisibilityUI() {
	if a.authTokenEdit != nil {
		a.authTokenEdit.SetPasswordMode(!a.authTokenVisible)
	}
	if a.authToggleBtn != nil {
		if a.authTokenVisible {
			_ = a.authToggleBtn.SetText("隐藏")
		} else {
			_ = a.authToggleBtn.SetText("显示")
		}
	}
}

func (a *guiApp) setStatusText(label *walk.Label, text string, color walk.Color) {
	if label == nil {
		return
	}
	_ = label.SetText(text)
	label.SetTextColor(color)
}

func (a *guiApp) startRefreshLoop() {
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if a.mw != nil {
					a.refreshStatus()
				}
			case <-a.done:
				return
			}
		}
	}()
}

func (a *guiApp) restoreWindow() {
	if a.mw == nil {
		return
	}
	a.mw.Synchronize(func() {
		a.mw.Show()
		_ = a.mw.SetFocus()
	})
}

func (a *guiApp) hideToTray(showBalloon bool) {
	if a.mw == nil {
		return
	}
	a.mw.Synchronize(func() {
		a.mw.Hide()
		if showBalloon && a.notifyIcon != nil {
			_ = a.notifyIcon.ShowInfo(appTitle, "主窗口已隐藏到系统托盘。")
		}
	})
}

func (a *guiApp) exitGUI() {
	a.exiting = true
	if a.mw == nil {
		return
	}
	a.mw.Synchronize(func() {
		a.mw.Close()
	})
}

func (a *guiApp) appendLog(format string, args ...any) {
	if a.logView == nil || a.mw == nil {
		return
	}
	line := fmt.Sprintf("[%s] %s\r\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
	a.mw.Synchronize(func() {
		a.logView.AppendText(line)
	})
}

func (a *guiApp) showError(title string, err error) {
	if err == nil {
		return
	}
	a.appendLog("%s: %v", title, err)
	walk.MsgBox(a.mw, title, err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
}

func defaultIfEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
