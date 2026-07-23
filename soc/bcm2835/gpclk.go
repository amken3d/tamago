// BCM2835 SoC general purpose clock (GPCLK) support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package bcm2835

import (
	"fmt"

	"github.com/usbarmory/tamago/internal/reg"
)

// Clock manager registers (BCM2835-ARM-Peripherals errata and Linux
// clk-bcm2835; the GP clocks feed GPIO alternate functions, e.g. the
// wireless chip LPO sleep clock on GPCLK2)
const (
	CM_BASE = 0x101000

	CM_GP0CTL = CM_BASE + 0x70
	CM_GP0DIV = CM_BASE + 0x74
	CM_GP1CTL = CM_BASE + 0x78
	CM_GP1DIV = CM_BASE + 0x7c
	CM_GP2CTL = CM_BASE + 0x80
	CM_GP2DIV = CM_BASE + 0x84

	CM_PASSWD = 0x5a << 24

	CM_CTL_MASH_1  = 1 << 9
	CM_CTL_BUSY    = 1 << 7
	CM_CTL_ENAB    = 1 << 4
	CM_CTL_SRC_OSC = 1

	// crystal oscillator (19.2 MHz on BCM2835-BCM2710 boards)
	OSC_FREQ = 19200000
)

// EnableGPCLK programs and enables general purpose clock n (0-2) at the
// argument frequency, sourced from the crystal oscillator with 1-stage
// MASH fractional division. The matching GPIO must be muxed to its
// GPCLK alternate function separately.
func EnableGPCLK(n int, hz uint32) error {
	if n < 0 || n > 2 || hz == 0 || hz > OSC_FREQ/2 {
		return fmt.Errorf("invalid GPCLK%d frequency %d", n, hz)
	}

	ctl := PeripheralAddress(uint32(CM_GP0CTL + 8*n))
	div := PeripheralAddress(uint32(CM_GP0DIV + 8*n))

	// stop the clock and wait for the generator to go idle
	reg.Write(ctl, CM_PASSWD|CM_CTL_SRC_OSC)

	for reg.Read(ctl)&CM_CTL_BUSY != 0 {
	}

	// fixed point 12.12 divider
	divi := OSC_FREQ / hz
	divf := uint32((uint64(OSC_FREQ%hz) << 12) / uint64(hz))

	reg.Write(div, CM_PASSWD|divi<<12|divf)
	reg.Write(ctl, CM_PASSWD|CM_CTL_MASH_1|CM_CTL_SRC_OSC)
	reg.Write(ctl, CM_PASSWD|CM_CTL_MASH_1|CM_CTL_SRC_OSC|CM_CTL_ENAB)

	return nil
}
