package main

import (
	"bufio"
	"fmt"
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/inconsolata"
	"golang.org/x/image/math/fixed"
	"image"
	"image/color"
	"image/png"
	"os"
	"sync"
	"unsafe"
)

func renderAll(fname string, entranceGroups []Entrance, rowStart int, rowCount int) {
	var err error

	const divider = 1
	supertilepx := 512 / divider

	wga := &sync.WaitGroup{}

	all := image.NewNRGBA(image.Rect(0, 0, 0x10*supertilepx, (rowCount*0x10*supertilepx)/0x10))
	// clear the image and remove alpha layer
	draw.Draw(
		all,
		all.Bounds(),
		image.NewUniform(color.NRGBA{0, 0, 0, 255}),
		image.Point{},
		draw.Src)

	greenTint := image.NewUniform(color.NRGBA{0, 255, 0, 64})
	redTint := image.NewUniform(color.NRGBA{255, 0, 0, 56})
	cyanTint := image.NewUniform(color.NRGBA{0, 255, 255, 64})
	blueTint := image.NewUniform(color.NRGBA{0, 0, 255, 64})

	black := image.NewUniform(color.RGBA{0, 0, 0, 255})
	yellow := image.NewUniform(color.RGBA{255, 255, 0, 255})
	white := image.NewUniform(color.RGBA{255, 255, 255, 255})

	for i := range entranceGroups {
		g := &entranceGroups[i]
		for _, room := range g.Rooms {
			st := int(room.Supertile)

			row := st/0x10 - rowStart
			col := st % 0x10
			if row < 0 || row >= rowCount {
				continue
			}

			wga.Add(1)
			go func(room *RoomState) {
				defer wga.Done()

				fmt.Printf("entrance $%02x supertile %s render start\n", g.EntranceID, room.Supertile)

				stx := col * supertilepx
				sty := row * supertilepx

				if room.Rendered != nil {
					draw.Draw(
						all,
						image.Rect(stx, sty, stx+supertilepx, sty+supertilepx),
						room.Rendered,
						image.Point{},
						draw.Src,
					)
				}

				// highlight tiles that are reachable:
				if drawOverlays {
					maxRange := 0x2000
					if room.IsDarkRoom() {
						maxRange = 0x1000
					}

					// draw supertile over pits, bombable floors, and warps:
					for j := range room.ExitPoints {
						ep := &room.ExitPoints[j]
						if !ep.WorthMarking {
							continue
						}

						_, er, ec := ep.Point.RowCol()
						x := int(ec) << 3
						y := int(er) << 3
						fd0 := font.Drawer{
							Dst:  all,
							Src:  black,
							Face: inconsolata.Regular8x16,
							Dot:  fixed.Point26_6{fixed.I(stx + x + 1), fixed.I(sty + y + 1)},
						}
						fd1 := font.Drawer{
							Dst:  all,
							Src:  yellow,
							Face: inconsolata.Regular8x16,
							Dot:  fixed.Point26_6{fixed.I(stx + x), fixed.I(sty + y)},
						}
						stStr := fmt.Sprintf("%02X", uint16(ep.Supertile))
						fd0.DrawString(stStr)
						fd1.DrawString(stStr)
					}

					// draw supertile over stairs:
					for j := range room.Stairs {
						sn := room.StairExitTo[j]
						_, er, ec := room.Stairs[j].RowCol()

						x := int(ec) << 3
						y := int(er) << 3
						fd0 := font.Drawer{
							Dst:  all,
							Src:  black,
							Face: inconsolata.Regular8x16,
							Dot:  fixed.Point26_6{fixed.I(stx + 8 + x + 1), fixed.I(sty - 8 + y + 1 + 12)},
						}
						fd1 := font.Drawer{
							Dst:  all,
							Src:  yellow,
							Face: inconsolata.Regular8x16,
							Dot:  fixed.Point26_6{fixed.I(stx + 8 + x), fixed.I(sty - 8 + y + 12)},
						}
						stStr := fmt.Sprintf("%02X", uint16(sn))
						fd0.DrawString(stStr)
						fd1.DrawString(stStr)
					}

					for t := 0; t < maxRange; t++ {
						v := room.Reachable[t]
						if v == 0x01 {
							continue
						}

						tt := MapCoord(t)
						lyr, tr, tc := tt.RowCol()
						overlay := greenTint
						if lyr != 0 {
							overlay = cyanTint
						}
						if v == 0x20 || v == 0x62 {
							overlay = redTint
						}

						x := int(tc) << 3
						y := int(tr) << 3
						draw.Draw(
							all,
							image.Rect(stx+x, sty+y, stx+x+8, sty+y+8),
							overlay,
							image.Point{},
							draw.Over,
						)
					}

					for t, d := range room.Hookshot {
						_, tr, tc := t.RowCol()
						x := int(tc) << 3
						y := int(tr) << 3

						overlay := blueTint
						_ = d

						draw.Draw(
							all,
							image.Rect(stx+x, sty+y, stx+x+8, sty+y+8),
							overlay,
							image.Point{},
							draw.Over,
						)
					}
				}

				fmt.Printf("entrance $%02x supertile %s render complete\n", g.EntranceID, room.Supertile)
			}(room)
		}
	}
	wga.Wait()

	for r := 0; r < 0x128; r++ {
		wga.Add(1)
		go func(st int) {
			defer wga.Done()

			row := st/0x10 - rowStart
			col := st % 0x10
			if row < 0 || row >= rowCount {
				return
			}

			stx := col * supertilepx
			sty := row * supertilepx

			// draw supertile number in top-left:
			var stStr string
			if st < 0x100 {
				stStr = fmt.Sprintf("%02X", st)
			} else {
				stStr = fmt.Sprintf("%03X", st)
			}
			(&font.Drawer{
				Dst:  all,
				Src:  black,
				Face: inconsolata.Bold8x16,
				Dot:  fixed.Point26_6{fixed.I(stx + 5), fixed.I(sty + 5 + 12)},
			}).DrawString(stStr)
			(&font.Drawer{
				Dst:  all,
				Src:  white,
				Face: inconsolata.Bold8x16,
				Dot:  fixed.Point26_6{fixed.I(stx + 4), fixed.I(sty + 4 + 12)},
			}).DrawString(stStr)
		}(r)
	}
	wga.Wait()

	if err = exportPNG(fmt.Sprintf("data/%s.png", fname), all); err != nil {
		panic(err)
	}
}

func drawSupertile(wg *sync.WaitGroup, g *Entrance, room *RoomState) {
	fmt.Printf("entrance $%02x supertile %s draw start\n", g.EntranceID, room.Supertile)

	defer func() {
		fmt.Printf("entrance $%02x supertile %s draw complete\n", g.EntranceID, room.Supertile)
		wg.Done()
	}()

	// gfx output is:
	//  s.VRAM: $4000[0x2000] = 4bpp tile graphics
	//  s.WRAM: $2000[0x2000] = BG1 64x64 tile map  [64][64]uint16
	//  s.WRAM: $4000[0x2000] = BG2 64x64 tile map  [64][64]uint16
	//  s.WRAM:$12000[0x1000] = BG1 64x64 tile type [64][64]uint8
	//  s.WRAM:$12000[0x1000] = BG2 64x64 tile type [64][64]uint8
	//  s.WRAM: $C300[0x0200] = CGRAM palette

	wram := (&room.WRAM)[:]

	// assume WRAM has rendering state as well:
	isDark := room.IsDarkRoom()

	//ioutil.WriteFile(fmt.Sprintf("data/%03X.vram", st), vram, 0644)

	cgram := (*(*[0x100]uint16)(unsafe.Pointer(&wram[0xC300])))[:]
	pal := cgramToPalette(cgram)

	// render BG image:
	if room.Rendered == nil {
		g := image.NewNRGBA(image.Rect(0, 0, 512, 512))
		bg1 := image.NewPaletted(image.Rect(0, 0, 512, 512), pal)
		bg2 := image.NewPaletted(image.Rect(0, 0, 512, 512), pal)
		doBG2 := !isDark

		bg1wram := (*(*[0x1000]uint16)(unsafe.Pointer(&wram[0x2000])))[:]
		bg2wram := (*(*[0x1000]uint16)(unsafe.Pointer(&wram[0x4000])))[:]
		tileset := (&room.VRAMTileSet)[:]

		//subdes := read8(wram, 0x1D)
		n0414 := read8(wram, 0x0414)
		translucent := n0414 == 0x07
		halfColor := n0414 == 0x04
		flip := n0414 == 0x03
		if translucent || halfColor {
			// render bg1 and bg2 separately

			// draw from back to front order:
			// BG2 priority 0:
			if doBG2 {
				renderBG(bg2, bg2wram, tileset, 0)
			}

			// BG1 priority 0:
			renderBG(bg1, bg1wram, tileset, 0)

			// BG2 priority 1:
			if doBG2 {
				renderBG(bg2, bg2wram, tileset, 1)
			}

			// BG1 priority 1:
			renderBG(bg1, bg1wram, tileset, 1)

			// combine bg1 and bg2:
			sat := func(v uint32) uint16 {
				if v > 0xffff {
					return 0xffff
				}
				return uint16(v)
			}

			if halfColor {
				// color math: add, half
				for y := 0; y < 512; y++ {
					for x := 0; x < 512; x++ {
						if bg2.ColorIndexAt(x, y) != 0 {
							r1, g1, b1, _ := bg1.At(x, y).RGBA()
							r2, g2, b2, _ := bg2.At(x, y).RGBA()
							c := color.RGBA64{
								R: sat(r1>>1 + r2>>1),
								G: sat(g1>>1 + g2>>1),
								B: sat(b1>>1 + b2>>1),
								A: 0xffff,
							}
							g.Set(x, y, c)
						} else {
							g.Set(x, y, bg1.At(x, y))
						}
					}
				}
			} else {
				// color math: add
				for y := 0; y < 512; y++ {
					for x := 0; x < 512; x++ {
						r1, g1, b1, _ := bg1.At(x, y).RGBA()
						r2, g2, b2, _ := bg2.At(x, y).RGBA()
						c := color.RGBA64{
							R: sat(r1 + r2),
							G: sat(g1 + g2),
							B: sat(b1 + b2),
							A: 0xffff,
						}
						g.Set(x, y, c)
					}
				}
			}
		} else if flip {
			// draw from back to front order:

			// BG1 priority 1:
			renderBG(bg1, bg1wram, tileset, 1)

			// BG1 priority 0:
			renderBG(bg1, bg1wram, tileset, 0)

			// BG2 priority 1:
			if doBG2 {
				renderBG(bg1, bg2wram, tileset, 1)
			}

			// BG2 priority 0:
			if doBG2 {
				renderBG(bg1, bg2wram, tileset, 0)
			}

			draw.Draw(g, g.Bounds(), bg1, image.Point{}, draw.Src)
		} else {
			// draw from back to front order:
			// BG2 priority 0:
			if doBG2 {
				renderBG(bg1, bg2wram, tileset, 0)
			}

			// BG1 priority 0:
			renderBG(bg1, bg1wram, tileset, 0)

			// BG2 priority 1:
			if doBG2 {
				renderBG(bg1, bg2wram, tileset, 1)
			}

			// BG1 priority 1:
			renderBG(bg1, bg1wram, tileset, 1)

			draw.Draw(g, g.Bounds(), bg1, image.Point{}, draw.Src)
		}

		//if isDark {
		//	// darken the room
		//	draw.Draw(
		//		g,
		//		g.Bounds(),
		//		image.NewUniform(color.RGBA64{0, 0, 0, 0x8000}),
		//		image.Point{},
		//		draw.Over,
		//	)
		//}

		// INIDISP contains PPU brightness
		brightness := read8(wram, 0x13) & 0xF
		if brightness < 15 {
			draw.Draw(
				g,
				g.Bounds(),
				image.NewUniform(color.RGBA64{0, 0, 0, uint16(brightness) << 12}),
				image.Point{},
				draw.Over,
			)
		}

		// store full underworld rendering for inclusion into EG map:
		room.Rendered = g

		//if err = exportPNG(fmt.Sprintf("data/%03X.png", uint16(room.Supertile)), g); err != nil {
		//	panic(err)
		//}
	}
}

func exportPNG(name string, g image.Image) (err error) {
	// export to PNG:
	var po *os.File

	po, err = os.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer func() {
		err = po.Close()
		if err != nil {
			return
		}
	}()

	bo := bufio.NewWriterSize(po, 8*1024*1024)

	err = png.Encode(bo, g)
	if err != nil {
		return
	}

	err = bo.Flush()
	if err != nil {
		return
	}

	return
}

var gammaRamp = [...]uint8{
	0x00, 0x01, 0x03, 0x06, 0x0a, 0x0f, 0x15, 0x1c,
	0x24, 0x2d, 0x37, 0x42, 0x4e, 0x5b, 0x69, 0x78,
	0x88, 0x90, 0x98, 0xa0, 0xa8, 0xb0, 0xb8, 0xc0,
	0xc8, 0xd0, 0xd8, 0xe0, 0xe8, 0xf0, 0xf8, 0xff,
}

func cgramToPalette(cgram []uint16) color.Palette {
	pal := make(color.Palette, 256)
	for i, bgr15 := range cgram {
		// convert BGR15 color format (MSB unused) to RGB24:
		b := (bgr15 & 0x7C00) >> 10
		g := (bgr15 & 0x03E0) >> 5
		r := bgr15 & 0x001F
		if false {
			pal[i] = color.NRGBA{
				R: gammaRamp[r],
				G: gammaRamp[g],
				B: gammaRamp[b],
				A: 0xff,
			}
		} else {
			pal[i] = color.NRGBA{
				R: uint8(r<<3 | r>>2),
				G: uint8(g<<3 | g>>2),
				B: uint8(b<<3 | b>>2),
				A: 0xff,
			}
		}
	}
	return pal
}

func renderBG(g *image.Paletted, bg []uint16, tiles []uint8, prio uint8) {
	a := uint32(0)
	for ty := 0; ty < 64; ty++ {
		for tx := 0; tx < 64; tx++ {
			z := bg[a]
			a++

			// priority check:
			if (z&0x2000 != 0) != (prio != 0) {
				continue
			}

			draw4bppTile(g, z, tiles, tx, ty)
		}
	}
}

func draw4bppTile(g *image.Paletted, z uint16, tiles []uint8, tx int, ty int) {
	//High     Low          Legend->  c: Starting character (tile) number
	//vhopppcc cccccccc               h: horizontal flip  v: vertical flip
	//                                p: palette number   o: priority bit

	p := byte((z>>10)&7) << 4
	c := int(z & 0x03FF)
	for y := 0; y < 8; y++ {
		fy := y
		if z&0x8000 != 0 {
			fy = 7 - y
		}
		p0 := tiles[(c<<5)+(y<<1)]
		p1 := tiles[(c<<5)+(y<<1)+1]
		p2 := tiles[(c<<5)+(y<<1)+16]
		p3 := tiles[(c<<5)+(y<<1)+17]
		for x := 0; x < 8; x++ {
			fx := x
			if z&0x4000 == 0 {
				fx = 7 - x
			}

			i := byte((p0>>x)&1) |
				byte(((p1>>x)&1)<<1) |
				byte(((p2>>x)&1)<<2) |
				byte(((p3>>x)&1)<<3)

			// transparency:
			if i == 0 {
				continue
			}

			g.SetColorIndex(tx<<3+fx, ty<<3+fy, p+i)
		}
	}
}
