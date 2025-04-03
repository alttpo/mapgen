package main

import (
	"fmt"
	"os"
	"strings"
)

func roomFindReachablePitsFromEnemies(room *RoomState) {
	st := room.Supertile

	e := &room.e
	wram := e.WRAM[:]
	// vram := e.VRAM[:]

	tiles := wram[0x12000:0x14000]

	coordLifo := make([]MapCoord, 0, 0x2000)
	startCoords := make([]MapCoord, 0, 16)

	room.TilesVisited = make(map[MapCoord]empty, 0x2000)

	// make a map full of $01 Collision and carve out reachable areas:
	for i := range room.Reachable {
		room.Reachable[i] = 0x01
	}

	// open up "locked" cell doors:
	for i := uint32(0); i < 6; i++ {
		gt := read16(wram, 0x06E0+i<<1)
		if gt == 0 {
			break
		}

		if gt&0x8000 != 0 {
			// locked cell door:
			t := MapCoord((gt & 0x7FFF) >> 1)
			if tiles[t] == 0x58+uint8(i) {
				tiles[t+0x00] = 0x00
				tiles[t+0x01] = 0x00
				tiles[t+0x40] = 0x00
				tiles[t+0x41] = 0x00
			}
			if tiles[t|0x1000] == 0x58+uint8(i) {
				tiles[t|0x1000+0x00] = 0x00
				tiles[t|0x1000+0x01] = 0x00
				tiles[t|0x1000+0x40] = 0x00
				tiles[t|0x1000+0x41] = 0x00
			}
		}
	}

	// open up doorways:
	room.Doors = make([]Door, 0, 16)
	for m := 0; m < 16; m++ {
		tpos := read16(wram, uint32(0x19A0+(m<<1)))
		// stop marker:
		if tpos == 0 {
			break
		}

		door := Door{
			Pos:  MapCoord(tpos >> 1),
			Type: DoorType(read16(wram, uint32(0x1980+(m<<1)))),
			Dir:  Direction(read16(wram, uint32(0x19C0+(m<<1)))),
		}
		if door.Type == 0x30 {
			// exploding wall:
			pos := int(door.Pos)
			for c := 0; c < 11; c++ {
				for r := 0; r < 12; r++ {
					tiles[pos+(r<<6)-c] = 0
					tiles[pos+(r<<6)+1+c] = 0
				}
			}
			continue
		}

		room.Doors = append(room.Doors, door)

		var ok bool
		var c MapCoord
		count := 3
		// find how far to clear to opposite doorway:
		c = door.Pos
		dbg := strings.Builder{}
		for i := 0; i < 16; i++ {
			c, _, ok = c.MoveBy(door.Dir, 1)
			if !ok {
				break
			}
			fmt.Fprintf(&dbg, "%02X,", tiles[c])
			if tiles[c] == 0x02 && count >= 8 {
				count++
				break
			}
			count++
		}
		fmt.Printf("%s: door type=%s dir=%s pos=%s: %s\n", st, door.Type, door.Dir, door.Pos, dbg.String())

		var secondTileOffs MapCoord
		switch door.Dir {
		case DirNorth:
			c, _, _ = door.Pos.MoveBy(DirEast, 1)
			c, _, ok = c.MoveBy(DirSouth, 2)
			secondTileOffs = 0x01
		case DirSouth:
			c, _, _ = door.Pos.MoveBy(DirSouth, 1)
			c, _, ok = c.MoveBy(DirEast, 1)
			secondTileOffs = 0x01
		case DirEast:
			c, _, ok = door.Pos.MoveBy(DirSouth, 1)
			secondTileOffs = 0x40
		case DirWest:
			c, _, _ = door.Pos.MoveBy(DirSouth, 1)
			c, _, ok = c.MoveBy(DirEast, 2)
			secondTileOffs = 0x40
		}

		// clear the path:
		for i := 0; ok && i < count; i++ {
			tiles[c+0x00] = 0
			tiles[c+secondTileOffs] = 0
			c, _, ok = c.MoveBy(door.Dir, 1)
		}
	}

	for i := uint32(0); i < 16; i++ {
		// skip inactive enemies:
		isDead := read8(wram, uint32(0x0DD0+i)) == 0
		if isDead {
			continue
		}

		// skip non-enemies:
		et := read8(wram, uint32(0x0E20+i))
		if et >= 0xD8 {
			// all collectibles and miscellaneous things that are not enemies:
			continue
		}
		switch et {
		// switches:
		case 0x04, 0x05, 0x06, 0x07, 0x14, 0x1D, 0x1E, 0x21:
			continue
		// bumper
		case 0x93:
			continue
		// eye laser
		case 0x95, 0x96, 0x97, 0x98:
			continue
		// keese:
		case 0x6F:
			continue
		// triggers:
		case 0x37, 0x40, 0x72, 0xB3, 0xBA:
			continue
		// NPCs:
		case 0x0B, 0x12, 0x16, 0x17, 0x1A, 0x1F, 0x28, 0x29, 0x2A, 0x2B, 0x2C, 0x2D, 0x2E, 0x2F, 0x30, 0x31, 0x32, 0x34, 0x35, 0x36, 0x39,
			0x3A, 0x3C, 0x3D, 0x3E, 0x3F, 0x52, 0x65, 0x73, 0x74, 0x75, 0x76, 0x78, 0xAD, 0xAB, 0xB4, 0xB5, 0xB6, 0xB7, 0xB8, 0xB9, 0xBB,
			0xBC, 0xBF, 0xC0, 0xC1, 0xC8, 0xD5:
			continue
		// items:
		case 0x33, 0x3B, 0x62, 0xAC, 0xB2:
			continue
		// pipes:
		case 0xAE, 0xAF, 0xB0, 0xB1:
			continue
		}

		// find abs coords for this enemy:
		yl, yh := read8(wram, 0x0D00+i), read8(wram, 0x0D20+i)
		xl, xh := read8(wram, 0x0D10+i), read8(wram, 0x0D30+i)
		y := uint16(yl) | uint16(yh)<<8
		x := uint16(xl) | uint16(xh)<<8

		// which layer the sprite is on (0 or 1):
		layer := read8(wram, 0x0F20+i)

		// find tilemap coords for this enemy:
		coord := AbsToMapCoord(x, y, uint16(layer))

		fmt.Printf("%s: enemy type=$%02X, pos=%s\n", st, et, coord)

		startCoords = append(startCoords, coord)

		coordLifo = append(coordLifo, coord)
		coordLifo = append(coordLifo, coord+0x01)
		coordLifo = append(coordLifo, coord+0x40)
		coordLifo = append(coordLifo, coord+0x41)
	}

	hasReachablePit := false
	for len(coordLifo) > 0 {
		lifoLen := len(coordLifo) - 1
		c := coordLifo[lifoLen]
		coordLifo = coordLifo[:lifoLen]

		// skip already visited tiles:
		if _, ok := room.TilesVisited[c]; ok {
			continue
		}
		// mark visited:
		room.TilesVisited[c] = empty{}

		t := tiles[c]

		if t == 0x20 {
			hasReachablePit = true
			room.Reachable[c] = t
		} else if room.isAlwaysWalkable(t) || room.isMaybeWalkable(c, t) {
			room.Reachable[c] = t
			if cn, _, ok := c.MoveBy(DirEast, 1); ok {
				coordLifo = append(coordLifo, cn)
			}
			if cn, _, ok := c.MoveBy(DirWest, 1); ok {
				coordLifo = append(coordLifo, cn)
			}
			if cn, _, ok := c.MoveBy(DirNorth, 1); ok {
				coordLifo = append(coordLifo, cn)
			}
			if cn, _, ok := c.MoveBy(DirSouth, 1); ok {
				coordLifo = append(coordLifo, cn)
			}
		}
	}

	// mark the enemy starting positions:
	for _, c := range startCoords {
		room.Reachable[c] = 0xFF
	}

	room.HasReachablePit = hasReachablePit

	if false {
		os.WriteFile(fmt.Sprintf("t%03X.tmap", uint16(st)), tiles, 0644)
	}
	room.DrawSupertile()
}
