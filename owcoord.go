package main

type OWCoord uint16

func Map16ToOWCoord(t uint16) (c OWCoord) {
	y := t & 0xFF80
	x := t & 0x007F
	// y-coordinate must be doubled due to 2x2 expansion from map16 to map8
	// x-coordinate needs no transform because uint16->uint8 conversion is balanced by 2x2 expansion
	return OWCoord((y << 1) + x)
}

func RowColToOWCoord(row, col int) OWCoord {
	return OWCoord((row&0x7F)<<7 + col&0x7F)
}

func (c OWCoord) RowCol() (row, col int) {
	row = int((c & 0x3F80) >> 7)
	col = int(c & 0x7F)
	return
}

func (c OWCoord) Traverse(d Direction, inc int) (OWCoord, Direction, bool) {
	it := int(c)
	row, col := c.RowCol()

	switch d {
	case DirNorth:
		if row >= 0+inc {
			return OWCoord(it - (inc << 7)), d, true
		}
		return c, d, false
	case DirSouth:
		if row <= 0x7F-inc {
			return OWCoord(it + (inc << 7)), d, true
		}
		return c, d, false
	case DirWest:
		if col >= 0+inc {
			return OWCoord(it - inc), d, true
		}
		return c, d, false
	case DirEast:
		if col <= 0x7F-inc {
			return OWCoord(it + inc), d, true
		}
		return c, d, false
	default:
		panic("bad direction")
	}
}
