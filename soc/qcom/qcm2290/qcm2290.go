// Qualcomm QCM2290 (and QRB2210) configuration and support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the qcm2290 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

// Package qcm2290 provides hardware initialization and drivers for the
// Qualcomm QCM2290/QRB2210 SoC (4x Cortex-A53, arm64).
//
// The addresses and peripheral facts derive from the mainline Linux device
// tree (arch/arm64/boot/dts/qcom/agatti.dtsi) as the platform has no public
// reference manual.
//
// This package is only meant to be used with `GOOS=tamago GOARCH=arm64` as
// supported by the TamaGo framework for bare metal Go, see
// https://github.com/usbarmory/tamago.
package qcm2290

import (
	_ "unsafe"

	"github.com/usbarmory/tamago/arm64"
	"github.com/usbarmory/tamago/arm64/gic"
)

// Peripheral base addresses (agatti.dtsi)
const (
	// DDR base
	DDR_BASE = 0x40000000

	// GENI serial engine, QUP0 SE4: the boot-chain debug UART
	UART4_BASE = 0x04a90000

	// GENI serial engine, QUP0 SE5: the header-exposed SPI
	SPI5_BASE = 0x04a94000

	// APSS watchdog (qcom,kpss-wdt register layout)
	WDT_BASE = 0x0f017000

	// GICv3
	GICD_BASE = 0x0f200000
	GICR_BASE = 0x0f300000
)

//go:linkname ramStackOffset runtime/goos.RamStackOffset
var ramStackOffset uint64 = 0x100

// Peripheral instances
var (
	ARM64 = &arm64.CPU{}

	GIC = &gic.GIC{
		GICD: GICD_BASE,
		GICR: GICR_BASE,
	}

	// UART4 is the debug console (115200n8, configured by the boot
	// chain)
	UART4 = &GENIUART{Base: UART4_BASE}

	// SPI5 is the header-exposed SPI master (GPIO 14-17)
	SPI5 = &GENISPI{Base: SPI5_BASE}

	// WDT is the application processor watchdog
	WDT = &Watchdog{Base: WDT_BASE}
)

// Init takes care of the lower level SoC initialization triggered early in
// runtime setup (e.g. runtime/goos.Hwinit1).
func Init() {
	ARM64.Init()
	ARM64.EnableCache()

	// The boot chain may leave the watchdog armed; quiesce it before
	// anything else can stall.
	WDT.Disable()

	initTimers()
}
