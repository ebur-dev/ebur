package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// Tray icons are drawn at startup in the brand palette (SRS §4.5), so no
// image files ship: an Elephant Grey disc with a status dot (GUI-01).
var (
	elephantGrey = color.RGBA{0x2B, 0x2F, 0x33, 0xFF}
	ivory        = color.RGBA{0xF6, 0xF1, 0xE4, 0xFF}
	statusColour = map[string]color.RGBA{
		"all":  {0x3E, 0x9B, 0x5F, 0xFF}, // Grass Green: everything running
		"some": {0xD8, 0x9B, 0x2B, 0xFF}, // Savanna Gold: some running
		"none": {0x8A, 0x8F, 0x94, 0xFF}, // grey: stopped
	}
)

// statusIcon renders a 32x32 PNG with anti-aliased edges.
func statusIcon(summary string) []byte {
	const size = 32
	dot, ok := statusColour[summary]
	if !ok {
		dot = statusColour["none"]
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	c := float64(size-1) / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			d := math.Hypot(float64(x)-c, float64(y)-c)
			switch {
			case d <= 7.5:
				img.Set(x, y, blend(dot, ivory, d-6.5))
			case d <= 9.5:
				img.Set(x, y, blend(ivory, elephantGrey, d-8.5))
			case d <= 15.5:
				img.Set(x, y, fade(elephantGrey, d-14.5))
			}
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// blend mixes a into b over one pixel at an edge (t from 0 to 1).
func blend(a, b color.RGBA, t float64) color.RGBA {
	t = math.Max(0, math.Min(1, t))
	mix := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
	return color.RGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), 0xFF}
}

// fade makes the outer edge transparent.
func fade(c color.RGBA, t float64) color.RGBA {
	t = math.Max(0, math.Min(1, t))
	a := 1 - t
	return color.RGBA{uint8(float64(c.R) * a), uint8(float64(c.G) * a), uint8(float64(c.B) * a), uint8(255 * a)}
}
