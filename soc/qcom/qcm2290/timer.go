// Qualcomm QCM2290 timer support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the qcm2290 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package qcm2290

import (
	_ "unsafe"
)

func initTimers() {
	// The system counter is configured and started by the Qualcomm boot
	// chain; CNTFRQ_EL0 is valid (19.2 MHz), so only the multiplier
	// needs deriving.
	ARM64.InitGenericTimers(0, 0)
}

//go:linkname nanotime runtime/goos.Nanotime
func nanotime() int64 {
	return ARM64.GetTime()
}
