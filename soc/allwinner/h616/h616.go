// Allwinner H616 configuration and support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the h616 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

// Package h616 provides hardware initialization and drivers for the
// Allwinner H616 SoC (4x Cortex-A53, arm64, GIC-400).
//
// The addresses and peripheral facts derive from the mainline Linux device
// tree (arch/arm64/boot/dts/allwinner/sun50i-h616.dtsi) and the H616 user
// manual.
//
// The package assumes the mainline boot chain (boot0/SPL -> TF-A ->
// U-Boot) has run: DRAM is initialized, CNTFRQ is programmed (24 MHz),
// PSCI is available via SMC to BL31 (resident in SRAM, not DRAM), and the
// debug UART is muxed and clocked.
//
// This package is only meant to be used with `GOOS=tamago GOARCH=arm64` as
// supported by the TamaGo framework for bare metal Go, see
// https://github.com/usbarmory/tamago.
package h616

import (
	_ "unsafe"

	"github.com/usbarmory/tamago/arm64"
)

// Peripheral base addresses (sun50i-h616.dtsi)
const (
	// DDR base
	DDR_BASE = 0x40000000

	// CCU (clock controller)
	CCU_BASE = 0x03001000

	// PIO (pin controller, banks PA..PI)
	PIO_BASE = 0x0300b000

	// GIC-400 (GICv2)
	GICD_BASE = 0x03021000
	GICC_BASE = 0x03022000

	// UART0: the debug console (PH0/PH1, configured by U-Boot)
	UART0_BASE = 0x05000000

	// Watchdog (sun6i layout)
	WDT_BASE = 0x030090a0
)

//go:linkname ramStackOffset runtime/goos.RamStackOffset
var ramStackOffset uint64 = 0x100

// Peripheral instances
var (
	ARM64 = &arm64.CPU{}

	// UART0 is the debug console (115200n8, configured by U-Boot)
	UART0 = &UART{Base: UART0_BASE}
)

// Init takes care of the lower level SoC initialization triggered early in
// runtime setup (e.g. runtime/goos.Hwinit1).
func Init() {
	ARM64.Init()
	ARM64.EnableCache()

	initTimers()
}
