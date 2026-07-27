// BCM2835 SoC reset (watchdog)
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package bcm2835

import "github.com/usbarmory/tamago/internal/reg"

// Power-management block registers (reset via the watchdog).
const (
	pmBase          = 0x100000
	pmRSTC          = 0x1c
	pmWDOG          = 0x24
	pmPassword      = 0x5a000000
	pmRSTCWrcfgClr  = 0xffffffcf
	pmRSTCFullReset = 0x00000020
)

// Reset triggers a full SoC reset via the watchdog; the VideoCore then reloads
// the kernel from the SD card. It does not return.
func Reset() {
	rstc := peripheralBase + pmBase + pmRSTC
	wdog := peripheralBase + pmBase + pmWDOG

	v := reg.Read(rstc) & pmRSTCWrcfgClr
	reg.Write(wdog, pmPassword|10) // ~10 watchdog ticks
	reg.Write(rstc, pmPassword|v|pmRSTCFullReset)

	for {
	}
}
