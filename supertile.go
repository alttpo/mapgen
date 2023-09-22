package main

import "fmt"

type Supertile uint16

func (s Supertile) String() string { return fmt.Sprintf("$%03x", uint16(s)) }

func (s Supertile) MoveBy(dir Direction) (sn Supertile, sd Direction, ok bool) {
	// don't move within EG2:
	if s&0xFF00 != 0 {
		ok = false
	}

	sn, sd, ok = s, dir, false
	switch dir {
	case DirNorth:
		sn = Supertile(uint16(s) - 0x10)
		ok = uint16(s)&0xF0 > 0
		break
	case DirSouth:
		sn = Supertile(uint16(s) + 0x10)
		ok = uint16(s)&0xF0 < 0xF0
		break
	case DirWest:
		sn = Supertile(uint16(s) - 1)
		ok = uint16(s)&0x0F > 0
		break
	case DirEast:
		sn = Supertile(uint16(s) + 1)
		ok = uint16(s)&0x0F < 0xF
		break
	}

	// don't cross EG maps:
	if sn&0xFF00 != 0 {
		ok = false
	}

	return
}

func (s Supertile) AbsTopLeft() (sx uint16, sy uint16) {
	st := uint16(s)
	sx = (st & 0x0F) << 9
	sy = (st & 0xF0) << 5
	// add eg2 offset to sy if applicable:
	sy += (st & 0x100) << 5
	return
}
