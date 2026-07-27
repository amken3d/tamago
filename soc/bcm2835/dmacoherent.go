// BCM2835 SoC coherent DMA support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package bcm2835

import (
	"encoding/binary"
	"fmt"
	"time"
	"unsafe"

	"github.com/usbarmory/tamago/internal/reg"
)

// Coherent DMA for real hardware. Two facts make this work on the Pi:
//
//   - DMA buffers and control blocks must be uncached. The MMU maps the ARM
//     runtime RAM (runtime.MemRegion) cacheable and everything above it -- the
//     GPU-reserved heap -- as Device (uncached). So memory allocated from the
//     GPU via the mailbox is coherent with the DMA engine without any cache
//     maintenance (which matters here: set/way cache ops are unsafe once a
//     second core is coherent).
//   - The DMA engine addresses memory by *bus* address: peripherals at
//     0x7Exxxxxx and RAM via the DRAM_FLAG_NOCACHE (0xC0000000) alias. The ARM
//     accesses the same memory at the masked physical address.
//
// The pre-existing DMAChannel.Copy puts its control block in cacheable RAM and
// writes raw (non-bus) addresses, so it does not work on hardware; use
// CoherentBuffer + Transfer instead.

// DMA_ENABLE is the global channel-enable register, offset from the DMA base.
const DMA_ENABLE = 0xff0

// busPeripheralBase is the DMA engine's view of the peripheral window (the ARM
// sees it at peripheralBase, e.g. 0x3f000000; the VideoCore bus alias is fixed).
const busPeripheralBase = 0x7E000000

// CoherentBuffer is uncached, GPU-allocated memory usable as a DMA source or
// target (and to hold control blocks).
type CoherentBuffer struct {
	handle uint32
	Bus    uint32 // bus address for the DMA engine (0xC0000000 alias)
	Phys   uint32 // ARM-accessible (uncached) address
	Size   uint32
	buf    []byte
}

// AllocCoherent allocates size bytes of uncached, DMA-coherent memory from the
// GPU heap, aligned to 32 bytes (the control-block alignment requirement).
func AllocCoherent(size uint32) (*CoherentBuffer, error) {
	handle := AllocateGPUMemory(size, 32, GPU_MEMORY_FLAG_DIRECT|GPU_MEMORY_FLAG_ZERO)
	if handle == 0 {
		return nil, fmt.Errorf("bcm2835: GPU memory alloc failed (size %d)", size)
	}
	bus := LockGPUMemory(handle)
	if bus == 0 {
		return nil, fmt.Errorf("bcm2835: GPU memory lock failed (handle %#x)", handle)
	}
	phys := bus &^ DRAM_FLAG_NOCACHE
	buf := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(phys))), int(size))
	return &CoherentBuffer{handle: handle, Bus: bus, Phys: phys, Size: size, buf: buf}, nil
}

// Bytes returns the ARM-accessible (uncached) view of the buffer.
func (b *CoherentBuffer) Bytes() []byte { return b.buf }

// Enable turns on this DMA channel in the global enable register.
func (ch *DMAChannel) Enable() {
	en := peripheralBase + DMA_CH_BASE0 + DMA_ENABLE
	reg.Write(en, reg.Read(en)|(1<<uint(ch.index)))
}

// Index returns the channel number.
func (ch *DMAChannel) Index() int { return ch.index }

// Start arms a single-control-block DMA transfer without waiting. cb is 32+
// bytes of coherent memory for the control block; cbBus is its bus address
// (32-byte aligned). ti is the transfer-info word; src and dst are bus
// addresses (peripherals 0x7Exxxxxx, RAM via the 0xC0000000 alias). For a
// peripheral-paced transfer, arm this before triggering the peripheral so the
// engine drains the FIFO as it fills.
func (ch *DMAChannel) Start(cb []byte, cbBus, ti, src, dst, length uint32) {
	conv := binary.LittleEndian
	conv.PutUint32(cb[0:], ti)
	conv.PutUint32(cb[4:], src)
	conv.PutUint32(cb[8:], dst)
	conv.PutUint32(cb[12:], length)
	conv.PutUint32(cb[16:], 0) // 2D stride
	conv.PutUint32(cb[20:], 0) // next control block (none)
	conv.PutUint32(cb[24:], 0)
	conv.PutUint32(cb[28:], 0)

	reg.Write(ch.base+DMA_CH_REG_CS, DMA_CS_RESET)
	for reg.Read(ch.base+DMA_CH_REG_CS)&DMA_CS_RESET != 0 {
	}
	reg.Write(ch.base+DMA_CH_REG_DEBUG, 0x7) // write-1-clear error flags
	reg.Write(ch.base+DMA_CH_REG_CONBLK_AD, cbBus)
	reg.Write(ch.base+DMA_CH_REG_CS, DMA_CS_ACTIVE)
}

// Wait blocks until the armed transfer completes, reporting timeout or error.
func (ch *DMAChannel) Wait(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for reg.Read(ch.base+DMA_CH_REG_CS)&DMA_CS_ACTIVE != 0 {
		if time.Now().After(deadline) {
			return fmt.Errorf("bcm2835: DMA timeout cs=%#x debug=%#x",
				reg.Read(ch.base+DMA_CH_REG_CS), reg.Read(ch.base+DMA_CH_REG_DEBUG))
		}
	}
	if cs := reg.Read(ch.base + DMA_CH_REG_CS); cs&DMA_CS_ERROR != 0 {
		return fmt.Errorf("bcm2835: DMA error cs=%#x debug=%#x",
			cs, reg.Read(ch.base+DMA_CH_REG_DEBUG))
	}
	return nil
}

// Abort resets the channel, cancelling any in-flight transfer.
func (ch *DMAChannel) Abort() {
	reg.Write(ch.base+DMA_CH_REG_CS, DMA_CS_RESET)
	for reg.Read(ch.base+DMA_CH_REG_CS)&DMA_CS_RESET != 0 {
	}
}

// Transfer runs a single-control-block DMA transfer and waits for completion.
func (ch *DMAChannel) Transfer(cb []byte, cbBus, ti, src, dst, length uint32) error {
	ch.Start(cb, cbBus, ti, src, dst, length)
	return ch.Wait(1 * time.Second)
}
