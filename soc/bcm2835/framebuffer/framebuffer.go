// BCM2835 SoC FrameBuffer support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package framebuffer

import (
	"encoding/binary"
	"fmt"

	"github.com/usbarmory/tamago/soc/bcm2835"
)

// EDID gets the raw EDID information from the attached monitor
func EDID() []byte {
	outBuf := []byte{}

	for block := uint32(0); true; block++ {
		buf := make([]byte, bcm2835.VC_MEM_GET_EDID_BLOCK_LEN)
		binary.LittleEndian.PutUint32(buf[0:], block)

		msg := &bcm2835.MailboxMessage{
			Tags: []bcm2835.MailboxTag{
				{
					ID:     bcm2835.VC_MEM_GET_EDID_BLOCK,
					Buffer: buf,
				},
			},
		}

		bcm2835.Mailbox.Call(bcm2835.VC_CH_PROPERTYTAGS_A_TO_VC, msg)

		tag := msg.Tag(bcm2835.VC_MEM_GET_EDID_BLOCK)

		if tag == nil || len(tag.Buffer) < 8 {
			return outBuf
		}

		if binary.LittleEndian.Uint32(tag.Buffer[0:]) != block {
			panic("Got EDID data for wrong block")
		}

		status := binary.LittleEndian.Uint32(tag.Buffer[4:])
		if status != 0 {
			break
		}

		outBuf = append(outBuf, tag.Buffer[8:]...)
	}

	return outBuf
}

// PhysicalSize is the dimensions of the current framebuffer in pixels
func PhysicalSize() (width uint32, height uint32) {
	buf := make([]byte, bcm2835.VC_FB_GET_PHYSICAL_SIZE_LEN)

	msg := &bcm2835.MailboxMessage{
		Tags: []bcm2835.MailboxTag{
			{
				ID:     bcm2835.VC_FB_GET_PHYSICAL_SIZE,
				Buffer: buf,
			},
		},
	}

	bcm2835.Mailbox.Call(bcm2835.VC_CH_PROPERTYTAGS_A_TO_VC, msg)

	tag := msg.Tag(bcm2835.VC_FB_GET_PHYSICAL_SIZE)

	if tag == nil || len(tag.Buffer) < 8 {
		return 0, 0
	}

	return binary.LittleEndian.Uint32(tag.Buffer[0:]), binary.LittleEndian.Uint32(tag.Buffer[4:])
}

// SetPhysicalSize changes the display resolution (in pixels)
func SetPhysicalSize(width uint32, height uint32) error {
	buf := make([]byte, bcm2835.VC_FB_SET_PHYSICAL_SIZE_LEN)
	binary.LittleEndian.PutUint32(buf[0:], width)
	binary.LittleEndian.PutUint32(buf[4:], height)

	msg := &bcm2835.MailboxMessage{
		Tags: []bcm2835.MailboxTag{
			{
				ID:     bcm2835.VC_FB_SET_PHYSICAL_SIZE,
				Buffer: buf,
			},
		},
	}

	bcm2835.Mailbox.Call(bcm2835.VC_CH_PROPERTYTAGS_A_TO_VC, msg)

	tag := msg.Tag(bcm2835.VC_FB_SET_PHYSICAL_SIZE)

	if tag == nil || len(tag.Buffer) < 8 {
		return fmt.Errorf("failed to set desired size")
	}

	newWidth := binary.LittleEndian.Uint32(tag.Buffer[0:])
	newHeight := binary.LittleEndian.Uint32(tag.Buffer[4:])

	if newWidth != width || newHeight != height {
		return fmt.Errorf("failed to set desired size")
	}

	return nil
}

// Config describes an allocated framebuffer.
type Config struct {
	Width  uint32
	Height uint32
	Pitch  uint32 // bytes per row
	Addr   uint32 // ARM physical address
	Size   uint32 // buffer size in bytes
}

// Setup configures the display to the argument size with a 32 bits-per-pixel
// RGB(A) framebuffer and allocates it, returning its configuration. All
// property tags are submitted in a single mailbox transaction as required by
// the firmware for consistent mode setting.
func Setup(width, height uint32) (*Config, error) {
	physBuf := make([]byte, bcm2835.VC_FB_SET_PHYSICAL_SIZE_LEN)
	binary.LittleEndian.PutUint32(physBuf[0:], width)
	binary.LittleEndian.PutUint32(physBuf[4:], height)

	virtBuf := make([]byte, bcm2835.VC_FB_SET_VIRTUAL_SIZE_LEN)
	binary.LittleEndian.PutUint32(virtBuf[0:], width)
	binary.LittleEndian.PutUint32(virtBuf[4:], height)

	depthBuf := make([]byte, bcm2835.VC_FB_SET_DEPTH_LEN)
	binary.LittleEndian.PutUint32(depthBuf[0:], 32)

	orderBuf := make([]byte, bcm2835.VC_FB_SET_PIXEL_ORDER_LEN)
	binary.LittleEndian.PutUint32(orderBuf[0:], 1) // RGB

	allocBuf := make([]byte, bcm2835.VC_FB_ALLOC_BUFFER_LEN)
	binary.LittleEndian.PutUint32(allocBuf[0:], 4096) // alignment

	pitchBuf := make([]byte, bcm2835.VC_FB_GET_PITCH_LEN)

	msg := &bcm2835.MailboxMessage{
		Tags: []bcm2835.MailboxTag{
			{ID: bcm2835.VC_FB_SET_PHYSICAL_SIZE, Buffer: physBuf},
			{ID: bcm2835.VC_FB_SET_VIRTUAL_SIZE, Buffer: virtBuf},
			{ID: bcm2835.VC_FB_SET_DEPTH, Buffer: depthBuf},
			{ID: bcm2835.VC_FB_SET_PIXEL_ORDER, Buffer: orderBuf},
			{ID: bcm2835.VC_FB_ALLOC_BUFFER, Buffer: allocBuf},
			{ID: bcm2835.VC_FB_GET_PITCH, Buffer: pitchBuf},
		},
	}

	bcm2835.Mailbox.Call(bcm2835.VC_CH_PROPERTYTAGS_A_TO_VC, msg)

	alloc := msg.Tag(bcm2835.VC_FB_ALLOC_BUFFER)

	if alloc == nil || len(alloc.Buffer) < 8 {
		return nil, fmt.Errorf("framebuffer allocation failed")
	}

	pitch := msg.Tag(bcm2835.VC_FB_GET_PITCH)

	if pitch == nil || len(pitch.Buffer) < 4 {
		return nil, fmt.Errorf("framebuffer pitch query failed")
	}

	cfg := &Config{
		Width:  width,
		Height: height,
		Pitch:  binary.LittleEndian.Uint32(pitch.Buffer[0:]),
		// convert VideoCore bus address to ARM physical address
		Addr: binary.LittleEndian.Uint32(alloc.Buffer[0:]) &^ bcm2835.DRAM_FLAG_NOCACHE,
		Size: binary.LittleEndian.Uint32(alloc.Buffer[4:]),
	}

	if cfg.Addr == 0 || cfg.Size == 0 {
		return nil, fmt.Errorf("framebuffer allocation failed")
	}

	return cfg, nil
}
