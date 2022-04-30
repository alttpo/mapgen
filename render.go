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
	"image/gif"
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

	if drawNumbers {
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
	}

	if err = exportPNG(fmt.Sprintf("data/%s.png", fname), all); err != nil {
		panic(err)
	}
}

func (room *RoomState) DrawSupertile() {
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

	// INIDISP contains PPU brightness
	brightness := read8(wram, 0x13) & 0xF
	_ = brightness

	//ioutil.WriteFile(fmt.Sprintf("data/%03X.vram", st), vram, 0644)

	cgram := (*(*[0x100]uint16)(unsafe.Pointer(&wram[0xC300])))[:]
	pal := cgramToPalette(cgram)

	palTransp := pal
	palTransp[0] = color.Transparent

	// render BG image:

	bg1p := [2]*image.Paletted{
		image.NewPaletted(image.Rect(0, 0, 512, 512), pal),
		image.NewPaletted(image.Rect(0, 0, 512, 512), pal),
	}
	bg2p := [2]*image.Paletted{
		image.NewPaletted(image.Rect(0, 0, 512, 512), pal),
		image.NewPaletted(image.Rect(0, 0, 512, 512), pal),
	}

	doBG2 := !isDark

	bg1wram := (*(*[0x1000]uint16)(unsafe.Pointer(&wram[0x2000])))[:]
	bg2wram := (*(*[0x1000]uint16)(unsafe.Pointer(&wram[0x4000])))[:]
	tileset := (&room.VRAMTileSet)[:]

	// render all separate BG1 and BG2 priority layers:
	renderBGsep(bg1p, bg1wram, tileset)
	if doBG2 {
		renderBGsep(bg2p, bg2wram, tileset)
	}

	order := [4]*image.Paletted{bg2p[0], bg1p[0], bg2p[1], bg1p[1]}

	//subdes := read8(wram, 0x1D)
	n0414 := read8(wram, 0x0414)
	addColor := n0414 == 0x07
	halfColor := n0414 == 0x04
	flip := n0414 == 0x03

	if flip || addColor || halfColor {
		// draw from back to front order:
		// bg1[1]
		// bg1[0]
		// bg2[1]
		// bg2[0]
		//order = [4]*image.Paletted{bg1p[1], bg1p[0], bg2p[1], bg2p[0]}
		order = [4]*image.Paletted{bg1p[0], bg1p[1], bg2p[0], bg2p[1]}
	} else {
		// draw from back to front order:
		// bg2[0]
		// bg1[0]
		// bg2[1]
		// bg1[1]
		//order = [4]*image.Paletted{bg2p[0], bg1p[0], bg2p[1], bg1p[1]}
		order = [4]*image.Paletted{bg2p[0], bg2p[1], bg1p[0], bg1p[1]}
	}

	if room.Rendered != nil {
		// subsequent GIF frames:
		frame := renderBGComposedPaletted(pal, order, addColor, halfColor)

		room.GIF.Image = append(room.GIF.Image, frame)
		room.GIF.Delay = append(room.GIF.Delay, 50)
		room.GIF.Disposal = append(room.GIF.Disposal, gif.DisposalNone)

		return
	}

	// switch everything but the first layer to have 0 as transparent:
	order[0].Palette = pal
	for p := 1; p < 4; p++ {
		order[p].Palette = palTransp
	}

	blankFrame := newBlankFrame()

	// first GIF frames build up the layers:
	frames := [4]*image.Paletted{
		renderBGComposedPaletted(pal, [4]*image.Paletted{order[0], blankFrame, blankFrame, blankFrame}, addColor, halfColor),
		renderBGComposedPaletted(pal, [4]*image.Paletted{order[0], order[1], blankFrame, blankFrame}, addColor, halfColor),
		renderBGComposedPaletted(pal, [4]*image.Paletted{order[0], order[1], order[2], blankFrame}, addColor, halfColor),
		renderBGComposedPaletted(pal, [4]*image.Paletted{order[0], order[1], order[2], order[3]}, addColor, halfColor),
	}

	room.GIF.Image = append(room.GIF.Image, frames[:]...)
	room.GIF.Delay = append(room.GIF.Delay, 50, 50, 50, 50)
	room.GIF.Disposal = append(room.GIF.Disposal, 0, 0, 0, 0)

	g := image.NewNRGBA(image.Rect(0, 0, 512, 512))

	if halfColor {
		// color math: add, half
		for y := 0; y < 512; y++ {
			for x := 0; x < 512; x++ {
				bg1c := pick(bg1p[0].ColorIndexAt(x, y), bg1p[1].ColorIndexAt(x, y))
				bg2c := pick(bg2p[0].ColorIndexAt(x, y), bg2p[1].ColorIndexAt(x, y))
				if bg2c != 0 {
					r1, g1, b1, _ := pal[bg1c].RGBA()
					r2, g2, b2, _ := pal[bg2c].RGBA()
					c := color.RGBA64{
						R: sat(r1>>1 + r2>>1),
						G: sat(g1>>1 + g2>>1),
						B: sat(b1>>1 + b2>>1),
						A: 0xffff,
					}
					g.Set(x, y, c)
				} else {
					g.Set(x, y, pal[bg1c])
				}
			}
		}
	} else if addColor {
		// color math: add
		for y := 0; y < 512; y++ {
			for x := 0; x < 512; x++ {
				bg1c := pick(bg1p[0].ColorIndexAt(x, y), bg1p[1].ColorIndexAt(x, y))
				bg2c := pick(bg2p[0].ColorIndexAt(x, y), bg2p[1].ColorIndexAt(x, y))
				r1, g1, b1, _ := pal[bg1c].RGBA()
				r2, g2, b2, _ := pal[bg2c].RGBA()
				c := color.RGBA64{
					R: sat(r1 + r2),
					G: sat(g1 + g2),
					B: sat(b1 + b2),
					A: 0xffff,
				}
				g.Set(x, y, c)
			}
		}
	} else {
		// no color math:
		for y := 0; y < 512; y++ {
			for x := 0; x < 512; x++ {
				c0 := order[0].ColorIndexAt(x, y)
				c1 := order[1].ColorIndexAt(x, y)
				c2 := order[2].ColorIndexAt(x, y)
				c3 := order[3].ColorIndexAt(x, y)
				c := pick(pick(c0, c1), pick(c2, c3))
				g.Set(x, y, pal[c])
			}
		}
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

	//if brightness < 15 {
	//	draw.Draw(
	//		g,
	//		g.Bounds(),
	//		image.NewUniform(color.RGBA64{0, 0, 0, uint16(brightness) << 12}),
	//		image.Point{},
	//		draw.Over,
	//	)
	//}

	// store full underworld rendering for inclusion into EG map:
	room.Rendered = g

	func() {
		if err := exportPNG(fmt.Sprintf("data/%03X.png", uint16(room.Supertile)), g); err != nil {
			panic(err)
		}

		if err := exportPNG(fmt.Sprintf("data/%03X.bg1.0.png", uint16(room.Supertile)), bg1p[0]); err != nil {
			panic(err)
		}
		if err := exportPNG(fmt.Sprintf("data/%03X.bg1.1.png", uint16(room.Supertile)), bg1p[1]); err != nil {
			panic(err)
		}
		if err := exportPNG(fmt.Sprintf("data/%03X.bg2.0.png", uint16(room.Supertile)), bg2p[0]); err != nil {
			panic(err)
		}
		if err := exportPNG(fmt.Sprintf("data/%03X.bg2.1.png", uint16(room.Supertile)), bg2p[1]); err != nil {
			panic(err)
		}
	}()
}

func newBlankFrame() *image.Paletted {
	return image.NewPaletted(
		image.Rect(0, 0, 512, 512),
		color.Palette{color.Transparent},
	)
}

// saturate a 16-bit value:
func sat(v uint32) uint16 {
	if v > 0xffff {
		return 0xffff
	}
	return uint16(v)
}

// prefer p1's color unless it's zero:
func pick(c0, c1 uint8) uint8 {
	if c1 != 0 {
		return c1
	} else {
		return c0
	}
}

func renderBGComposedPaletted(
	pal color.Palette,
	order [4]*image.Paletted,
	addColor bool,
	halfColor bool,
) *image.Paletted {
	frame := image.NewPaletted(image.Rect(0, 0, 512, 512), pal)

	// store mixed colors in second half of palette which is unused by BG layers:
	hc := uint8(128)
	mixedColors := make(map[uint16]uint8, 0x200)

	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			c0 := order[0].ColorIndexAt(x, y)
			c1 := order[1].ColorIndexAt(x, y)
			c2 := order[2].ColorIndexAt(x, y)
			c3 := order[3].ColorIndexAt(x, y)

			m1 := pick(c0, c1)
			m2 := pick(c2, c3)

			var c uint8
			if addColor || halfColor {
				if m2 == 0 {
					c = m1
				} else {
					key := uint16(m1) | uint16(m2)<<8

					var ok bool
					if c, ok = mixedColors[key]; !ok {
						c = hc
						r1, g1, b1, _ := pal[m1].RGBA()
						r2, g2, b2, _ := pal[m2].RGBA()
						if halfColor {
							pal[c] = color.RGBA64{
								R: sat(r1>>1 + r2>>1),
								G: sat(g1>>1 + g2>>1),
								B: sat(b1>>1 + b2>>1),
								A: 0xffff,
							}
						} else {
							pal[c] = color.RGBA64{
								R: sat(r1 + r2),
								G: sat(g1 + g2),
								B: sat(b1 + b2),
								A: 0xffff,
							}
						}
						mixedColors[key] = c
						hc++
					}
				}
			} else {
				c = pick(m1, m2)
			}

			frame.SetColorIndex(x, y, c)
		}
	}
	frame.Palette = pal

	return frame
}

func (room *RoomState) RenderGIF() {
	// present last frame for 3 seconds:
	f := len(room.GIF.Delay) - 1
	if f >= 0 {
		room.GIF.Delay[f] = 300
	}

	// render GIF:
	gw, err := os.OpenFile(
		fmt.Sprintf("data/%03x.gif", uint16(room.Supertile)),
		os.O_TRUNC|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		panic(err)
	}
	defer gw.Close()

	err = gif.EncodeAll(gw, &room.GIF)
	if err != nil {
		panic(err)
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

func renderBGsep(g [2]*image.Paletted, bg []uint16, tiles []uint8) {
	a := uint32(0)
	for ty := 0; ty < 64; ty++ {
		for tx := 0; tx < 64; tx++ {
			z := bg[a]
			a++

			// priority check:
			p := (z & 0x2000) >> 13
			draw4bppTile(g[p], z, tiles, tx, ty)
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
