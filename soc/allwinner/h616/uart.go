// Allwinner (Synopsys DesignWare) APB UART driver, polled 16550 mode
// https://github.com/usbarmory/tamago
//
// Copyright (c) the h616 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package h616

import (
	"github.com/usbarmory/tamago/internal/reg"
)

// DW APB UART registers: 16550-compatible layout, 32-bit registers at
// 4-byte stride (dts: reg-shift = <2>, reg-io-width = <4>).
const (
	UART_RBR = 0x00 // receive buffer (read)
	UART_THR = 0x00 // transmit holding (write)
	UART_IER = 0x04 // interrupt enable
	UART_FCR = 0x08 // FIFO control (write)
	UART_LCR = 0x0c // line control
	UART_LSR = 0x14 // line status
	UART_USR = 0x7c // DesignWare UART status

	LSR_DR   = 1 << 0 // receive data ready
	LSR_THRE = 1 << 5 // transmit holding register empty

	FCR_FIFO_EN = 1 << 0
)

// UART represents a DW APB UART instance. The baud rate, pin mux and bus
// clock set by the boot chain (U-Boot debug console) are preserved: this
// driver never touches the CCU, the divisor latch or the line control
// register.
type UART struct {
	Base uint32
}

// txPollSpins bounds register polling by iteration count rather than by
// time: Tx backs the runtime console (Printk), which must work at any
// boot stage, including before timers or the scheduler exist.
const txPollSpins = 200000

// Init prepares the UART for polled operation. Framing, baud and mux are
// inherited from the boot chain; only the FIFOs and interrupt signaling
// are (re)set to a known state.
func (u *UART) Init() {
	reg.Write(u.Base+UART_IER, 0)           // polled: no interrupts
	reg.Write(u.Base+UART_FCR, FCR_FIFO_EN) // FIFOs on, reset
}

// Tx transmits a single character to the serial port.
func (u *UART) Tx(c byte) {
	for i := 0; i < txPollSpins; i++ {
		if reg.Read(u.Base+UART_LSR)&LSR_THRE != 0 {
			reg.Write(u.Base+UART_THR, uint32(c))
			return
		}
	}
}

// Rx receives a single character from the serial port, returning false
// when the receive FIFO is empty.
func (u *UART) Rx() (c byte, valid bool) {
	if reg.Read(u.Base+UART_LSR)&LSR_DR == 0 {
		return 0, false
	}

	return byte(reg.Read(u.Base + UART_RBR)), true
}

// Read available data from the serial port into buf.
func (u *UART) Read(buf []byte) (n int, _ error) {
	for n = 0; n < len(buf); n++ {
		c, valid := u.Rx()

		if !valid {
			break
		}

		buf[n] = c
	}

	return
}

// Write data from buf to the serial port.
func (u *UART) Write(buf []byte) (n int, _ error) {
	for n = 0; n < len(buf); n++ {
		u.Tx(buf[n])
	}

	return
}
