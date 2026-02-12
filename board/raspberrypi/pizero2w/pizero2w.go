// Raspberry Pi Zero 2W support for tamago/arm64
// https://github.com/usbarmory/tamago
//
// Copyright (c) the pizero2w package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

// Package pizero2w provides hardware initialization, automatically on import,
// for the Raspberry Pi Zero 2W single board computer.
//
// The Pi Zero 2W uses the BCM2710A1 SoC (same die as BCM2837) with a
// quad-core ARM Cortex-A53 (ARMv8-A) at 1 GHz and 512 MB LPDDR2.
//
// This package is only meant to be used with `GOOS=tamago GOARCH=arm64` as
// supported by the TamaGo framework for bare metal Go, see
// https://github.com/usbarmory/tamago.
package pizero2w

import (
	_ "unsafe"

	pi "github.com/usbarmory/tamago/board/raspberrypi"
	"github.com/usbarmory/tamago/soc/bcm2835"
)

// On the BCM2837/BCM2710A1 peripheral addresses are remapped from their
// hardware 'bus' address to the 0x3f000000 'physical' address. This is the
// same as the Pi 2 and Pi 3.
const peripheralBase = 0x3f000000

type board struct{}

// Board provides access to the capabilities of the Pi Zero 2W.
var Board pi.Board = &board{}

// Init takes care of the lower level initialization triggered early in runtime
// setup (post World start).
//
//go:linkname Init runtime/goos.Hwinit1
func Init() {
	// Defer to generic BCM2835 initialization, with BCM2837/BCM2710A1
	// peripheral base address.
	bcm2835.Init(peripheralBase)
}
