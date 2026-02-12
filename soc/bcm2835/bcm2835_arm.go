// BCM2835 SoC support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build arm

package bcm2835

import (
	_ "unsafe"

	"github.com/usbarmory/tamago/arm"
)

// nanos - should be same value as arm/timer.go refFreq
const refFreq int64 = 1e9

// ARM processor instance
var ARM = &arm.CPU{
	// required before Init()
	TimerMultiplier: 1,
}

//go:linkname ramStackOffset runtime/goos.RamStackOffset
var ramStackOffset uint32 = 0x100000 // 1 MB

//go:linkname nanotime runtime/goos.Nanotime
func nanotime() int64 {
	return read_systimer()*ARM.TimerMultiplier + ARM.TimerOffset
}

// Init takes care of the lower level initialization triggered early in runtime
// setup (e.g. runtime/goos.Hwinit1).
func Init(base uint32) {
	peripheralBase = base

	ARM.Init(ramStart)
	ARM.EnableVFP()

	// required when booting in SDP mode
	ARM.EnableSMP()

	// MMU initialization is required to take advantage of data cache
	ARM.InitMMU()
	ARM.EnableCache()

	ARM.TimerMultiplier = refFreq / SysTimerFreq

	// initialize serial console
	MiniUART.Init()
}
