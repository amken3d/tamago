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
	pmRSTS          = 0x20
	pmWDOG          = 0x24
	pmPassword      = 0x5a000000
	pmRSTCWrcfgClr  = 0xffffffcf
	pmRSTCFullReset = 0x00000020
	pmRSTSPartition = 0x00000555 // boot-partition select bits (0x555 = 63 = halt)
)

// Reset triggers a full SoC reset via the watchdog, selecting boot partition 0
// so the VideoCore reloads the kernel from the SD card. It does not return.
//
// Note: on boards where firmware has left the SD card in a state the boot ROM
// cannot re-read (e.g. the card was switched to SPI mode and not power-cycled),
// this will halt rather than reboot. It does not return.
func Reset() {
	rsts := peripheralBase + pmBase + pmRSTS
	rstc := peripheralBase + pmBase + pmRSTC
	wdog := peripheralBase + pmBase + pmWDOG

	reg.Write(rsts, pmPassword|(reg.Read(rsts)&^pmRSTSPartition)) // partition 0

	reg.Write(wdog, pmPassword|10) // ~10 watchdog ticks
	v := reg.Read(rstc) & pmRSTCWrcfgClr
	reg.Write(rstc, pmPassword|v|pmRSTCFullReset)

	for {
	}
}

// PowerOff halts the SoC by selecting boot partition 63 (the VideoCore "halt"
// partition) and tripping the watchdog. The board powers down and stays off
// until the power is cycled. It does not return.
func PowerOff() {
	rsts := peripheralBase + pmBase + pmRSTS
	rstc := peripheralBase + pmBase + pmRSTC
	wdog := peripheralBase + pmBase + pmWDOG

	reg.Write(rsts, pmPassword|reg.Read(rsts)|pmRSTSPartition) // partition 63 = halt

	reg.Write(wdog, pmPassword|10)
	v := reg.Read(rstc) & pmRSTCWrcfgClr
	reg.Write(rstc, pmPassword|v|pmRSTCFullReset)

	for {
	}
}
