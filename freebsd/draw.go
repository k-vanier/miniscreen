package main

// Polices et primitives de dessin anticrénelées, sans dépendance au système
// ni à l'écran (le tableau de bord peut ainsi être prévisualisé en PNG).

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// ---------- polices ----------

var fontCache = map[string]font.Face{}

func face(size float64, bold bool) font.Face {
	key := fmt.Sprint(size, bold)
	if f, ok := fontCache[key]; ok {
		return f
	}
	data := goregular.TTF
	if bold {
		data = gobold.TTF
	}
	ft, err := opentype.Parse(data)
	if err != nil {
		panic(err)
	}
	f, err := opentype.NewFace(ft, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err)
	}
	fontCache[key] = f
	return f
}

func textWidth(f font.Face, s string) int {
	return font.MeasureString(f, s).Ceil()
}

func drawString(dst draw.Image, f font.Face, x, baseline int, s string, c color.Color) {
	d := font.Drawer{Dst: dst, Src: image.NewUniform(c), Face: f, Dot: fixed.P(x, baseline)}
	d.DrawString(s)
}

func drawCentered(dst draw.Image, f font.Face, cx, baseline int, s string, c color.Color) {
	drawString(dst, f, cx-textWidth(f, s)/2, baseline, s, c)
}

func drawRight(dst draw.Image, f font.Face, right, baseline int, s string, c color.Color) {
	drawString(dst, f, right-textWidth(f, s), baseline, s, c)
}

// ---------- couleurs ----------

func lerpColor(a, b color.RGBA, t float64) color.RGBA {
	t = max(0, min(1, t))
	l := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return color.RGBA{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B), 255}
}

// blend mélange c sur le pixel (x, y) avec une opacité a (0-1).
func blend(dst *image.RGBA, x, y int, c color.RGBA, a float64) {
	if a <= 0 || !(image.Point{x, y}.In(dst.Rect)) {
		return
	}
	i := dst.PixOffset(x, y)
	p := dst.Pix[i : i+3 : i+3]
	a = min(a, 1)
	p[0] = uint8(float64(p[0])*(1-a) + float64(c.R)*a + 0.5)
	p[1] = uint8(float64(p[1])*(1-a) + float64(c.G)*a + 0.5)
	p[2] = uint8(float64(p[2])*(1-a) + float64(c.B)*a + 0.5)
}

func fillRect(dst *image.RGBA, r image.Rectangle, c color.Color) {
	draw.Draw(dst, r, image.NewUniform(c), image.Point{}, draw.Src)
}

// ---------- formes ----------

const ss = 4 // sur-échantillonnage par axe pour l'anticrénelage

// coverage remplit le rectangle r en appelant inside pour ss×ss points par
// pixel, et applique la couleur au prorata des points à l'intérieur.
func coverage(dst *image.RGBA, r image.Rectangle, c color.RGBA, inside func(x, y float64) bool) {
	r = r.Intersect(dst.Rect)
	for py := r.Min.Y; py < r.Max.Y; py++ {
		for px := r.Min.X; px < r.Max.X; px++ {
			n := 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					if inside(float64(px)+(float64(sx)+0.5)/ss, float64(py)+(float64(sy)+0.5)/ss) {
						n++
					}
				}
			}
			blend(dst, px, py, c, float64(n)/(ss*ss))
		}
	}
}

// roundRect remplit un rectangle aux coins arrondis de rayon rad.
func roundRect(dst *image.RGBA, r image.Rectangle, rad float64, c color.RGBA) {
	x0, y0, x1, y1 := float64(r.Min.X), float64(r.Min.Y), float64(r.Max.X), float64(r.Max.Y)
	rad = min(rad, (x1-x0)/2, (y1-y0)/2)
	coverage(dst, r, c, func(x, y float64) bool {
		cx := max(x0+rad, min(x1-rad, x))
		cy := max(y0+rad, min(y1-rad, y))
		return (x-cx)*(x-cx)+(y-cy)*(y-cy) <= rad*rad
	})
}

// ring dessine une jauge circulaire : piste complète, puis arc de pct %
// depuis midi dans le sens horaire, avec des extrémités arrondies.
func ring(dst *image.RGBA, cx, cy, r, thick, pct float64, fg, track color.RGBA) {
	box := image.Rect(int(cx-r)-1, int(cy-r)-1, int(cx+r)+2, int(cy+r)+2)
	rIn, rMid := r-thick, r-thick/2
	inAnnulus := func(x, y float64) bool {
		d := math.Hypot(x-cx, y-cy)
		return d >= rIn && d <= r
	}
	coverage(dst, box, track, inAnnulus)
	pct = max(0, min(100, pct))
	if pct == 0 {
		return
	}
	end := pct / 100 * 2 * math.Pi
	ex, ey := cx+rMid*math.Sin(end), cy-rMid*math.Cos(end)
	cap2 := thick * thick / 4
	coverage(dst, box, fg, func(x, y float64) bool {
		dx, dy := x-cx, y-cy
		if inAnnulus(x, y) {
			a := math.Atan2(dx, -dy)
			if a < 0 {
				a += 2 * math.Pi
			}
			if a <= end {
				return true
			}
		}
		return dx*dx+(dy+rMid)*(dy+rMid) <= cap2 || (x-ex)*(x-ex)+(y-ey)*(y-ey) <= cap2
	})
}

// areaChart trace l'historique vals (le plus récent à droite) dans r :
// aire en dégradé vertical surmontée d'une ligne pleine. maxV fixe l'échelle.
func areaChart(dst *image.RGBA, r image.Rectangle, vals []float64, maxV float64, c color.RGBA) {
	if maxV <= 0 {
		return
	}
	w, h := r.Dx(), r.Dy()
	vals = vals[max(0, len(vals)-w):]
	off := w - len(vals)
	prev := -1
	for i, v := range vals {
		x := r.Min.X + off + i
		top := r.Max.Y - 1 - int(max(0, min(1, v/maxV))*float64(h-1)+0.5)
		for y := top + 1; y < r.Max.Y; y++ {
			blend(dst, x, y, c, 0.12+0.38*float64(r.Max.Y-y)/float64(h))
		}
		// Ligne : relie verticalement au point précédent pour rester continue.
		y0, y1 := top, top
		if prev >= 0 {
			y0, y1 = min(top, prev), max(top, prev)
		}
		for y := y0; y <= y1; y++ {
			blend(dst, x, y, c, 1)
		}
		prev = top
	}
}

// lineChart trace seulement la ligne de vals dans r (pour superposer deux séries).
func lineChart(dst *image.RGBA, r image.Rectangle, vals []float64, maxV float64, c color.RGBA) {
	if maxV <= 0 {
		return
	}
	w, h := r.Dx(), r.Dy()
	vals = vals[max(0, len(vals)-w):]
	off := w - len(vals)
	prev := -1
	for i, v := range vals {
		x := r.Min.X + off + i
		top := r.Max.Y - 1 - int(max(0, min(1, v/maxV))*float64(h-1)+0.5)
		y0, y1 := top, top
		if prev >= 0 {
			y0, y1 = min(top, prev), max(top, prev)
		}
		for y := y0; y <= y1; y++ {
			blend(dst, x, y, c, 1)
		}
		prev = top
	}
}
