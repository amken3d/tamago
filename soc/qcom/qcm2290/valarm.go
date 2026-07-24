// Qualcomm QCM2290 virtual timer alarm
// https://github.com/usbarmory/tamago
//
// Copyright (c) the qcm2290 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package qcm2290

import (
	"math"
)

// defined in valarm.s
func read_cntvct() uint64
func write_cntvtval(val uint32, enable bool)
func read_cntvctl() uint64

// VirtualCount returns the Counter-timer Virtual Count (CNTVCT); the
// delta against Counter() exposes CNTVOFF.
func VirtualCount() uint64 {
	return read_cntvct()
}

// VirtTimerStatus returns CNTV_CTL, whose ISTATUS bit (2) reports an
// expired (firing) virtual timer condition.
func VirtTimerStatus() uint64 {
	return read_cntvctl()
}

// SetVAlarm sets the EL1 virtual timer to the absolute time matching the
// argument nanoseconds value; an interrupt is generated at expiration.
//
// The virtual timer is used instead of the arm64 package's physical timer
// alarm (CNTP): under the Qualcomm hypervisor the physical timer PPI is
// not delivered to non-secure EL1 — the stock kernel selects the virtual
// timer for the same reason. CNTVOFF is zero here, so the virtual count
// equals the physical count used by the timebase.
func SetVAlarm(ns int64) {
	if ns == 0 {
		write_cntvtval(0, false)
		return
	}

	if ARM64.TimerMultiplier == 0 {
		return
	}

	// float division: truncating the multiplier to an integer (52 vs
	// 52.083 ns/tick at 19.2 MHz) makes the absolute tick target late
	// by ~0.16% of total counter uptime — hundreds of ms within minutes
	set := uint64(float64(ns-ARM64.TimerOffset) / ARM64.TimerMultiplier)
	now := read_cntvct()
	cnt := set - now

	if set <= now {
		cnt = 1
	} else if cnt > math.MaxInt32 {
		cnt = math.MaxInt32
	}

	write_cntvtval(uint32(cnt), true)
}
