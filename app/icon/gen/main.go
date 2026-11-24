// Command gen turns the Ebur logo (black elephant on white) into the
// app's icon assets. Run from the repo root after changing the logo:
//
//	go run ./app/icon/gen
//
// It writes:
//   - app/icon/ebur-256.png: ivory elephant on an Elephant Grey rounded
//     square, the window and taskbar icon
//   - app/icon/elephant-mask-64.png: the elephant alone as an alpha mask,
//     which the tray combines with a status dot at run time
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

var (
	elephantGrey = color.RGBA{0x2B, 0x2F, 0x33, 0xFF}
	ivory        = color.RGBA{0xF6, 0xF1, 0xE4, 0xFF}
)

func main() {
	f, err := os.Open("ebur_logo_dark.png")
	if err != nil {
		log.Fatal(err)
	}
	src, err := png.Decode(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}
	mask, box := elephantMask(src)
	// Square box around the elephant so it is not stretched.
	side := max(box.Dx(), box.Dy())
	cx, cy := (box.Min.X+box.Max.X)/2, (box.Min.Y+box.Max.Y)/2
	sq := image.Rect(cx-side/2, cy-side/2, cx-side/2+side, cy-side/2+side)

	write("app/icon/elephant-mask-64.png", alphaImage(sample(mask, sq, 64)))
	write("app/icon/ebur-256.png", appIcon(mask, sq, 256))
}

// elephantMask maps dark pixels to 1 and light pixels to 0, with soft
// edges kept from the source's anti-aliasing.
func elephantMask(src image.Image) ([][]float64, image.Rectangle) {
	b := src.Bounds()
	m := make([][]float64, b.Dy())
	box := image.Rectangle{Min: b.Max, Max: b.Min}
	for y := 0; y < b.Dy(); y++ {
		m[y] = make([]float64, b.Dx())
		for x := 0; x < b.Dx(); x++ {
			r, g, bl, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			lum := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)) / 257
			v := (200 - lum) / 145 // 55 or darker = 1, 200 or lighter = 0
			v = math.Max(0, math.Min(1, v)) * float64(a) / 0xFFFF
			m[y][x] = v
			if v > 0.5 {
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return m, box
}

// sample box-filters the mask region r down to n x n.
func sample(m [][]float64, r image.Rectangle, n int) [][]float64 {
	out := make([][]float64, n)
	scale := float64(r.Dx()) / float64(n)
	for ty := 0; ty < n; ty++ {
		out[ty] = make([]float64, n)
		for tx := 0; tx < n; tx++ {
			x0, y0 := r.Min.X+int(float64(tx)*scale), r.Min.Y+int(float64(ty)*scale)
			x1, y1 := r.Min.X+int(float64(tx+1)*scale), r.Min.Y+int(float64(ty+1)*scale)
			var sum float64
			cnt := 0
			for y := y0; y < max(y1, y0+1); y++ {
				for x := x0; x < max(x1, x0+1); x++ {
					if y >= 0 && y < len(m) && x >= 0 && x < len(m[0]) {
						sum += m[y][x]
					}
					cnt++
				}
			}
			out[ty][tx] = sum / float64(cnt)
		}
	}
	return out
}

func alphaImage(a [][]float64) *image.NRGBA {
	n := len(a)
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			img.SetNRGBA(x, y, color.NRGBA{0xFF, 0xFF, 0xFF, uint8(a[y][x] * 255)})
		}
	}
	return img
}

// appIcon draws the elephant at 72% of a rounded square.
func appIcon(m [][]float64, sq image.Rectangle, n int) *image.NRGBA {
	inner := int(float64(n) * 0.72)
	el := sample(m, sq, inner)
	off := (n - inner) / 2
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	radius := float64(n) * 0.22
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			bg := roundedCoverage(float64(x)+0.5, float64(y)+0.5, float64(n), radius)
			if bg == 0 {
				continue
			}
			e := 0.0
			if ix, iy := x-off, y-off; ix >= 0 && iy >= 0 && ix < inner && iy < inner {
				e = el[iy][ix]
			}
			mix := func(a, b uint8) uint8 { return uint8(float64(a)*(1-e) + float64(b)*e) }
			img.SetNRGBA(x, y, color.NRGBA{mix(elephantGrey.R, ivory.R), mix(elephantGrey.G, ivory.G), mix(elephantGrey.B, ivory.B), uint8(bg * 255)})
		}
	}
	return img
}

// roundedCoverage is 1 inside a rounded square of side s, 0 outside, with
// a one-pixel soft edge.
func roundedCoverage(x, y, s, r float64) float64 {
	dx := math.Max(math.Max(r-x, x-(s-r)), 0)
	dy := math.Max(math.Max(r-y, y-(s-r)), 0)
	d := math.Hypot(dx, dy) - r
	return math.Max(0, math.Min(1, 0.5-d))
}

func write(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
	f.Close()
	log.Printf("wrote %s", path)
}
