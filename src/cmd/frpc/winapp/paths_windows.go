//go:build windows

package winapp

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const (
	serviceName        = "frpc"
	serviceDisplayName = "frpc Service"
	pipeName           = `\\.\pipe\frpc-service-ipc`
)

type guiSettings struct {
	SilentStartAtLogin bool `json:"silentStartAtLogin"`
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
