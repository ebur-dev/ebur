package main

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/png"
	"math"
)

// Icon assets are generated from the Ebur logo by app/icon/gen.
var (
	//go:embed icon/ebur-256.png
	appIconPNG []byte
	//go:embed icon/elephant-mask-64.png
	elephantMaskPNG []byte
)

// Brand palette (SRS §4.5).
var (
	elephantGrey = color.RGBA{0x2B, 0x2F, 0x33, 0xFF}
	ivory        = color.RGBA{0xF6, 0xF1, 0xE4, 0xFF}
	statusColour = map[string]color.RGBA{
		"all":  {0x3E, 0x9B, 0x5F, 0xFF}, // Grass Green: everything running
		"some": {0xD8, 0x9B, 0x2B, 0xFF}, // Savanna Gold: some running
		"none": {0x8A, 0x8F, 0x94, 0xFF}, // grey: stopped
	}
)

// statusIcon renders the 32x32 tray icon: the ivory elephant on an
// Elephant Grey rounded square, with a status dot (GUI-01).
func statusIcon(summary string) []byte {
	const n = 32
	dot, ok := statusColour[summary]
	if !ok {
		dot = statusColour["none"]
	}
	elephant := maskAt(24)
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			bg := rounded(fx, fy, n, 7)
			if bg == 0 {
				continue
			}
			c := elephantGrey
			if ex, ey := x-3, y-2; ex >= 0 && ey >= 0 && ex < 24 && ey < 24 {
				c = mix(c, ivory, elephant[ey][ex])
			}
			// Status dot with an ivory ring, bottom right.
			d := math.Hypot(fx-25, fy-25)
			c = mix(c, ivory, edge(d, 7.5))
			c = mix(c, dot, edge(d, 6))
			img.SetRGBA(x, y, premultiply(c, bg))
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// maskAt box-filters the embedded 64px elephant mask down to n x n.
func maskAt(n int) [][]float64 {
	src, err := png.Decode(bytes.NewReader(elephantMaskPNG))
	out := make([][]float64, n)
	for i := range out {
		out[i] = make([]float64, n)
	}
	if err != nil {
		return out
	}
	sb := src.Bounds()
	scale := float64(sb.Dx()) / float64(n)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			var sum, cnt float64
			for sy := int(float64(y) * scale); sy < int(float64(y+1)*scale); sy++ {
				for sx := int(float64(x) * scale); sx < int(float64(x+1)*scale); sx++ {
					_, _, _, a := src.At(sb.Min.X+sx, sb.Min.Y+sy).RGBA()
					sum += float64(a) / 0xFFFF
					cnt++
				}
			}
			if cnt > 0 {
				out[y][x] = sum / cnt
			}
		}
	}
	return out
}

// edge is 1 inside radius r, 0 outside, with a one-pixel soft edge.
func edge(d, r float64) float64 { return math.Max(0, math.Min(1, r+0.5-d)) }

// rounded is the coverage of a rounded square of side s and radius r.
func rounded(x, y, s, r float64) float64 {
	dx := math.Max(math.Max(r-x, x-(s-r)), 0)
	dy := math.Max(math.Max(r-y, y-(s-r)), 0)
	return math.Max(0, math.Min(1, 0.5-(math.Hypot(dx, dy)-r)))
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	m := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
	return color.RGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), 0xFF}
}

// premultiply applies coverage as alpha (image.RGBA is premultiplied).
func premultiply(c color.RGBA, a float64) color.RGBA {
	return color.RGBA{uint8(float64(c.R) * a), uint8(float64(c.G) * a), uint8(float64(c.B) * a), uint8(255 * a)}
}
