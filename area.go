package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/image/draw"
	"golang.org/x/image/math/fixed"
)

type AreaID uint8

func (a AreaID) String() string {
	return fmt.Sprintf("OW$%02X", uint8(a))
}

func (a AreaID) RowCol() (row, col int) {
	row = int(a&0x38) >> 3
	col = int(a & 0x07)
	return
}

func (a AreaID) AbsXY(c OWCoord) (absX, absY int) {
	row, col := c.RowCol()
	arow, acol := a.RowCol()
	absY = (arow*0x40 + row) * 8
	absX = (acol*0x40 + col) * 8
	return
}

type Area struct {
	AreaID AreaID

	Width  int // width in 8x8 tiles
	Height int // height in 8x8 tiles

	IsLoaded bool

	// image rendering:
	Rendered      image.Image
	RenderedNRGBA *image.NRGBA

	// map data:
	Map8  [0x4000]uint16 // for presentation
	Tiles [0x4000]byte   // for tile types

	// flood fill state:
	TilesVisited  map[OWCoord]empty
	Reachable     [0x4000]byte
	Hookshot      [0x4000]uint8
	AllowDirFlags [0x4000]uint8

	// live emulator with its associated dynamic WRAM:
	// these CANNOT be safely used concurrently
	e    *System
	WRAM [0x20000]byte

	// static initial copies of WRAM and VRAM after loading the area:
	// these can be safely READ from concurrently AFTER area construction
	WRAMAfterLoaded [0x20000]byte
	VRAMAfterLoaded [0x10000]byte
	VRAMTileSet     [0x4000]byte

	Entrances    []AreaEntrance
	TileEntrance map[OWCoord]*AreaEntrance

	Mutex sync.Mutex
}

type AreaEntrance struct {
	OWCoord    OWCoord
	EntranceID uint8
	IsPit      bool
	Used       bool
}

func (a *Area) RowCol(c OWCoord) (row, col int) {
	return c.RowCol()
}

func (a *Area) Traverse(c OWCoord, d Direction, inc int) (OWCoord, Direction, bool) {
	it := int(c)
	row, col := a.RowCol(c)

	// don't allow perpendicular movement along the outer edge; this prevents accidental/leaky flood fill
	if (col <= 1 || col >= int(a.Width-1)) && (d != DirEast && d != DirWest) {
		return c, d, false
	} else if (row <= 1 || row >= int(a.Height)-1) && (d != DirNorth && d != DirSouth) {
		return c, d, false
	}

	switch d {
	case DirNorth:
		if row >= 0+inc {
			return OWCoord(it - (inc << 7)), d, true
		}
		return c, d, false
	case DirSouth:
		if row <= int(a.Height-1)-inc {
			return OWCoord(it + (inc << 7)), d, true
		}
		return c, d, false
	case DirWest:
		if col >= 0+inc {
			return OWCoord(it - inc), d, true
		}
		return c, d, false
	case DirEast:
		if col <= int(a.Width-1)-inc {
			return OWCoord(it + inc), d, true
		}
		return c, d, false
	default:
		panic("bad direction")
	}
}

// NeighborEdge determines:
// 1. if the neighbor area in the direction given is reachable
// 2. if Link is at an OWCoord that leads to an edge transition
// 3. the neighboring area's corrected AreaID (accounting for large areas)
// 4. the OWCoord in the new area at the opposite edge (assuming areas are same size)
// ed: edge direction
// nda: new area ID assuming the OW were divided into equal-sized areas (use Area#CorrectAreaID to fix)
// ok: if it is ok to transition on this edge
func (a *Area) NeighborEdge(c OWCoord, d Direction) (absX, absY int, na AreaID, ok bool) {
	// Hack to stop traversing from waterfall into TR light world:
	if a.AreaID == 0x0F && d == DirNorth {
		ok = false
		return
	}

	row, col := a.RowCol(c)
	absX, absY = a.AbsTileXY(c)

	// we move by 3 here to skip 1-tile collision borders around certain OW areas, e.g. north side of OW$10
	switch d {
	case DirNorth:
		ok = row == 1
		absY -= 3
		if absY < 0 {
			ok = false
			return
		}
	case DirSouth:
		ok = row == int(a.Height)-2
		absY += 3
		if absY > 0x1FF {
			ok = false
			return
		}
	case DirWest:
		ok = col == 1
		absX -= 3
		if absX < 0 {
			ok = false
			return
		}
	case DirEast:
		ok = col == int(a.Width)-2
		absX += 3
		if absX > 0x1FF {
			ok = false
			return
		}
	default:
		panic("bad direction")
	}

	// calculate desired areaID assuming all areas are the same size $40 x $40
	nda := AreaID(uint8((absY>>6)<<3) | uint8(absX>>6)&0x07 | (uint8(a.AreaID) & 0x40))
	// adjust to real areaID:
	na = a.CorrectAreaID(nda)

	return
}

func (a *Area) AbsTileXY(c OWCoord) (absX, absY int) {
	row, col := a.RowCol(c)
	arow, acol := a.AreaID.RowCol()
	absY = arow*0x40 + row
	absX = acol*0x40 + col
	return
}

func (a *Area) AbsXY(c OWCoord) (absX, absY int) {
	row, col := a.RowCol(c)
	arow, acol := a.AreaID.RowCol()
	absY = (arow*0x40 + row) * 8
	absX = (acol*0x40 + col) * 8
	return
}

func (a *Area) GetMap16At(c OWCoord) (vt uint16, ok bool) {
	row, col := a.RowCol(c)

	// only works for the top-left coord of each map16 block:
	if row&1 != 0 || col&1 != 0 {
		return 0, false
	}

	ok = true
	row >>= 1
	col >>= 1

	map16 := a.WRAM[0x2000:]
	vt = read16(map16, uint32(row)<<7|uint32(col)<<1)

	return
}

func (a *Area) OppositeEdge(c OWCoord) (ct OWCoord, ok bool) {
	row, col := a.RowCol(c)
	if row <= 1 {
		return RowColToOWCoord(a.Height-1-row, col), true
	}
	if row >= int(a.Height)-2 {
		return RowColToOWCoord(a.Height-1-row, col), true
	}
	if col <= 1 {
		return RowColToOWCoord(row, a.Width-1-col), true
	}
	if col >= int(a.Width)-2 {
		return RowColToOWCoord(row, a.Width-1-col), true
	}
	return c, false
}

func (a *Area) CorrectAreaID(aid AreaID) (na AreaID) {
	// entire overworld is 512 tiles x 512 tiles
	// each area is max $80 tiles wide x $80 tiles high

	// Overworld_ActualScreenID is an 8x8 grid of AreaIDs
	// look up real area id, accounting for the large areas:
	addr := alttp.Overworld_ActualScreenID + uint32(aid&0x3F)
	na = AreaID(a.e.Bus.Read8(addr) | (uint8(aid) & 0x40))

	return
}

var (
	validDoorTypes = [...]uint32{
		0x00FE_014A, 0x00C5_00C4, 0x00FE_014F, 0x0114_0115,
		0x0115_0114, 0x0175_0174, 0x0156_0155, 0x00F5_00F5,
		0x00E2_00EE, 0x01EF_01EB, 0x0119_0118, 0x00FE_0146,
		0x0172_0171, 0x0177_0155, 0x013F_0137, 0x0172_0174,
		0x0112_0173, 0x0161_0121, 0x0172_0164, 0x014C_0155,
		0x0156_0157, 0x01EF_0128, 0x00FE_0114, 0x00FE_0123,
		0x00FE_0113, 0x010B_0109, 0x0173_0118, 0x0143_0161,
		0x0149_0149, 0x0175_0117, 0x0103_0174, 0x0100_0101,
		0x01CC_01CC, 0x015E_0131, 0x0167_0051, 0x0128_014E,
		0x0131_0131, 0x0112_0112, 0x016D_017A, 0x0163_0163,
		0x0173_0172, 0x00FE_01BD, 0x0113_0152, 0x0177_0167,
	}
)

func (a *Area) IsDoorTypeAt(c OWCoord) bool {
	row, col := a.RowCol(c)
	if col >= int(a.Width)-1 {
		return false
	}
	if row >= int(a.Height)-1 {
		return false
	}

	v := uint32(a.Map8[c]&0x01FF)<<16 | uint32(a.Map8[c+1]&0x01FF)
	for _, t := range validDoorTypes {
		if v == t {
			return true
		}
	}
	return false
}

// isAlwaysWalkable checks if the tile is always walkable on, regardless of state
func (a *Area) isAlwaysWalkable(v uint8) bool {
	return v == 0x00 || // no collision
		v == 0x09 || // shallow water
		v == 0x22 || // manual stairs
		v == 0x23 || v == 0x24 || // floor switches
		(v >= 0x0D && v <= 0x0F) || // spikes / floor ice
		v == 0x3A || v == 0x3B || // star tiles
		v == 0x40 || // thick grass
		v == 0x48 || // diggable ground
		v == 0x4B || // warp
		v == 0x60 || // rupee tile
		(v >= 0x68 && v <= 0x6B) || // conveyors
		v == 0xA0 || // north/south dungeon swap door (for HC to sewers)
		v&0xF0 == 0x70 || // pots/pegs/blocks
		v == 0x62 || // bombable floor
		v == 0x66 || v == 0x67 || // crystal pegs (orange/blue)
		v == 0x50 // bushes
}

func (a *Area) canHookThru(v uint8) bool {
	return v == 0x00 || // no collision
		v == 0x08 || v == 0x09 || // water
		(v >= 0x0D && v <= 0x0F) || // spikes / floor ice
		v == 0x1C || v == 0x0C || // layer pass through
		v == 0x20 || // pit
		v == 0x22 || // manual stairs
		v == 0x23 || v == 0x24 || // floor switches
		(v >= 0x28 && v <= 0x2B) || // ledge tiles
		v == 0x3A || v == 0x3B || // star tiles
		v == 0x40 || // thick grass
		v == 0x4B || // warp
		v == 0x60 || // rupee tile
		(v >= 0x68 && v <= 0x6B) || // conveyors
		v == 0xB6 || // somaria start
		v == 0xBC // somaria start
}

// isHookable determines if the tile can be attached to with a hookshot
func (a *Area) isHookable(v uint8) bool {
	return v == 0x27 || // general hookable object
		v == 0x02 ||
		(v >= 0x58 && v <= 0x5D) || // chests (TODO: check $0500 table for kind)
		v&0xF0 == 0x70 // pot/peg/block
}

func (a *Area) Render() {
	a.RenderedNRGBA = image.NewNRGBA(image.Rect(0, 0, int(a.Width)*8, int(a.Height)*8))
	g := a.RenderedNRGBA

	{
		cgram := (*(*[0x100]uint16)(unsafe.Pointer(&a.WRAM[0xC300])))[:]
		pal := cgramToPalette(cgram)
		bg1 := [2]*image.Paletted{
			image.NewPaletted(image.Rect(0, 0, int(a.Width)*8, int(a.Height)*8), pal),
			image.NewPaletted(image.Rect(0, 0, int(a.Width)*8, int(a.Height)*8), pal),
		}
		bg2 := [2]*image.Paletted{
			image.NewPaletted(image.Rectangle{}, nil),
			image.NewPaletted(image.Rectangle{}, nil),
		}
		renderMap8(bg1, int(a.Width), int(a.Height), a.Map8[:], a.VRAMTileSet[:], drawBG1p0, drawBG1p1)

		// compose the priority layers:
		ComposeToNonPalettedOW(g, pal, bg1, bg2, int(a.Width), false, false)
	}

	for {
		e := System{}
		e.InitEmulatorFrom(a.e)

		m, sm := read8(e.WRAM[:], 0x10), read8(e.WRAM[:], 0x11)
		if m != 0x09 || sm != 0x00 {
			// we require submodule 00 to render sprites with:
			break
		}

		// set data bank:
		e.CPU.RDBR = 0x06

		tlbgX, tlbgY := a.AbsXY(OWCoord(0))

		// move in half-screen increments horizontally and vertically:
		for row := 0; row < a.Height-0x20; row += 0x10 {
			for col := 0; col < a.Width-0x20; col += 0x10 {
				// set BG coordinates:
				bgX, bgY := a.AbsXY(RowColToOWCoord(row, col))
				fmt.Printf("%s: sprites: bg=(%04X, %04X)\n", a.AreaID, bgX, bgY)
				e.Bus.Write16(0x7E00E2, uint16(bgX))
				e.Bus.Write16(0x7E00E8, uint16(bgY))

				// set link x/y coords for collision detection:
				lkX, lkY := bgX+0x80, bgY+0x78
				e.Bus.Write16(0x7E0022, uint16(lkX)) // X
				e.Bus.Write16(0x7E0020, uint16(lkY)) // Y

				// clear i-frames and invuln:
				e.WRAM[0x031F] = 0
				e.WRAM[0x037B] = 0

				// execute Sprite_Main up until `LDX #$0F` which begins sprite_executesingle loop:
				if err := e.ExecAtUntil(0x068328|fastRomBank, 0x06839F|fastRomBank, 0x200000); err != nil {
					fmt.Fprintln(os.Stderr, err)
				} else {
					for i := 0; i < 16; i++ {
						// clear OAM slots:
						for j := 0; j < 0x80; j++ {
							e.WRAM[0x0800+(j<<2)+0] = 0
							e.WRAM[0x0800+(j<<2)+1] = 0xF0
							e.WRAM[0x0800+(j<<2)+2] = 0
							e.WRAM[0x0800+(j<<2)+3] = 0
							e.WRAM[0x0A20+j] = 0
						}

						// set X register to sprite slot:
						e.Bus.Write16(0x7E0FA0, uint16(i))
						e.CPU.RX = uint16(i)
						e.CPU.RXl = uint8(i)

						// 8 bit mode:
						e.CPU.M = 1
						e.CPU.X = 1

						// 0x0684DD // JSR Sprite_ExecuteSingle with X=sprite slot
						spriteExecuteSingle := 0x0683A4 | fastRomBank

						// execute logic for the sprite:
						if err := e.ExecAtUntil(spriteExecuteSingle, spriteExecuteSingle+3, 0x200000); err != nil {
							fmt.Fprintln(os.Stderr, err)
						} else {
							renderOAMSprites(
								g,
								(*[0x100]uint16)(unsafe.Pointer(&e.WRAM[0xC500])),
								e.VRAM,
								e.HWIO.PPU.ObjTilemapAddress,
								e.HWIO.PPU.ObjNameSelect,
								(*[0x200]byte)(unsafe.Pointer(&e.WRAM[0x0800])),
								(*[0x80]byte)(unsafe.Pointer(&e.WRAM[0x0A20])),
								bgX-tlbgX,
								bgY-tlbgY,
							)
						}
					}
				}
			} // col
		} // row

		break
	}

	a.Rendered = g
}

func (a *Area) DrawOverlays() {
	if !drawOverlays {
		return
	}

	greenTint := image.NewUniform(color.NRGBA{255, 255, 0, 64})
	redTint := image.NewUniform(color.NRGBA{255, 0, 0, 96})
	// cyanTint := image.NewUniform(color.NRGBA{0, 255, 255, 64})
	// blueTint := image.NewUniform(color.NRGBA{0, 0, 255, 48})

	for c := 0; c < 0x4000; c++ {
		v := a.Reachable[c]
		if v == 0x01 {
			continue
		}

		tt := OWCoord(c)
		tr, tc := a.RowCol(tt)
		overlay := greenTint
		if v == 0x20 || v == 0x62 {
			overlay = redTint
		}

		x := int(tc) << 3
		y := int(tr) << 3
		draw.Draw(
			a.RenderedNRGBA,
			image.Rect(x, y, x+8, y+8),
			overlay,
			image.Point{},
			draw.Over,
		)
	}

	// yellow boxes around entrances:
	yellowTint := image.NewUniform(color.NRGBA{255, 255, 0, 192})
	for _, ent := range a.Entrances {
		row, col := a.RowCol(ent.OWCoord)

		w, h := 4, 4
		if ent.IsPit {
			h = 2
		}

		drawShadowedString(
			a.RenderedNRGBA,
			yellowTint,
			fixed.Point26_6{
				X: fixed.I(int(col*8) + (w-2)*4),
				Y: fixed.I(int(row*8) - 2),
			},
			fmt.Sprintf("%02X", uint8(ent.EntranceID)),
		)
		drawOutlineBox(
			a.RenderedNRGBA,
			yellowTint,
			col*8,
			row*8,
			8*w,
			8*h,
		)
		drawOutlineBox(
			a.RenderedNRGBA,
			yellowTint,
			col*8-1,
			row*8-1,
			8*w+2,
			8*h+2,
		)
	}
}
