// BCM2835 SoC support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build arm64

package bcm2835

import (
	_ "unsafe"

	"github.com/usbarmory/tamago/arm64"
)

// ARM64 processor instance
var ARM64 = &arm64.CPU{}

//go:linkname ramStackOffset runtime/goos.RamStackOffset
var ramStackOffset uint64 = 0x100

//go:linkname nanotime runtime/goos.Nanotime
func nanotime() int64 {
	return ARM64.GetTime()
}

// Init takes care of the lower level initialization triggered early in runtime
// setup (e.g. runtime/goos.Hwinit1).
func Init(base uint32) {
	peripheralBase = base

	ARM64.Init()
	ARM64.EnableCache()

	// On BCM2837/BCM2710A1 the firmware sets CNTFRQ_EL0 to the correct
	// frequency (19.2 MHz). Passing 0 for freq skips writing it and just
	// reads the value already set.
	ARM64.InitGenericTimers(0, 0)

	// initialize serial console
	MiniUART.Init()
}
