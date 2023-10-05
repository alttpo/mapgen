package main

import "fmt"

type DMARegs [16]byte

func (c *DMARegs) ctrl() byte { return c[0] }
func (c *DMARegs) dest() byte { return c[1] }
func (c *DMARegs) srcL() byte { return c[2] }
func (c *DMARegs) srcH() byte { return c[3] }
func (c *DMARegs) srcB() byte { return c[4] }
func (c *DMARegs) sizL() byte { return c[5] }
func (c *DMARegs) sizH() byte { return c[6] }

type DMAChannel struct{}

func (c *DMAChannel) Transfer(regs *DMARegs, ch int, h *HWIO) {
	aSrc := uint32(regs.srcB())<<16 | uint32(regs.srcH())<<8 | uint32(regs.srcL())
	siz := uint16(regs.sizH())<<8 | uint16(regs.sizL())

	bDest := regs.dest()
	bDestAddr := uint32(bDest) | 0x2100

	incr := regs.ctrl()&0x10 == 0
	fixed := regs.ctrl()&0x08 != 0
	mode := regs.ctrl() & 7

	//if h.s.Logger != nil {
	//	fmt.Fprintf(h.s.Logger, "PC=$%06x\n", h.s.GetPC())
	//	fmt.Fprintf(h.s.Logger, "DMA[%d] start: $%06x -> $%04x [$%05x]\n", ch, aSrc, bDestAddr, siz)
	//}

	if regs.ctrl()&0x80 != 0 {
		// PPU -> CPU
		panic("PPU -> CPU DMA transfer not supported!")
	} else {
		// CPU -> PPU
		if h.s.Logger != nil {
			fmt.Fprintf(h.s.Logger, "dma: a=%06X, b=%04x, s=%04x; vaddr=%04x\n", aSrc, bDestAddr, siz, h.PPU.addr)
		}
	copyloop:
		for {
			switch mode {
			case 0:
				h.Write(bDestAddr, h.s.Bus.EaRead(aSrc))
				if !fixed {
					if incr {
						aSrc = ((aSrc&0xFFFF)+1)&0xFFFF + aSrc&0xFF0000
					} else {
						aSrc = ((aSrc&0xFFFF)-1)&0xFFFF + aSrc&0xFF0000
					}
				}
				siz--
				if siz == 0 {
					break copyloop
				}
				break
			case 1:
				// p
				h.Write(bDestAddr, h.s.Bus.EaRead(aSrc))
				if !fixed {
					if incr {
						aSrc = ((aSrc&0xFFFF)+1)&0xFFFF + aSrc&0xFF0000
					} else {
						aSrc = ((aSrc&0xFFFF)-1)&0xFFFF + aSrc&0xFF0000
					}
				}
				siz--
				if siz == 0 {
					break copyloop
				}
				// p+1
				h.Write(bDestAddr+1, h.s.Bus.EaRead(aSrc))
				if !fixed {
					if incr {
						aSrc = ((aSrc&0xFFFF)+1)&0xFFFF + aSrc&0xFF0000
					} else {
						aSrc = ((aSrc&0xFFFF)-1)&0xFFFF + aSrc&0xFF0000
					}
				}
				siz--
				if siz == 0 {
					break copyloop
				}
				break
			case 2:
				panic("mode 2!!!")
			case 3:
				panic("mode 3!!!")
			case 4:
				panic("mode 4!!!")
			case 5:
				panic("mode 5!!!")
			case 6:
				panic("mode 6!!!")
			case 7:
				panic("mode 7!!!")
			}
		}
	}

	//if h.s.Logger != nil {
	//	fmt.Fprintf(h.s.Logger, "DMA[%d]  stop: $%06x -> $%04x [$%05x]\n", ch, aSrc, bDestAddr, siz)
	//}
}

type HWIO struct {
	s *System

	DMARegs [8]DMARegs
	DMA     [8]DMAChannel

	PPU struct {
		incrMode      bool   // false = increment after $2118, true = increment after $2119
		incrAmt       uint16 // 1, 32, or 128
		addrRemapping byte
		addr          uint16

		oamadd                 uint16
		ObjTilemapAddress      uint32
		ObjNamespaceSeparation uint32
	}

	APU struct {
		mpya uint8  // multiplicand A
		mpyb uint8  // multiplicand B
		mpyp uint16 // product

		divd uint16 // dividend
		divi uint8  // divisor
		divq uint16 // quotient
	}

	ControllerInput [2]uint16

	// mapped to $5000-$7FFF
	Dyn [0x3000]byte
}

func (h *HWIO) Reset() {
	h.DMARegs = [8]DMARegs{}
	h.DMA = [8]DMAChannel{}
	h.PPU.incrMode = false
	h.PPU.incrAmt = 0
	h.PPU.addrRemapping = 0
	h.PPU.addr = 0
	h.Dyn = [0x3000]byte{}
}

func (h *HWIO) Read(address uint32) (value byte) {
	offs := address & 0xFFFF
	if offs >= 0x5000 {
		value = h.Dyn[offs-0x5000]
		return
	}

	if offs == 0x4214 {
		// RDDIVL
		value = uint8(h.APU.divq & 0xFF)
		return
	}
	if offs == 0x4215 {
		// RDDIVH
		value = uint8(h.APU.divq >> 8)
		return
	}

	if offs == 0x4216 {
		// RDMPYL
		value = uint8(h.APU.mpyp & 0xFF)
		return
	}
	if offs == 0x4217 {
		// RDMPYH
		value = uint8(h.APU.mpyp >> 8)
		return
	}

	if offs == 0x4218 {
		value = byte(h.ControllerInput[0] & 0xFF)
		return
	}
	if offs == 0x4219 {
		value = byte(h.ControllerInput[0] >> 8)
		return
	}
	// OPVCT
	if offs == 0x213D {
		value = 0xF0
		return
	}

	//if h.s.Logger != nil {
	//	fmt.Fprintf(h.s.Logger, "hwio[$%04x] -> $%02x\n", offs, value)
	//}
	return
}

func (h *HWIO) Write(address uint32, value byte) {
	offs := address & 0xFFFF

	if offs == 0x4200 {
		// NMITIMEN
		return
	}

	if offs == 0x4202 {
		// WRMPYA
		h.APU.mpya = value
		h.APU.mpyp = uint16(h.APU.mpya) * uint16(h.APU.mpyb)
		return
	}
	if offs == 0x4203 {
		// WRMPYB
		h.APU.mpyb = value
		h.APU.mpyp = uint16(h.APU.mpya) * uint16(h.APU.mpyb)
		return
	}
	if offs == 0x4204 {
		// WRDIVL
		h.APU.divd = h.APU.divd&0xFF00 | uint16(value)
		if h.APU.divi != 0 {
			h.APU.divq = h.APU.divd / uint16(h.APU.divi)
		}
		return
	}
	if offs == 0x4205 {
		// WRDIVH
		h.APU.divd = h.APU.divd&0x00FF | uint16(value)<<8
		if h.APU.divi != 0 {
			h.APU.divq = h.APU.divd / uint16(h.APU.divi)
		}
		return
	}
	if offs == 0x4206 {
		// WRDIVB
		h.APU.divi = value
		if h.APU.divi != 0 {
			h.APU.divq = h.APU.divd / uint16(h.APU.divi)
		}
		return
	}

	if offs == 0x420b {
		// MDMAEN:
		hdmaen := value
		//if h.s.Logger != nil {
		//	fmt.Fprintf(h.s.Logger, "hwio[$%04x] <- $%02x DMA start\n", offs, hdmaen)
		//}
		// execute DMA transfers from channels 0..7 in order:
		for c := range h.DMA {
			if hdmaen&(1<<c) == 0 {
				continue
			}

			// channel enabled:
			h.DMA[c].Transfer(&h.DMARegs[c], c, h)
		}
		//if h.s.Logger != nil {
		//	fmt.Fprintf(h.s.Logger, "hwio[$%04x] <- $%02x DMA end\n", offs, hdmaen)
		//}
		return
	}
	if offs == 0x420c {
		// HDMAEN:
		// no HDMA support
		//if h.s.Logger != nil {
		//	fmt.Fprintf(h.s.Logger, "hwio[$%04x] <- $%02x HDMA ignored\n", offs, value)
		//}
		return
	}
	if offs&0xFF00 == 0x4300 {
		// DMA registers:
		ch := (offs & 0x00F0) >> 4
		if ch <= 7 {
			reg := offs & 0x000F
			h.DMARegs[ch][reg] = value
		}

		//if h.s.Logger != nil {
		//	fmt.Fprintf(h.s.Logger, "hwio[$%04x] <- $%02x DMA register\n", offs, value)
		//}
		return
	}

	if offs == 0x2100 {
		// INIDISP
		return
	}
	if offs == 0x2101 {
		// OBSEL
		h.PPU.ObjNamespaceSeparation = uint32(value&0x18) << 9
		h.PPU.ObjTilemapAddress = uint32(value&0x7) << 13
		// skip size table
		return
	}
	if offs == 0x2102 {
		// OAMADDL
		h.PPU.oamadd = uint16(value) | h.PPU.oamadd&0xFF00
		return
	}
	if offs == 0x2103 {
		// OAMADDH
		h.PPU.oamadd = uint16(value)<<8 | h.PPU.oamadd&0x00FF
		return
	}
	if offs == 0x2104 {
		// OAMDATA
		h.s.OAM[h.PPU.oamadd] = value

		// TODO: how to wrap this?
		h.PPU.oamadd = h.PPU.oamadd + 1
		if h.PPU.oamadd >= 544 {
			h.PPU.oamadd = 0
		}
		return
	}
	if offs == 0x2121 {
		// CGADD
		return
	}
	if offs == 0x2122 {
		// CGDATA
		return
	}
	if offs == 0x212e || offs == 0x212f {
		// TMW, TSW
		return
	}

	// PPU:
	if offs == 0x2115 {
		// VMAIN = o---mmii
		h.PPU.incrMode = value&0x80 != 0
		switch value & 3 {
		case 0:
			h.PPU.incrAmt = 1
			break
		case 1:
			h.PPU.incrAmt = 32
			break
		default:
			h.PPU.incrAmt = 128
			break
		}
		h.PPU.addrRemapping = (value & 0x0C) >> 2
		if h.PPU.addrRemapping != 0 {
			fmt.Printf("unsupported VRAM address remapping mode %d\n", h.PPU.addrRemapping)
		}
		//if h.s.Logger != nil {
		//	fmt.Fprintf(h.s.Logger, "PC=$%06x\n", h.s.GetPC())
		//	fmt.Fprintf(h.s.Logger, "VMAIN = $%02x\n", value)
		//}
		return
	}
	if offs == 0x2116 {
		// VMADDL
		h.PPU.addr = uint16(value) | h.PPU.addr&0xFF00
		//if h.s.Logger != nil {
		//	fmt.Fprintf(h.s.Logger, "PC=$%06x\n", h.s.GetPC())
		//	fmt.Fprintf(h.s.Logger, "VMADDL = $%04x\n", h.PPU.addr)
		//}
		return
	}
	if offs == 0x2117 {
		// VMADDH
		h.PPU.addr = uint16(value)<<8 | h.PPU.addr&0x00FF
		//if h.s.Logger != nil {
		//	fmt.Fprintf(h.s.Logger, "PC=$%06x\n", h.s.GetPC())
		//	fmt.Fprintf(h.s.Logger, "VMADDH = $%04x\n", h.PPU.addr)
		//}
		return
	}
	if offs == 0x2118 {
		// VMDATAL
		h.s.VRAM[h.PPU.addr<<1] = value
		if h.PPU.incrMode == false {
			h.PPU.addr += h.PPU.incrAmt
		}
		return
	}
	if offs == 0x2119 {
		// VMDATAH
		h.s.VRAM[(h.PPU.addr<<1)+1] = value
		if h.PPU.incrMode == true {
			h.PPU.addr += h.PPU.incrAmt
		}
		return
	}

	// APU:
	if offs >= 0x2140 && offs <= 0x2143 {
		// APUIO0 .. APUIO3
		return
	}

	if h.s.Logger != nil {
		fmt.Fprintf(h.s.Logger, "hwio[$%04x] <- $%02x\n", offs, value)
	}
}

func (h *HWIO) Shutdown() {
}

func (h *HWIO) Size() uint32 {
	return 0x10000
}

func (h *HWIO) Clear() {
}

func (h *HWIO) Dump(address uint32) []byte {
	return nil
}
