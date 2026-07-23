// BCM2835 SoC support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

// Package bcm2835 provides support to Go bare metal unikernels written using
// the TamaGo framework on BCM2835/BCM2836 SoCs.
//
// This package is only meant to be used with `GOOS=tamago GOARCH=arm` as
// supported by the TamaGo framework for bare metal Go, see
// https://github.com/usbarmory/tamago.
package bcm2835

import (
	_ "unsafe"

	"github.com/usbarmory/tamago/arm"
)

// nanos - should be same value as arm/timer.go refFreq
const refFreq int64 = 1e9

// DRAM_FLAG_NOCACHE disables caching by setting to high bits
const DRAM_FLAG_NOCACHE = 0xC0000000

// peripheralBase represents the (remapped) peripheral base address, it varies
// by model and it is therefore initialized (see Init) by individual board
// packages.
var peripheralBase uint32

// ARM processor instance
var ARM = &arm.CPU{
	// required before Init()
	TimerMultiplier: 1,
}

//go:linkname ramStackOffset runtime/goos.RamStackOffset
var ramStackOffset uint32 = 0x100000 // 1 MB

// Init0 performs the CPU-level initialization triggered before runtime setup
// (pre World start), to be called from a board package runtime/goos.Hwinit0
// hook. The MMU and caches are enabled here, before runtime.check: on
// Cortex-A53 (Pi Zero 2 W) the 64-bit atomics used by runtime.check
// (LDREX/STREX) only work on cacheable normal memory. The peripheral base is
// also set here, as runtime setup itself uses SoC peripherals before Hwinit1
// (e.g. the RNG for schedinit's randinit).
func Init0(base uint32) {
	peripheralBase = base

	// The default reserved-area location (RamStart = 0x0) cannot be used
	// on the Raspberry Pi: the firmware parks CPU cores 1-3 in a spin-loop
	// stub at 0x0-0x100, which the vector table would overwrite. Place the
	// 64 kB reserved area at 0x10000 instead; the image TEXT is linked at
	// 0x20000 to leave room (see the application Makefile).
	arm.SetVectorTableStart(0x10000)

	// The Pi firmware enters the kernel in TrustZone Normal World; skip
	// the SCR-read trap probe in arm.NonSecure (secure-only registers such
	// as SCR/MVBAR must not be touched).
	arm.SetNonSecure()

	ARM.InitEarly()
	ARM.EnableVFP()

	// Coherency (CPUECTLR.SMPEN) is already enabled by the Pi firmware's
	// boot stub (armstub7.S) before the kernel is entered. TamaGo's
	// EnableSMP writes the Cortex-A7 ACTLR.SMP bit, which does not exist
	// on the Cortex-A53 and may trap from the non-secure world — skip it.

	// MMU initialization is required to take advantage of data cache
	ARM.InitMMU()
	ARM.EnableCache()
}

// Init takes care of the lower level initialization triggered early in runtime
// setup (e.g. runtime/goos.Hwinit1).
func Init(base uint32) {
	peripheralBase = base

	// deferred from hwinit0: assigning these hooks allocates, which
	// requires the runtime to be initialized
	ARM.InitGoosHooks()

	setTimerMultiplier()

	// initialize serial console
	MiniUART.Init()
}

// PeripheralAddress returns the absolute address for a peripheral. The Pi
// boards map 'bus addresses' to board specific base addresses but with
// consistent layout otherwise.
func PeripheralAddress(offset uint32) uint32 {
	return peripheralBase + offset
}
