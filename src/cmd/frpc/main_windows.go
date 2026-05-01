//go:build windows

package main

import (
	"os"

	"github.com/fatedier/frp/cmd/frpc/sub"
	"github.com/fatedier/frp/cmd/frpc/winapp"
	"github.com/fatedier/frp/pkg/util/system"
	_ "github.com/fatedier/frp/web/frpc"
)

func main() {
	system.EnableCompatibilityMode()
	if handled, code := winapp.HandleWindowsEntry(os.Args[1:]); handled {
		if code != 0 {
			os.Exit(code)
		}
		return
	}
	sub.Execute()
}
