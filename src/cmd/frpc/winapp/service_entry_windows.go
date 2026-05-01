//go:build windows

package winapp

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/svc"
)

func HandleWindowsEntry(args []string) (bool, int) {
	switch {
	case hasArg(args, "--service"):
		return true, exitFromErr(runServiceEntry())
	case hasArg(args, "--install-service"):
		return true, exitFromErr(installService())
	case hasArg(args, "--uninstall-service"):
		return true, exitFromErr(uninstallService())
	case hasArg(args, "--start-service"):
		return true, exitFromErr(startServiceProcessElevated())
	case hasPrefixArg(args, "--set-service-startup="):
		mode := strings.TrimPrefix(firstMatchingArg(args, "--set-service-startup="), "--set-service-startup=")
		return true, exitFromErr(setServiceStartup(mode))
	case hasArg(args, "--silent"):
		return true, exitFromErr(RunGUI(true))
	case len(args) == 0:
		return true, exitFromErr(RunGUI(false))
	default:
		return false, 0
	}
}

func runServiceEntry() error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if !isService {
		host := newServiceHost()
		if err := host.start(); err != nil {
			return err
		}
		defer host.shutdown()

		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		<-ch
		return nil
	}
	return svc.Run(serviceName, &serviceHandler{})
}

type serviceHandler struct{}

func (h *serviceHandler) Execute(_ []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}

	host := newServiceHost()
	if err := host.start(); err != nil {
		return true, 1
	}
	defer host.shutdown()

	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for c := range r {
		switch c.Cmd {
		case svc.Interrogate:
			changes <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			changes <- svc.Status{State: svc.StopPending}
			return false, 0
		}
	}
	return false, 0
}

func exitFromErr(err error) int {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func hasArg(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
}

func hasPrefixArg(args []string, prefix string) bool {
	return firstMatchingArg(args, prefix) != ""
}

func firstMatchingArg(args []string, prefix string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return arg
		}
	}
	return ""
}
