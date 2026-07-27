// Allwinner H616 watchdog
// https://github.com/usbarmory/tamago
//
// Copyright (c) the h616 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package h616

import (
	"github.com/usbarmory/tamago/internal/reg"
)

// Watchdog registers (sun6i layout, H616 user manual 9.13; base WDT_BASE).
const (
	wdogCTRL = 0x10
	wdogCFG  = 0x14
	wdogMODE = 0x18

	// CTRL restart key (bits 12:1) with restart bit
	wdogKey = 0xa57<<1 | 1

	// CFG: 1 = whole system reset
	wdogCfgSystem = 0x1

	// MODE: interval (bits 7:4, 0 = 0.5 s) | enable (bit 0)
	wdogEnable = 0x1
)

// WDTReset requests a whole-system reset through the watchdog with the
// shortest interval (0.5 s). It does not require secure-world services
// (unlike the PSCI reset path) and does not return on success.
func WDTReset() {
	reg.Write(WDT_BASE+wdogCFG, wdogCfgSystem)
	reg.Write(WDT_BASE+wdogMODE, wdogEnable)
	// restart the counter for a full interval
	reg.Write(WDT_BASE+wdogCTRL, wdogKey)

	for {
	}
}