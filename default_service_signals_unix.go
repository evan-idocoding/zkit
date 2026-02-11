//go:build unix

package zkit

import (
	"os"
	"syscall"
)

func defaultSignals() []os.Signal {
	return []os.Signal{
		os.Interrupt,    // SIGINT
		syscall.SIGTERM, // graceful termination
	}
}
