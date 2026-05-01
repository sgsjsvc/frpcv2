//go:build windows

package winapp

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fatedier/frp/cmd/frpc/runtime"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

type scmStatus struct {
	Installed bool
	Running   bool
	AutoStart bool
	PID       uint32
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
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	if err := ensureParentDir(machineConfigPath()); err != nil {
		return err
	}
	if _, err := os.Stat(machineConfigPath()); os.IsNotExist(err) {
		if err := os.WriteFile(machineConfigPath(), runtime.DefaultClientConfigContent(), 0o644); err != nil {
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
		if status, queryErr := service.Query(); queryErr == nil && status.State != svc.Running {
			if err := service.Start(); err != nil {
				return err
			}
		}
		return waitForServiceState(service, svc.Running, 10*time.Second)
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

	if err := service.Start(); err != nil {
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

func startServiceProcessElevated() error {
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

	status, err := service.Query()
	if err == nil && status.State == svc.Running {
		return nil
	}
	if err := service.Start(); err != nil && !strings.Contains(strings.ToLower(err.Error()), "already running") {
		return err
	}
	return waitForServiceState(service, svc.Running, 10*time.Second)
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

func setLoginSilentStart(enabled bool) error {
	settings, err := loadGUISettings()
	if err != nil {
		return err
	}
	settings.SilentStartAtLogin = enabled
	if err := saveGUISettings(settings); err != nil {
		return err
	}

	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	runKey := `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	valueName := "frpc-gui"
	if enabled {
		cmdValue := fmt.Sprintf("\"%s\" --silent", filepath.Clean(exePath))
		command := exec.Command("reg", "add", runKey, "/v", valueName, "/t", "REG_SZ", "/d", cmdValue, "/f")
		return command.Run()
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

func runElevated(args ...string) error {
	exePath, err := os.Executable()
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
