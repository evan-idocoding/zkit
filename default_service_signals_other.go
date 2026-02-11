//go:build !unix

package zkit

import "os"

func defaultSignals() []os.Signal {
	// Best effort: at least support os.Interrupt.
	return []os.Signal{os.Interrupt}
}
