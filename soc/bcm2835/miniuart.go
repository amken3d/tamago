// BCM2835 mini-UART driver
// https://github.com/usbarmory/tamago
//
// Copyright (c) The TamaGo Authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.
//
// This mini-UART is specifically intended for use as a
// console. See BCM2835-ARM-Peripherals.pdf that is
// widely available.
//

package bcm2835

import (
	"github.com/usbarmory/tamago/arm"
	"github.com/usbarmory/tamago/internal/reg"
)

// AUX_ENABLES bits. The three auxiliary peripherals -- mini-UART, SPI1 and
// SPI2 -- share this one register, so it must be read-modify-written: a blind
// write here disables the other two.
const (
	AUX_ENABLE_MINIUART = 0 // bit position, not a mask: see reg.Set
	AUX_ENABLE_SPI1     = 1
	AUX_ENABLE_SPI2     = 2
)

const (
	AUX_ENABLES     = 0x215004
	AUX_MU_IO_REG   = 0x215040
	AUX_MU_IER_REG  = 0x215044
	AUX_MU_IIR_REG  = 0x215048
	AUX_MU_LCR_REG  = 0x21504C
	AUX_MU_MCR_REG  = 0x215050
	AUX_MU_LSR_REG  = 0x215054
	AUX_MU_MSR_REG  = 0x215058
	AUX_MU_SCRATCH  = 0x21505C
	AUX_MU_CNTL_REG = 0x215060
	AUX_MU_STAT_REG = 0x215064
	AUX_MU_BAUD_REG = 0x215068
)

type miniUART struct {
	lsr uint32
	io  uint32
}

// MiniUART is a secondary low throughput UART intended to be
// used as a console.
var MiniUART = &miniUART{}

// Init initializes the MiniUART.
func (hw *miniUART) Init() {
	// Set only our own enable bit. This used to write 1 outright, which
	// silently cleared SPI1 and SPI2 -- so bringing up SPI1 and then touching
	// the console (or the reverse) turned one of them off, presenting as random
	// link failures rather than as a conflict.
	reg.Set(PeripheralAddress(AUX_ENABLES), 0) // bit 0: mini-UART
	reg.Write(PeripheralAddress(AUX_MU_IER_REG), 0)
	reg.Write(PeripheralAddress(AUX_MU_CNTL_REG), 0)
	reg.Write(PeripheralAddress(AUX_MU_LCR_REG), 3)
	reg.Write(PeripheralAddress(AUX_MU_MCR_REG), 0)
	reg.Write(PeripheralAddress(AUX_MU_IER_REG), 0)
	reg.Write(PeripheralAddress(AUX_MU_IIR_REG), 0xc6)
	reg.Write(PeripheralAddress(AUX_MU_BAUD_REG), 270)

	// Not using GPIO abstraction here because at the point
	// we initialize mini-UART during initialization, to
	// provide 'console', calling Lock on sync.Mutex fails.
	ra := reg.Read(PeripheralAddress(GPFSEL1))
	ra &= ^(uint32(7) << 12) // gpio14
	ra |= 2 << 12            // alt5
	ra &= ^(uint32(7) << 15) // gpio15
	ra |= 2 << 15            // alt5
	reg.Write(PeripheralAddress(GPFSEL1), ra)

	reg.Write(PeripheralAddress(GPPUD), 0)
	arm.Busyloop(150)

	reg.Write(PeripheralAddress(GPPUDCLK0), (1<<14)|(1<<15))
	arm.Busyloop(150)

	reg.Write(PeripheralAddress(GPPUDCLK0), 0)
	reg.Write(PeripheralAddress(AUX_MU_CNTL_REG), 3)

	hw.lsr = PeripheralAddress(AUX_MU_LSR_REG)
	hw.io = PeripheralAddress(AUX_MU_IO_REG)
}

// EnableRxInterrupt enables the mini-UART receive interrupt (AUX_MU_IER_REG
// bit 0), so a byte arriving in the RX FIFO raises the shared AUX interrupt
// (IRQ_AUX) at the ARM interrupt controller. The interrupt is cleared by
// draining the FIFO (see Rx); there is no separate acknowledge.
func (hw *miniUART) EnableRxInterrupt() {
	reg.Write(PeripheralAddress(AUX_MU_IER_REG), 1)
}

// TX transmits a single character to the serial port.
func (hw *miniUART) Tx(c byte) {
	for {
		if reg.Read(hw.lsr)&0x20 != 0 {
			break
		}
	}

	reg.Write(hw.io, uint32(c))
}

// Rx receives a single character from the serial port, returning ok=false
// if none is available. The mini-UART RX FIFO is 8 bytes deep: at 115200
// baud it fills in ~700 µs, so a poller must drain it at least that often
// to avoid overruns.
func (hw *miniUART) Rx() (c byte, ok bool) {
	if reg.Read(hw.lsr)&0x01 == 0 {
		return
	}

	return byte(reg.Read(hw.io)), true
}

// Read drains available data from the serial port RX FIFO into the buffer,
// without blocking, returning the number of bytes read.
func (hw *miniUART) Read(buf []byte) (n int, _ error) {
	for n < len(buf) {
		c, ok := hw.Rx()

		if !ok {
			break
		}

		buf[n] = c
		n++
	}

	return
}

// Write data from buffer to serial port.
func (hw *miniUART) Write(buf []byte) (n int, _ error) {
	for _, c := range buf {
		hw.Tx(c)
	}

	return len(buf), nil
}
