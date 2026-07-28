// Allwinner H616 USB host bring-up (EHCI/OHCI pairs + PHY)
// https://github.com/usbarmory/tamago
//
// Copyright (c) the h616 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package h616

import (
	"time"
	_ "unsafe"

	"github.com/usbarmory/tamago/internal/reg"
)

// The H616 has four USB lanes; the CB1 wires two of them:
//
//   - USB0 (PHY0, dual-routed between the musb OTG and EHCI0/OHCI0):
//     the CM4-connector USB pins. On the Manta M8P this lane reaches
//     the RS2227XN mux -- Type-C (flashing, peripheral) or the FE1.1S
//     hub + STM32G0B1 (normal, host). The vendor DTS comment ("PHY0
//     pins are connected to a USB-C socket") and the shared USBOTG_ID
//     net on the M8P schematic identify the CM4 lane as PHY0, and in
//     host mode PHY0 must be routed to the HCI pair via REG_PHY_OTGCTL.
//   - USB1 (PHY1, plain EHCI1/OHCI1): secondary lane (mainline's
//     manta DTS enables it; kept for carrier boards that use it).
//
// A full-speed-only host uses the OHCI of each pair and leaves the
// EHCI unconfigured (CONFIGFLAG=0) so root ports route to the OHCI
// companion.
const (
	EHCI0_BASE = 0x05101000
	OHCI0_BASE = 0x05101400
	EHCI1_BASE = 0x05200000
	OHCI1_BASE = 0x05200400

	// PHY control block (usbphy reg "phy_ctrl")
	phyCtrlBase = 0x05100400
	phyISCR     = phyCtrlBase + 0x00 // REG_ISCR
	phyCtlA33   = phyCtrlBase + 0x10 // REG_PHYCTL_A33
	phyOTGCtl   = phyCtrlBase + 0x20 // REG_PHY_OTGCTL

	iscrDPDMPullup = 1 << 16
	iscrIDPullup   = 1 << 17
	iscrForceIDLow = 2 << 14 // host mode
	iscrForceIDMsk = 3 << 14

	phyCtlVBUSVLDEXT = 1 << 5
	phyCtlSIDDQ      = 1 << 3

	otgctlRouteMUSB = 1 << 0

	// PMU register offsets/bits (phy-sun4i-usb.c)
	pmuPHYCtl   = 0x10    // REG_HCI_PHY_CTL
	pmuICHR8    = 1 << 10 // SUNXI_AHB_ICHR8_EN
	pmuINCR4    = 1 << 9  // SUNXI_AHB_INCR4_BURST_EN
	pmuINCRAlgn = 1 << 8  // SUNXI_AHB_INCRX_ALIGN_EN
	pmuULPIByp  = 1 << 0  // SUNXI_ULPI_BYPASS_EN

	usb2PMU = 0x05310800 // pmu2, for the H616 PHY2-SIDDQ quirk

	// CCU registers (ccu-sun50i-h616.c)
	usb2ClkReg = CCU_BASE + 0xa78 // USB2: PHY2 gate+reset (SIDDQ quirk only)
	usbBGRReg  = CCU_BASE + 0xa8c // USB bus gates (low half) and resets (high)

	// USBn clock register bits
	usbClkOHCIGate = 31 // OHCI 12M gate
	usbClkPHYRst   = 30 // PHY reset (1 = deasserted)
	usbClkPHYGate  = 29 // PHY 24M gate
	usbClkOHCISrc  = 24 // bits 25:24 OHCI 12M source, 00 = 48MHz/4

	bgrEHCI2Gate = 6 // PMU2 access for the PHY2-SIDDQ quirk
	bgrEHCI2Rst  = 22
	bgrOTGGate   = 8  // musb OTG AHB clock: the PHY0 control block
	bgrOTGRst    = 24 // (ISCR/PHYCTL/OTGCTL @ 0x051004xx) lives in it
)

// usbLane describes one EHCI/OHCI pair.
type usbLane struct {
	ehci, ohci, pmu uint32
	clkReg          uint32 // CCU USBn clock register
	gateOHCI        int    // usbBGRReg bit positions
	gateEHCI        int
	rstOHCI         int
	rstEHCI         int
}

var usbLanes = [2]usbLane{
	{EHCI0_BASE, OHCI0_BASE, 0x05101800, CCU_BASE + 0xa70, 0, 4, 16, 20},
	{EHCI1_BASE, OHCI1_BASE, 0x05200800, CCU_BASE + 0xa74, 1, 5, 17, 21},
}

// DMA window for cache-coherent bus-master descriptors (OHCI EDs/TDs,
// HCCA) and bounce buffers: the top 2 MB of the 512 MB bmx RAM claim,
// kept out of runtime.MemRegion() by the board's RamSize and mapped
// Normal Non-cacheable by the arm64 MMU tables (UncachedStart/Size
// below, wired via linkname so the values are set before InitMMU runs).
const (
	UNCACHED_BASE = 0x5fe00000
	UNCACHED_SIZE = 0x200000
)

//go:linkname uncachedStart github.com/usbarmory/tamago/arm64.UncachedStart
var uncachedStart uint64 = UNCACHED_BASE

//go:linkname uncachedSize github.com/usbarmory/tamago/arm64.UncachedSize
var uncachedSize uint64 = UNCACHED_SIZE

var phy2QuirkDone bool

// phy2Quirk pulls PHY2 out of analog power-down: the other H616 PHYs
// need PHY2's bias circuit to function (phy-sun4i-usb.c
// needs_phy2_siddq). Runs once.
func phy2Quirk() {
	if phy2QuirkDone {
		return
	}
	phy2QuirkDone = true

	reg.Set(usb2ClkReg, usbClkPHYGate)
	reg.Set(usb2ClkReg, usbClkPHYRst)
	reg.Set(usbBGRReg, bgrEHCI2Gate) // PMU2 lives in the EHCI2 block
	reg.Set(usbBGRReg, bgrEHCI2Rst)

	time.Sleep(time.Millisecond)
	reg.Clear(usb2PMU+pmuPHYCtl, 3) // SIDDQ
}

// USBHostInit ungates, resets and powers up a USB lane for full-speed
// OHCI host operation, following the mainline phy-sun4i-usb.c
// sun50i_h616_cfg sequence. For lane 0 it additionally performs the
// PHY0 dual-route setup: SIDDQ/VBUSVLDEXT in the common PHYCTL
// register, ID forced low (host), and the OTG mux routed to the HCI
// pair. The lane's EHCI CONFIGFLAG is cleared so its root port is
// owned by the OHCI companion regardless of what earlier boot stages
// did.
func USBHostInit(lane int) {
	l := usbLanes[lane]

	// PHY clock + reset deassert, OHCI 12M source 00 (48 MHz / 4) + gate
	reg.ClearN(l.clkReg, usbClkOHCISrc, 0b11)
	reg.Set(l.clkReg, usbClkPHYGate)
	reg.Set(l.clkReg, usbClkPHYRst)
	reg.Set(l.clkReg, usbClkOHCIGate)

	phy2Quirk()

	// bus clock gates + reset deassert (PMU lives in the EHCI block)
	reg.Set(usbBGRReg, l.gateOHCI)
	reg.Set(usbBGRReg, l.gateEHCI)
	reg.Set(usbBGRReg, l.rstOHCI)
	reg.Set(usbBGRReg, l.rstEHCI)

	time.Sleep(time.Millisecond)

	// PHY out of analog power-down
	reg.Clear(l.pmu+pmuPHYCtl, 3) // SIDDQ

	if lane == 0 {
		// the PHY0 control block sits in the musb OTG's address
		// space: its AHB clock must run to reach these registers
		reg.Set(usbBGRReg, bgrOTGGate)
		reg.Set(usbBGRReg, bgrOTGRst)
		time.Sleep(time.Millisecond)

		// PHY0: SIDDQ clear + external-VBUS-valid in the common
		// PHYCTL register, ID forced low (host role), and the
		// dual-route mux pointed at EHCI0/OHCI0 (not the musb OTG)
		reg.Write(phyCtlA33, reg.Read(phyCtlA33)&^uint32(phyCtlSIDDQ)|phyCtlVBUSVLDEXT)

		iscr := reg.Read(phyISCR)
		iscr |= iscrDPDMPullup | iscrIDPullup
		iscr = iscr&^uint32(iscrForceIDMsk) | iscrForceIDLow
		reg.Write(phyISCR, iscr)

		reg.Write(phyOTGCtl, reg.Read(phyOTGCtl)&^uint32(otgctlRouteMUSB))
	}

	// PHY passby: AHB burst enables + ULPI bypass
	reg.Write(l.pmu, reg.Read(l.pmu)|pmuICHR8|pmuINCR4|pmuINCRAlgn|pmuULPIByp)

	// EHCI unconfigured: CONFIGFLAG (opbase+0x40) = 0 routes the root
	// port to the OHCI companion
	caplength := reg.Read(l.ehci) & 0xff
	reg.Write(l.ehci+caplength+0x40, 0)

	time.Sleep(time.Millisecond)
}

// USB1Init is the previous single-lane entry point, retained for
// compatibility: it brings up lane 1 only.
func USB1Init() { USBHostInit(1) }

// USBReg is one entry of USBDebug's register dump.
type USBReg struct {
	Name string
	Addr uint32
	Val  uint32
}

// USBDebug snapshots the clock/PHY/routing registers relevant to USB
// host bring-up (call after USBHostInit so the blocks are clocked).
func USBDebug() []USBReg {
	regs := []USBReg{
		{"CCU_USB0_CLK", usbLanes[0].clkReg, 0},
		{"CCU_USB1_CLK", usbLanes[1].clkReg, 0},
		{"CCU_USB2_CLK", usb2ClkReg, 0},
		{"CCU_USB_BGR", usbBGRReg, 0},
		{"PHY_ISCR", phyISCR, 0},
		{"PHY_CTL_A33", phyCtlA33, 0},
		{"PHY_OTGCTL", phyOTGCtl, 0},
		{"PMU0", usbLanes[0].pmu, 0},
		{"PMU0_PHY_CTL", usbLanes[0].pmu + pmuPHYCtl, 0},
		{"PMU1", usbLanes[1].pmu, 0},
		{"PMU1_PHY_CTL", usbLanes[1].pmu + pmuPHYCtl, 0},
		{"PMU2_PHY_CTL", usb2PMU + pmuPHYCtl, 0},
	}
	for i := range regs {
		regs[i].Val = reg.Read(regs[i].Addr)
	}
	return regs
}
