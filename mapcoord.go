package main

import "fmt"

type MapCoord uint16

func (t MapCoord) String() string {
	_, row, col := t.RowCol()
	return fmt.Sprintf("$%04x={%02x,%02x}", uint16(t), row, col)
}

func (t MapCoord) IsLayer2() bool {
	return t&0x1000 != 0
}

func AbsToMapCoord(absX, absY, layer uint16) MapCoord {
	// modeled after RoomTag_GetTilemapCoords#_01CDA5
	c := ((absY)&0x01F8)<<3 | ((absX)&0x01F8)>>3
	if layer != 0 {
		return MapCoord(c | 0x1000)
	}
	return MapCoord(c)
}

func (t MapCoord) ToAbsCoord(st Supertile) (x uint16, y uint16) {
	_, row, col := t.RowCol()
	x = col<<3 + 0x1
	y = row<<3 - 0xE

	// add absolute position from supertile:
	y += (uint16(st) & 0xF0) << 5
	x += (uint16(st) & 0x0F) << 9
	return
}

func (t MapCoord) MoveBy(dir Direction, increment int) (MapCoord, Direction, bool) {
	it := int(t)
	row := (it & 0xFC0) >> 6
	col := it & 0x3F

	// don't allow perpendicular movement along the outer edge
	// this prevents accidental/leaky flood fill along the edges
	if row == 0 || row == 0x3F {
		if dir != DirNorth && dir != DirSouth {
			return t, dir, false
		}
	}
	if col == 0 || col == 0x3F {
		if dir != DirWest && dir != DirEast {
			return t, dir, false
		}
	}

	switch dir {
	case DirNorth:
		if row >= 0+increment {
			return MapCoord(it - (increment << 6)), dir, true
		}
		return t, dir, false
	case DirSouth:
		if row <= 0x3F-increment {
			return MapCoord(it + (increment << 6)), dir, true
		}
		return t, dir, false
	case DirWest:
		if col >= 0+increment {
			return MapCoord(it - increment), dir, true
		}
		return t, dir, false
	case DirEast:
		if col <= 0x3F-increment {
			return MapCoord(it + increment), dir, true
		}
		return t, dir, false
	default:
		panic("bad direction")
	}

	return t, dir, false
}

func (t MapCoord) Row() MapCoord {
	return t & 0x0FFF >> 6
}

func (t MapCoord) Col() MapCoord {
	return t & 0x003F
}

func (t MapCoord) RowCol() (layer, row, col uint16) {
	layer = uint16(t & 0x1000)
	row = uint16((t & 0x0FC0) >> 6)
	col = uint16(t & 0x003F)
	return
}

func (t MapCoord) IsEdge() (ok bool, dir Direction, row, col uint16) {
	_, row, col = t.RowCol()
	if row == 0 {
		ok, dir = true, DirNorth
		return
	}
	if row == 0x3F {
		ok, dir = true, DirSouth
		return
	}
	if col == 0 {
		ok, dir = true, DirWest
		return
	}
	if col == 0x3F {
		ok, dir = true, DirEast
		return
	}
	return
}

func (t MapCoord) OnEdge(d Direction) MapCoord {
	lyr, row, col := t.RowCol()
	switch d {
	case DirNorth:
		return MapCoord(lyr | (0x00 << 6) | col)
	case DirSouth:
		return MapCoord(lyr | (0x3F << 6) | col)
	case DirWest:
		return MapCoord(lyr | (row << 6) | 0x00)
	case DirEast:
		return MapCoord(lyr | (row << 6) | 0x3F)
	default:
		panic("bad direction")
	}
	return t
}

func (t MapCoord) IsDoorEdge() (ok bool, dir Direction, row, col uint16) {
	_, row, col = t.RowCol()
	if row <= 0x08 {
		ok, dir = true, DirNorth
		return
	}
	if row >= 0x3F-8 {
		ok, dir = true, DirSouth
		return
	}
	if col <= 0x08 {
		ok, dir = true, DirWest
		return
	}
	if col >= 0x3F-8 {
		ok, dir = true, DirEast
		return
	}
	return
}

func (t MapCoord) OppositeDoorEdge() MapCoord {
	lyr, row, col := t.RowCol()
	if row <= 0x08 {
		return MapCoord(lyr | (0x3A << 6) | col)
	}
	if row >= 0x3F-8 {
		return MapCoord(lyr | (0x06 << 6) | col)
	}
	if col <= 0x08 {
		return MapCoord(lyr | (row << 6) | 0x3A)
	}
	if col >= 0x3F-8 {
		return MapCoord(lyr | (row << 6) | 0x06)
	}
	panic("not at an edge")
	return t
}

func (t MapCoord) FlipVertical() MapCoord {
	lyr, row, col := t.RowCol()
	row = 0x40 - row
	return MapCoord(lyr | (row << 6) | col)
}

func (t MapCoord) OppositeEdge() MapCoord {
	lyr, row, col := t.RowCol()
	if row == 0x00 {
		return MapCoord(lyr | (0x3F << 6) | col)
	}
	if row == 0x3F {
		return MapCoord(lyr | (0x00 << 6) | col)
	}
	if col == 0x00 {
		return MapCoord(lyr | (row << 6) | 0x3F)
	}
	if col == 0x3F {
		return MapCoord(lyr | (row << 6) | 0x00)
	}
	panic("not at an edge")
}
