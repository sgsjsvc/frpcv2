package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/fatedier/frp/client"
	"github.com/fatedier/frp/pkg/config"
	"github.com/fatedier/frp/pkg/config/source"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/config/v1/validation"
	"github.com/fatedier/frp/pkg/policy/featuregate"
	"github.com/fatedier/frp/pkg/policy/security"
	"github.com/fatedier/frp/pkg/util/log"
)

type ServiceBundle struct {
	Service     *client.Service
	Common      *v1.ClientCommonConfig
	Result      *config.ClientConfigLoadResult
	ConfigPath  string
	IsLegacy    bool
	UnsafeFlags []string
}

type CommonFormData struct {
	ServerAddr        string
	ServerPort        int
	User              string
	AuthMethod        string
	AuthToken         string
	WebServerAddr     string
	WebServerPort     int
	WebServerUser     string
	WebServerPassword string
}

func ValidateClientConfigFile(path string, strict bool, allowUnsafe []string) (*config.ClientConfigLoadResult, error) {
	result, err := config.LoadClientConfigResult(path, strict)
	if err != nil {
		return nil, err
	}
	unsafeFeatures := security.NewUnsafeFeatures(allowUnsafe)
	if _, _, err := filterAndValidate(result, unsafeFeatures); err != nil {
		return nil, err
	}
	return result, nil
}

func PrepareClientService(path string, strict bool, allowUnsafe []string) (*ServiceBundle, error) {
	result, err := config.LoadClientConfigResult(path, strict)
	if err != nil {
		return nil, err
	}
	if len(result.Common.FeatureGates) > 0 {
		if err := featuregate.SetFromMap(result.Common.FeatureGates); err != nil {
			return nil, err
		}
	}

	unsafeFeatures := security.NewUnsafeFeatures(allowUnsafe)
	proxyCfgs, visitorCfgs, err := filterAndValidate(result, unsafeFeatures)
	if err != nil {
		return nil, err
	}

	aggregator, err := buildAggregator(result, path)
	if err != nil {
		return nil, err
	}
	if err := aggregator.ConfigSource().ReplaceAll(proxyCfgs, visitorCfgs); err != nil {
		return nil, fmt.Errorf("failed to set config source: %w", err)
	}

	log.InitLogger(result.Common.Log.To, result.Common.Log.Level, int(result.Common.Log.MaxDays), result.Common.Log.DisablePrintColor)
	svc, err := client.NewService(client.ServiceOptions{
		Common:                 result.Common,
		ConfigSourceAggregator: aggregator,
		UnsafeFeatures:         unsafeFeatures,
		ConfigFilePath:         path,
	})
	if err != nil {
		return nil, err
	}

	return &ServiceBundle{
		Service:     svc,
		Common:      result.Common,
		Result:      result,
		ConfigPath:  path,
		IsLegacy:    result.IsLegacyFormat,
		UnsafeFlags: append([]string(nil), allowUnsafe...),
	}, nil
}

func RunPreparedService(bundle *ServiceBundle) error {
	if bundle == nil || bundle.Service == nil {
		return fmt.Errorf("service bundle is not initialized")
	}
	if bundle.ConfigPath != "" {
		log.Infof("start frpc service for config file [%s]", bundle.ConfigPath)
		defer log.Infof("frpc service for config file [%s] stopped", bundle.ConfigPath)
	}
	shouldGracefulClose := bundle.Common.Transport.Protocol == "kcp" || bundle.Common.Transport.Protocol == "quic"
	if shouldGracefulClose {
		go handleTermSignal(bundle.Service)
	}
	return bundle.Service.Run(context.Background())
}

func ParseClientConfigContent(content []byte, strict bool) (*v1.ClientConfig, error) {
	cfg := &v1.ClientConfig{}
	if err := config.LoadConfigure(content, cfg, strict, "toml"); err != nil {
		return nil, err
	}
	if err := cfg.ClientCommonConfig.Complete(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func ExtractCommonFormData(cfg *v1.ClientConfig) CommonFormData {
	if cfg == nil {
		return CommonFormData{}
	}
	return CommonFormData{
		ServerAddr:        cfg.ServerAddr,
		ServerPort:        cfg.ServerPort,
		User:              cfg.User,
		AuthMethod:        string(cfg.Auth.Method),
		AuthToken:         cfg.Auth.Token,
		WebServerAddr:     cfg.WebServer.Addr,
		WebServerPort:     cfg.WebServer.Port,
		WebServerUser:     cfg.WebServer.User,
		WebServerPassword: cfg.WebServer.Password,
	}
}

func ApplyCommonFormData(cfg *v1.ClientConfig, form CommonFormData) error {
	if cfg == nil {
		return fmt.Errorf("client config is nil")
	}
	cfg.ServerAddr = form.ServerAddr
	cfg.ServerPort = form.ServerPort
	cfg.User = form.User
	if form.AuthMethod != "" {
		cfg.Auth.Method = v1.AuthMethod(form.AuthMethod)
	}
	if cfg.Auth.Method == v1.AuthMethodToken {
		cfg.Auth.Token = form.AuthToken
	}
	cfg.WebServer.Addr = form.WebServerAddr
	cfg.WebServer.Port = form.WebServerPort
	cfg.WebServer.User = form.WebServerUser
	cfg.WebServer.Password = form.WebServerPassword
	return cfg.ClientCommonConfig.Complete()
}

func MarshalClientConfigToTOML(cfg *v1.ClientConfig) ([]byte, error) {
	if cfg == nil {
		return nil, fmt.Errorf("client config is nil")
	}
	payload, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, err
	}
	return toml.Marshal(raw)
}

func ValidateClientConfigContent(targetPath string, content []byte, strict bool, allowUnsafe []string) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "frpc-validate-*.toml")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	defer tmp.Close()

	if _, err := tmp.Write(content); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	_, err = ValidateClientConfigFile(tmpPath, strict, allowUnsafe)
	return err
}

func WriteValidatedClientConfig(targetPath string, content []byte, strict bool, allowUnsafe []string) error {
	if err := ValidateClientConfigContent(targetPath, content, strict, allowUnsafe); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(targetPath, content, 0o644)
}

func DefaultClientConfigContent() []byte {
	return []byte("serverAddr = \"154.12.16.190\"\nserverPort = 7000\nloginFailExit = false\n\nauth.method = \"token\"\nauth.token = \"sk-962464@zap\"\n")
}

func buildAggregator(result *config.ClientConfigLoadResult, cfgPath string) (*source.Aggregator, error) {
	configSource := source.NewConfigSource()
	aggregator := source.NewAggregator(configSource)
	if result.Common.Store.IsEnabled() {
		storePath := result.Common.Store.Path
		if storePath != "" && cfgPath != "" && !filepath.IsAbs(storePath) {
			storePath = filepath.Join(filepath.Dir(cfgPath), storePath)
		}
		storeSource, err := source.NewStoreSource(source.StoreSourceConfig{Path: storePath})
		if err != nil {
			return nil, fmt.Errorf("failed to create store source: %w", err)
		}
		aggregator.SetStoreSource(storeSource)
	}
	return aggregator, nil
}

func filterAndValidate(result *config.ClientConfigLoadResult, unsafeFeatures *security.UnsafeFeatures) ([]v1.ProxyConfigurer, []v1.VisitorConfigurer, error) {
	proxyCfgs, visitorCfgs := config.FilterClientConfigurers(result.Common, result.Proxies, result.Visitors)
	proxyCfgs = config.CompleteProxyConfigurers(proxyCfgs)
	visitorCfgs = config.CompleteVisitorConfigurers(visitorCfgs)
	warning, err := validation.ValidateAllClientConfig(result.Common, proxyCfgs, visitorCfgs, unsafeFeatures)
	if warning != nil {
		log.Warnf("config warning: %v", warning)
	}
	if err != nil {
		return nil, nil, err
	}
	return proxyCfgs, visitorCfgs, nil
}

func handleTermSignal(svc *client.Service) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	svc.GracefulClose(500 * time.Millisecond)
}
