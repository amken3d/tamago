// BCM2835 SoC support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build !linknanotime

package bcm2835

import (
	_ "unsafe"
)

// The runtime timebase defaults to the BCM2835 free-running 1 MHz system
// timer. Boards whose SoC provides ARMv7 Generic Timers (e.g. BCM2836 and
// later) can build with the `linknanotime` tag and provide their own
// runtime/goos.Nanotime (e.g. arm.CPU.GetTime, 52 ns resolution at the
// 19.2 MHz reference), keeping a single clock domain with arm.CPU.SetAlarm.

//go:linkname nanotime runtime/goos.Nanotime
func nanotime() int64 {
	return int64(float64(read_systimer())*ARM.TimerMultiplier) + ARM.TimerOffset
}

// setTimerMultiplier adjusts the ARM processor instance timer multiplier to
// the system timer frequency.
func setTimerMultiplier() {
	ARM.TimerMultiplier = float64(refFreq) / float64(SysTimerFreq)
}
