package main

// Rendu du tableau de bord : deux pages qui tournent (jauges CPU / RAM /
// température, puis réseau), en paysage 160x80 comme en portrait 80x160.

import (
	"fmt"
	"image"
	"image/color"
	"time"
)

type Stats struct {
	Host   string
	Uptime time.Duration

	CPU, CPUTemp      float64
	TempOK            bool
	RAMUsed, RAMTotal float64 // Go
	GPULoad, GPUTemp  float64
	GPUOK             bool

	NetOK          bool
	RxRate, TxRate float64 // octets/s

	RxHist, TxHist []float64 // une valeur par rafraîchissement
}

// NumPages renvoie le nombre de pages : la page réseau n'existe que si
// le débit a pu être lu.
func (s *Stats) NumPages() int {
	if s.NetOK {
		return 2
	}
	return 1
}

type palette struct {
	bg, panel, track, text, grey, dim      color.RGBA
	cyan, violet, orange, pink, green, red color.RGBA
}

// Thème clair par défaut ; accents plus foncés pour rester lisibles sur blanc.
var lightPalette = palette{
	bg:     color.RGBA{246, 248, 251, 255},
	panel:  color.RGBA{232, 236, 243, 255},
	track:  color.RGBA{220, 225, 235, 255},
	text:   color.RGBA{24, 28, 40, 255},
	grey:   color.RGBA{95, 103, 125, 255},
	dim:    color.RGBA{130, 137, 158, 255},
	cyan:   color.RGBA{0, 145, 215, 255},
	violet: color.RGBA{120, 75, 225, 255},
	orange: color.RGBA{235, 115, 15, 255},
	pink:   color.RGBA{220, 45, 125, 255},
	green:  color.RGBA{20, 160, 75, 255},
	red:    color.RGBA{215, 35, 35, 255},
}

var darkPalette = palette{
	bg:     color.RGBA{8, 10, 18, 255},
	panel:  color.RGBA{20, 24, 38, 255},
	track:  color.RGBA{36, 41, 60, 255},
	text:   color.RGBA{238, 240, 248, 255},
	grey:   color.RGBA{120, 128, 150, 255},
	dim:    color.RGBA{88, 95, 120, 255},
	cyan:   color.RGBA{60, 200, 255, 255},
	violet: color.RGBA{165, 120, 255, 255},
	orange: color.RGBA{255, 150, 50, 255},
	pink:   color.RGBA{255, 90, 160, 255},
	green:  color.RGBA{80, 220, 120, 255},
	red:    color.RGBA{255, 60, 60, 255},
}

var pal = lightPalette

// alert fait virer la couleur d'une jauge au rouge au-delà de 80 %.
func alert(c color.RGBA, pct float64) color.RGBA {
	return lerpColor(c, pal.red, (pct-80)/15)
}

func renderDashboard(w, h, page int, s *Stats) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fillRect(img, img.Bounds(), pal.bg)
	if page%s.NumPages() == 0 {
		pageGauges(img, s)
	} else {
		pageNetwork(img, s)
	}
	return img
}

// ---------- page 1 : jauges ----------

type gauge struct {
	label, value string
	pct          float64
	c            color.RGBA
}

func (s *Stats) gauges() []gauge {
	g := []gauge{
		{"CPU", fmt.Sprintf("%.0f%%", s.CPU), s.CPU, pal.cyan},
		{"RAM", fmt.Sprintf("%.0f%%", 100*s.RAMUsed/s.RAMTotal), 100 * s.RAMUsed / s.RAMTotal, pal.violet},
	}
	if s.TempOK {
		g = append(g, gauge{"TEMP", fmt.Sprintf("%.0f°", s.CPUTemp), s.CPUTemp, pal.orange})
	}
	if s.GPUOK {
		g = append(g, gauge{"GPU", fmt.Sprintf("%.0f%%", s.GPULoad), s.GPULoad, pal.pink})
	}
	return g
}

func pageGauges(img *image.RGBA, s *Stats) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	gs := s.gauges()
	lbl := face(9, true)
	if w > h {
		hdr := face(10, false)
		drawString(img, hdr, 4, 10, s.Host, pal.grey)
		drawRight(img, hdr, w-4, 10, "up "+fmtUptime(s.Uptime), pal.dim)
		cell := float64(w) / float64(len(gs))
		d := min(cell-6, 48)
		for i, g := range gs {
			cx := cell*float64(i) + cell/2
			drawRing(img, cx, 16+d/2, d, g)
			drawCentered(img, lbl, int(cx), int(16+d)+10, g.label, pal.grey)
		}
		return
	}
	cell := float64(h) / float64(len(gs))
	d := min(cell-14, 52, float64(w)-14)
	for i, g := range gs {
		cy := cell*float64(i) + (cell-10)/2
		drawRing(img, float64(w)/2, cy, d, g)
		drawCentered(img, lbl, w/2, int(cy+d/2)+10, g.label, pal.grey)
	}
}

func drawRing(img *image.RGBA, cx, cy, d float64, g gauge) {
	thick := max(4, d/8)
	ring(img, cx, cy, d/2, thick, g.pct, alert(g.c, g.pct), pal.track)
	f := face(max(9, d*0.26), true)
	asc := f.Metrics().CapHeight.Ceil()
	drawCentered(img, f, int(cx+0.5), int(cy)+asc/2, g.value, pal.text)
}

// ---------- page 2 : réseau ----------

func pageNetwork(img *image.RGBA, s *Stats) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	down, up := "↓ "+fmtRate(s.RxRate)+"/s", "↑ "+fmtRate(s.TxRate)+"/s"
	fb := face(12, true)
	var r image.Rectangle
	if w > h {
		drawString(img, fb, 4, 13, down, pal.green)
		drawRight(img, fb, w-4, 13, up, pal.violet)
		r = image.Rect(2, 18, w-2, h-2)
	} else {
		drawCentered(img, fb, w/2, 16, down, pal.green)
		drawCentered(img, fb, w/2, 33, up, pal.violet)
		r = image.Rect(2, 40, w-2, h-2)
	}
	panel(img, r)
	chart := r.Inset(3)
	peak := 1024.0 // échelle minimale : 1 Ko/s
	for i := range s.RxHist {
		peak = max(peak, s.RxHist[i], s.TxHist[i])
	}
	areaChart(img, chart, s.RxHist, peak*1.1, pal.green)
	lineChart(img, chart, s.TxHist, peak*1.1, pal.violet)
}

func panel(img *image.RGBA, r image.Rectangle) {
	roundRect(img, r, 4, pal.panel)
}

// ---------- formats ----------

func fmtUptime(d time.Duration) string {
	m := int(d.Minutes())
	switch {
	case m >= 24*60:
		return fmt.Sprintf("%dj %dh", m/(24*60), m/60%24)
	case m >= 60:
		return fmt.Sprintf("%dh %02dm", m/60, m%60)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

func fmtRate(b float64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fG", b/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fM", b/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0fK", b/(1<<10))
	default:
		return fmt.Sprintf("%.0fB", b)
	}
}
