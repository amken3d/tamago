// BCM2835 SoC support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

// Package bcm2835 provides support to Go bare metal unikernels written using
// the TamaGo framework on BCM2835/BCM2836/BCM2837 SoCs.
//
// This package is only meant to be used with `GOOS=tamago` as supported by
// the TamaGo framework for bare metal Go, see
// https://github.com/usbarmory/tamago.
package bcm2835

// DRAM_FLAG_NOCACHE disables caching by setting to high bits
const DRAM_FLAG_NOCACHE = 0xC0000000

// peripheralBase represents the (remapped) peripheral base address, it varies
// by model and it is therefore initialized (see Init) by individual board
// packages.
var peripheralBase uint32

// PeripheralAddress returns the absolute address for a peripheral. The Pi
// boards map 'bus addresses' to board specific base addresses but with
// consistent layout otherwise.
func PeripheralAddress(offset uint32) uint32 {
	return peripheralBase + offset
}
