package client

import "github.com/fatedier/frp/client/configmgmt"

func NewConfigManager(svr *Service) configmgmt.ConfigManager {
	return newServiceConfigManager(svr)
}
