// Analogix ANX7625 MIPI-DSI to DisplayPort bridge driver
// https://github.com/usbarmory/tamago
//
// Copyright (c) the qcm2290 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package qcm2290

import (
	"errors"
	"fmt"
)

// ANX7625 register pages, 7-bit I2C addresses
// (drivers/gpu/drm/bridge/analogix/anx7625.{c,h})
const (
	anxTCPC = 0x2c
	anxRXP0 = 0x3f
	anxRXP1 = 0x42
	anxRXP2 = 0x2a
	anxTXP0 = 0x38
	anxTXP1 = 0x3d
	anxTXP2 = 0x39
)

// ANX7625 drives the bridge over I2C1. On the UNO Q the chip has no
// reset/enable lines: it is always powered (it is also the USB-C PD
// sink controller) and its OCM firmware boots from flash on its own
// whenever a cable is attached. Configuration is 1080p60 RGB888 over
// 4-lane DSI, replayed from the mainline driver's programming with the
// Arduino board's swing values.
type ANX7625 struct {
	I2C *GENII2C

	// lastPage tracks the register bank for the access race
	// workaround between page switches.
	lastPage uint8
}

// pageWorkaroundReg returns the reserved offset written on page
// switches (i2c_access_workaround).
func pageWorkaroundReg(page uint8) uint8 {
	switch page {
	case anxTCPC:
		return 0x00
	case anxTXP0:
		return 0xd1
	case anxTXP1:
		return 0x60
	case anxRXP0:
		return 0x39
	case anxRXP1:
		return 0x7f
	default: // RX_P2, TX_P2
		return 0x00
	}
}

func (a *ANX7625) switchPage(page uint8) error {
	if page == a.lastPage {
		return nil
	}

	if err := a.I2C.Write(page, []byte{pageWorkaroundReg(page), 0x00}, true); err != nil {
		return err
	}

	a.lastPage = page
	return nil
}

func (a *ANX7625) wr(page, reg, val uint8) error {
	if err := a.switchPage(page); err != nil {
		return fmt.Errorf("page %#x: %v", page, err)
	}

	return a.I2C.Write(page, []byte{reg, val}, true)
}

func (a *ANX7625) rd(page, reg uint8) (uint8, error) {
	if err := a.switchPage(page); err != nil {
		return 0, fmt.Errorf("page %#x: %v", page, err)
	}

	var b [1]byte

	if err := a.I2C.ReadReg(page, reg, b[:]); err != nil {
		return 0, err
	}

	return b[0], nil
}

func (a *ANX7625) rmw(page, reg, clear, set uint8) error {
	v, err := a.rd(page, reg)

	if err != nil {
		return err
	}

	return a.wr(page, reg, v&^clear|set)
}

// WaitOCM confirms the on-chip firmware has booted: XTAL select, then
// FLASH_LOAD_STA bit7. The OCM only wakes with a USB-C sink attached.
func (a *ANX7625) WaitOCM(spins int) error {
	// XTAL_FRQ_SEL = 27 MHz
	if err := a.wr(anxRXP0, 0x3f, 0x80); err != nil {
		return fmt.Errorf("OCM asleep (RX_P0 NAK): %v", err)
	}

	for i := 0; i < spins; i++ {
		if v, err := a.rd(anxRXP0, 0x05); err == nil && v&0x80 != 0 {
			return nil
		}
	}

	return errors.New("OCM flash load timeout")
}

// ChipID returns the product ID (expect 0x7625).
func (a *ANX7625) ChipID() (uint16, error) {
	l, err := a.rd(anxTCPC, 0x02)

	if err != nil {
		return 0, err
	}

	h, err := a.rd(anxTCPC, 0x03)

	if err != nil {
		return 0, err
	}

	return uint16(h)<<8 | uint16(l), nil
}

// WaitHPD polls SYSTEM_STATUS until the monitor asserts hotplug.
func (a *ANX7625) WaitHPD(spins int) error {
	// HPD source ready + debounce timer (2 ms @ 27 MHz)
	for i := 0; i < spins; i++ {
		if v, err := a.rd(anxRXP0, 0x49); err == nil && v&0x40 != 0 {
			break
		}
	}

	a.wr(anxTXP2, 0xea, 0xf0)
	a.wr(anxTXP2, 0xeb, 0xd2)
	a.wr(anxTXP2, 0xec, 0x00)

	for i := 0; i < spins; i++ {
		if v, err := a.rd(anxRXP0, 0x45); err == nil && v&0x80 != 0 {
			return nil
		}
	}

	return errors.New("no HPD from monitor")
}

// auxWrite performs a native AUX write of one byte to a DPCD address.
func (a *ANX7625) auxWrite(addr uint32, val uint8) error {
	a.wr(anxRXP0, 0x27, 0x08) // len 1, native write
	a.wr(anxRXP0, 0x11, uint8(addr))
	a.wr(anxRXP0, 0x12, uint8(addr>>8))
	a.wr(anxRXP0, 0x13, uint8(addr>>16))
	a.wr(anxRXP0, 0x15, val)

	if err := a.rmw(anxRXP0, 0x14, 0, 0x10); err != nil { // OP_EN
		return err
	}

	for i := 0; i < 100000; i++ {
		v, err := a.rd(anxRXP0, 0x14)

		if err != nil {
			return err
		}

		if v&0x10 == 0 {
			if v&0x0f != 0 {
				return fmt.Errorf("AUX error %#x", v)
			}

			return nil
		}
	}

	return errors.New("AUX timeout")
}

// StartDP runs the on-HPD housekeeping, swing configuration, and DPCD
// power-up that precede DSI configuration.
func (a *ANX7625) StartDP() error {
	a.wr(anxTCPC, 0xcc, 0xff)      // clear INTR_ALERT_1
	a.wr(anxRXP0, 0x44, 0x00)      // clear INTERFACE_CHANGE_INT
	a.rmw(anxRXP1, 0xee, 0x60, 0)  // HDCP off
	a.rmw(anxRXP1, 0xec, 0, 0x10)  // try-auth flag
	a.rmw(anxRXP1, 0xff, 0, 0x01)  // notify OCM

	// DP swing, lanes 0/1 (board DTS values)
	for i, v := range []uint8{0x14, 0x54, 0x64, 0x74} {
		a.wr(anxTXP1, uint8(i), v)
		a.wr(anxTXP1, uint8(0x14+i), v)
	}

	if err := a.auxWrite(0x600, 0x01); err != nil { // DPCD SET_POWER D0
		return err
	}

	return a.rmw(anxRXP1, 0xee, 0x60, 0) // HDCP off again
}

// anxRXP1Snap replays the MIPI receiver configuration page verbatim
// from a live-Linux 1080p60 dump (config registers only; status,
// error-counter, and OCM-owned registers excluded). It includes the
// analog block 0x10-0x1a and lane control 0x05=0x4f that the mainline
// programming recipe does not cover.
var anxRXP1Snap = []struct{ reg, val uint8 }{
	{0x03, 0x18}, {0x05, 0x4f}, {0x07, 0x22}, {0x08, 0x10},
	{0x09, 0x02}, {0x0a, 0x02}, {0x0b, 0x00}, {0x0c, 0x02},
	{0x0d, 0x02}, {0x0e, 0x02},
	{0x10, 0x68}, {0x11, 0x68}, {0x12, 0x68}, {0x13, 0x68},
	{0x14, 0x68}, {0x15, 0x51}, {0x16, 0x25}, {0x17, 0xa0},
	{0x18, 0x80}, {0x19, 0x80}, {0x1a, 0xaa}, {0x1b, 0x3d},
	{0x1c, 0xa1}, {0x1d, 0x03},
	{0x1e, 0xb0}, {0x1f, 0x00}, {0x20, 0x00}, // PLL M = 0xb00000
	{0x21, 0x08}, {0x22, 0x00}, {0x23, 0x00}, // PLL N = 0x080000
	{0x24, 0x04}, {0x25, 0x49}, {0x26, 0x14}, {0x27, 0x40},
	{0x33, 0x34}, {0x34, 0x58}, {0x35, 0x15}, {0x37, 0xf2},
	{0x38, 0xe0}, {0x3b, 0x02}, {0x3c, 0x14}, {0x3d, 0x20},
	{0x47, 0x38}, {0x48, 0x0e}, {0x4a, 0x10},
	{0x70, 0x72}, {0x71, 0x06}, {0x72, 0x72}, {0x73, 0x06},
	{0x74, 0x72}, {0x75, 0x06}, {0x76, 0x72}, {0x77, 0x06},
	{0x78, 0x72}, {0x79, 0x06}, {0x7a, 0x72}, {0x7b, 0x06},
}

// ConfigDSI programs the MIPI receiver for 1080p60 RGB888 over 4
// lanes (pixel 148.5 MHz), then powers the RX on. Static values are
// replayed verbatim from a live dump; the PLL reset, M/N latch, and
// RX power toggles keep the driver's ordering.
func (a *ANX7625) ConfigDSI() error {
	a.rmw(anxRXP0, 0x40, 0x01, 0)      // DSC disable
	a.rmw(anxRXP0, 0x3f, 0xe0, 0x80)   // XTAL 27 MHz, low bits kept

	a.wr(anxRXP0, 0x24, 0x99) // pixel clock aux (live-dump value)
	a.wr(anxRXP0, 0x25, 148)  // pixel clock, integer MHz
	a.wr(anxRXP0, 0x26, 0)
	a.wr(anxRXP0, 0x43, 0x00) // unmask OCM interrupts (as Linux runs)

	for _, t := range anxRXP1Snap {
		if err := a.wr(anxRXP1, t.reg, t.val); err != nil {
			return err
		}
	}

	// video timing (RX_P2): 1080p60 CEA
	for _, t := range []struct{ reg, val uint8 }{
		{0x19, 0x98}, {0x1a, 0x08}, // HTOTAL 2200
		{0x1b, 0x80}, {0x1c, 0x07}, // HACTIVE 1920
		{0x1d, 0x58}, {0x1e, 0x00}, // HFP 88
		{0x1f, 0x2c}, {0x20, 0x00}, // HSYNC 44
		{0x21, 0x94}, {0x22, 0x00}, // HBP 148
		{0x14, 0x38}, {0x15, 0x04}, // VACTIVE 1080
		{0x16, 0x04},               // VFP 4
		{0x17, 0x05},               // VSYNC 5
		{0x18, 0x24},               // VBP 36
	} {
		if err := a.wr(anxRXP2, t.reg, t.val); err != nil {
			return err
		}
	}

	// ODFC PLL reset toggle (VCO tune erratum first)
	a.rmw(anxRXP1, 0x2b, 0x30, 0)
	a.rmw(anxRXP1, 0x2b, 0x02, 0)
	a.rmw(anxRXP1, 0x2b, 0, 0x02)

	// latch M/N (a couple of I2C round-trips exceed the 1 ms hold)
	a.rmw(anxRXP1, 0x2a, 0x18, 0)

	for i := 0; i < 4; i++ {
		a.rd(anxRXP1, 0x2a)
	}

	a.rmw(anxRXP1, 0x2a, 0, 0x18)

	// MIPI RX power-on toggle
	a.wr(anxRXP1, 0x0f, 0x00)

	if err := a.wr(anxRXP1, 0x0f, 0x80); err != nil {
		return err
	}

	return nil
}

// EnableVideo asserts MIPI_RX_EN and clears the mute: the OCM then
// trains the DP link and starts the stream once it sees two stable
// DSI frames.
func (a *ANX7625) EnableVideo() error {
	if err := a.rmw(anxRXP0, 0x28, 0, 0x20); err != nil {
		return err
	}

	return a.rmw(anxRXP0, 0x28, 0x10, 0)
}

// Status returns SYSTEM_STATUS (bit7 = HPD) and the measured DSI
// frame check registers for diagnostics.
func (a *ANX7625) Status() (sys, chkErr uint8) {
	sys, _ = a.rd(anxRXP0, 0x45)
	chkErr, _ = a.rd(anxRXP1, 0x31)

	return
}
