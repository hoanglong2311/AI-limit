// Command background draws the dmg window background (600x380 at 1x and
// 1200x760 at 2x): a soft gradient, an arrow from the app icon to the
// Applications link and a Vietnamese hint. Positions match dsstore.py.
//
//	go run ./scripts/dmg/background out.png scale font.ttf
//
// The Go fonts lack Vietnamese glyphs, so pass a TTF that has them (the
// committed scripts/dmg/background*.png were made with Noto Sans).
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"strconv"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	W, H       = 600, 380
	AppX, AppY = 160, 170 // icon centers, keep in sync with dsstore.py
	LnkX       = 440
)

func main() {
	if len(os.Args) < 4 {
		log.Fatal("usage: background out.png scale font.ttf")
	}
	s := 1.0
	s, _ = strconv.ParseFloat(os.Args[2], 64)
	ttf, err := os.ReadFile(os.Args[3])
	if err != nil {
		log.Fatal(err)
	}
	w, h := int(W*s), int(H*s)
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		t := float64(y) / float64(h)
		c := color.NRGBA{uint8(250 - 14*t), uint8(251 - 12*t), uint8(253 - 8*t), 255}
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}
	// Arrow: shaft plus head, anti-aliased by supersampled coverage.
	ink := color.NRGBA{120, 128, 140, 255}
	x0, x1, y := (AppX+70)*s, (LnkX-70)*s, AppY*s
	half := 3 * s
	head := 16 * s
	for py := int(y - head); py <= int(y+head); py++ {
		for px := int(x0); px <= int(x1)+1; px++ {
			cov := 0.0
			for sy := range 4 {
				for sx := range 4 {
					fx, fy := float64(px)+(float64(sx)+.5)/4, float64(py)+(float64(sy)+.5)/4
					in := fx >= x0 && fx <= x1-head && math.Abs(fy-y) <= half
					if fx > x1-head && fx <= x1 && math.Abs(fy-y) <= (x1-fx) {
						in = true
					}
					if in {
						cov++
					}
				}
			}
			if cov > 0 {
				a := cov / 16
				b := img.NRGBAAt(px, py)
				mix := func(f, g uint8) uint8 { return uint8(float64(f)*(1-a) + float64(g)*a) }
				img.SetNRGBA(px, py, color.NRGBA{mix(b.R, ink.R), mix(b.G, ink.G), mix(b.B, ink.B), 255})
			}
		}
	}
	f, err := opentype.Parse(ttf)
	if err != nil {
		log.Fatal(err)
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 15 * s, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		log.Fatal(err)
	}
	msg := "Kéo AI-limit vào thư mục Applications để cài đặt"
	d := font.Drawer{Dst: img, Src: image.NewUniform(color.NRGBA{90, 96, 108, 255}), Face: face}
	d.Dot = fixed.P((w-d.MeasureString(msg).Round())/2, int(320*s))
	d.DrawString(msg)
	out, err := os.Create(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	if err := png.Encode(out, img); err != nil {
		log.Fatal(err)
	}
}
