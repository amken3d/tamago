// BCM2835 SPI0 master driver
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package bcm2835

import (
	"errors"
	"sync"

	"github.com/usbarmory/tamago/internal/reg"
)

// SPI0 registers
// (p152, 10.5 Register View, BCM2835 ARM Peripherals).
const (
	SPI0_BASE = 0x204000

	SPIx_CS   = 0x00
	SPIx_FIFO = 0x04
	SPIx_CLK  = 0x08

	CS_RXF   = 1 << 20 // RX FIFO full
	CS_RXR   = 1 << 19 // RX FIFO needs reading
	CS_TXD   = 1 << 18 // TX FIFO can accept data
	CS_RXD   = 1 << 17 // RX FIFO contains data
	CS_DONE  = 1 << 16 // transfer done
	CS_REN   = 1 << 12 // read enable (bidirectional mode)
	CS_TA    = 1 << 7  // transfer active
	CS_CLEAR = 0b11 << 4
	CS_CPOL  = 1 << 3
	CS_CPHA  = 1 << 2
	CS_CS    = 0b11
)

// coreFreq is the VPU/core clock feeding the SPI (and mini-UART) clock
// dividers. It must be pinned in config.txt (core_freq=250, the reset
// default) for stable bus clocks, consistently with the mini-UART divisor
// used by this package.
const coreFreq = 250000000

// SPI represents a SPI master port instance.
type SPI struct {
	sync.Mutex

	// control/status, FIFO and clock register addresses
	cs   uint32
	fifo uint32
	clk  uint32

	// mode bits (CPOL/CPHA) applied to each transfer
	mode uint32
}

// SPI0 is the SPI0 master controller, exposed on GPIO7-11 (CE1, CE0, MISO,
// MOSI, SCLK).
var SPI0 = &SPI{}

// Init initializes the SPI0 master with the argument SPI mode (0-3) and clock
// frequency in Hz, muxing GPIO7-11 (CE1, CE0, MISO, MOSI, SCLK) to ALT0.
//
// Use InitBus instead on a board that repurposes the hardware chip-selects:
// GPIO7 and GPIO8 are ordinary pins until this function claims them, and a
// carrier is free to wire them to something else entirely.
func (hw *SPI) Init(mode int, hz uint32) error {
	return hw.InitBus(mode, hz, true)
}

// InitBus is Init with control over the hardware chip-selects.
//
// With ce false only MISO, MOSI and SCLK are muxed, leaving GPIO7 and GPIO8
// alone for the board to use and for the caller to drive its own chip-selects
// as plain GPIO. That is not an exotic case: a board with more than two SPI
// devices needs software chip-selects anyway, and one carrier here wires both
// CE pins to endstop inputs -- muxing them would turn two endstops into
// chip-select outputs, with a symptom (homing fails) that points nowhere near
// the cause (something initialised SPI).
func (hw *SPI) InitBus(mode int, hz uint32, ce bool) error {
	hw.Lock()
	defer hw.Unlock()

	if mode < 0 || mode > 3 {
		return errors.New("invalid SPI mode")
	}

	if hz == 0 || hz > coreFreq/2 {
		return errors.New("invalid SPI clock frequency")
	}

	pins := []int{9, 10, 11} // MISO, MOSI, SCLK
	if ce {
		pins = append(pins, 7, 8) // CE1, CE0
	}

	for _, num := range pins {
		gpio, err := NewGPIO(num)

		if err != nil {
			return err
		}

		if err = gpio.SelectFunction(GPIO_FN0); err != nil {
			return err
		}
	}

	hw.cs = PeripheralAddress(SPI0_BASE + SPIx_CS)
	hw.fifo = PeripheralAddress(SPI0_BASE + SPIx_FIFO)
	hw.clk = PeripheralAddress(SPI0_BASE + SPIx_CLK)

	hw.mode = 0

	if mode&0b10 != 0 {
		hw.mode |= CS_CPOL
	}

	if mode&0b01 != 0 {
		hw.mode |= CS_CPHA
	}

	// The divider must be a multiple of 2 (p156, BCM2835 ARM Peripherals);
	// round up so the resulting clock never exceeds the requested one.
	div := (coreFreq + hz - 1) / hz
	div = (div + 1) &^ 1

	if div > 65536 {
		div = 65536
	}

	reg.Write(hw.clk, div&0xffff) // 65536 encodes as 0
	reg.Write(hw.cs, hw.mode|CS_CLEAR)

	return nil
}

// Transfer performs a full-duplex polled transfer to the argument slave
// (chip select 0 or 1): len(tx) bytes are shifted out of MOSI while the
// same number of bytes are captured from MISO into rx, which may be nil to
// discard the read data, and must otherwise be at least as long as tx.
func (hw *SPI) Transfer(slave int, tx []byte, rx []byte) error {
	if hw.cs == 0 {
		return errors.New("SPI not initialized")
	}

	if slave < 0 || slave > 1 {
		return errors.New("invalid slave")
	}

	if rx != nil && len(rx) < len(tx) {
		return errors.New("rx buffer too short")
	}

	hw.Lock()
	defer hw.Unlock()

	base := hw.mode | uint32(slave)

	reg.Write(hw.cs, base|CS_CLEAR)
	reg.Write(hw.cs, base|CS_TA)

	for txi, rxi := 0, 0; rxi < len(tx); {
		for txi < len(tx) && reg.Read(hw.cs)&CS_TXD != 0 {
			reg.Write(hw.fifo, uint32(tx[txi]))
			txi++
		}

		for rxi < txi && reg.Read(hw.cs)&CS_RXD != 0 {
			data := reg.Read(hw.fifo)

			if rx != nil {
				rx[rxi] = byte(data)
			}

			rxi++
		}
	}

	for reg.Read(hw.cs)&CS_DONE == 0 {
	}

	reg.Write(hw.cs, base)

	return nil
}
