// Allwinner H616 timer support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the h616 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package h616

import (
	_ "unsafe"
)

func initTimers() {
	// The architected system counter is configured by the boot chain
	// (TF-A); CNTFRQ_EL0 is valid (24 MHz), so only the multiplier
	// needs deriving.
	ARM64.InitGenericTimers(0, 0)
}

//go:linkname nanotime runtime/goos.Nanotime
func nanotime() int64 {
	return ARM64.GetTime()
}
