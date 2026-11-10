// Draws the Brewctl app icon: a beer mug on an amber rounded square.
package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

const N = 1024
const SS = 4 // supersampling

type rgb struct{ r, g, b float64 }

func rrect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx := math.Max(x0+r, math.Min(x, x1-r))
	cy := math.Max(y0+r, math.Min(y, y1-r))
	return math.Hypot(x-cx, y-cy) <= r
}

// squircle-ish: superellipse matching the macOS icon shape reasonably well.
func squircle(x, y, c, half float64) bool {
	dx, dy := math.Abs(x-c)/half, math.Abs(y-c)/half
	return math.Pow(dx, 5)+math.Pow(dy, 5) <= 1
}

func circle(x, y, cx, cy, r float64) bool { return math.Hypot(x-cx, y-cy) <= r }

func sample(x, y float64) (rgb, float64) {
	if !squircle(x, y, 512, 412) {
		return rgb{}, 0
	}
	t := (y - 100) / 824
	bg := rgb{249 + (222-249)*t, 196 + (134-196)*t, 99 + (20-99)*t}
	cream := rgb{255, 250, 240}
	ink := rgb{96, 52, 4}

	foam := circle(x, y, 372, 372, 62) || circle(x, y, 462, 346, 78) || circle(x, y, 560, 362, 70) ||
		circle(x, y, 632, 392, 48) || rrect(x, y, 312, 372, 680, 452, 36)
	body := rrect(x, y, 336, 400, 656, 768, 44)
	handleOuter := rrect(x, y, 600, 468, 780, 688, 84)
	handleInner := rrect(x, y, 600, 524, 724, 632, 40)
	stripe := rrect(x, y, 408, 520, 452, 708, 22) || rrect(x, y, 540, 520, 584, 708, 22)

	// soft drop shadow under the mug
	if !(foam || body || (handleOuter && !handleInner)) {
		if rrect(x, y-14, 336, 400, 656, 768, 44) || (rrect(x, y-14, 600, 468, 780, 688, 84) && !rrect(x, y-14, 600, 524, 724, 632, 40)) {
			return rgb{bg.r * 0.86, bg.g * 0.82, bg.b * 0.7}, 1
		}
		return bg, 1
	}
	if foam {
		return rgb{255, 255, 255}, 1
	}
	if body {
		if stripe {
			return rgb{236, 158, 44}, 1
		}
		return cream, 1
	}
	_ = ink
	return cream, 1
}

func main() {
	img := image.NewNRGBA(image.Rect(0, 0, N, N))
	for py := 0; py < N; py++ {
		for px := 0; px < N; px++ {
			var r, g, b, a float64
			for sy := 0; sy < SS; sy++ {
				for sx := 0; sx < SS; sx++ {
					c, al := sample(float64(px)+(float64(sx)+0.5)/SS, float64(py)+(float64(sy)+0.5)/SS)
					r += c.r * al
					g += c.g * al
					b += c.b * al
					a += al
				}
			}
			if a > 0 {
				img.SetNRGBA(px, py, color.NRGBA{uint8(r/a + 0.5), uint8(g/a + 0.5), uint8(b/a + 0.5), uint8(a/(SS*SS)*255 + 0.5)})
			}
		}
	}
	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}
