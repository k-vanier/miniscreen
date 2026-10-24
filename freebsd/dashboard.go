package main

// Rendu du tableau de bord : trois pages qui tournent (horloge, jauges,
// historiques), en paysage 160x80 comme en portrait 80x160.

import (
	"fmt"
	"image"
	"image/color"
	"time"
)

type Stats struct {
	Now    time.Time
	Host   string
	Uptime time.Duration
	Load   [3]float64

	CPU, CPUTemp      float64
	TempOK            bool
	RAMUsed, RAMTotal float64 // Go
	GPULoad, GPUTemp  float64
	GPUOK             bool

	NetOK          bool
	RxRate, TxRate float64 // octets/s

	CPUHist, RxHist, TxHist []float64 // une valeur par rafraîchissement
}

const numPages = 3

var (
	cBg     = color.RGBA{8, 10, 18, 255}
	cPanel  = color.RGBA{20, 24, 38, 255}
	cTrack  = color.RGBA{36, 41, 60, 255}
	cWhite  = color.RGBA{238, 240, 248, 255}
	cGrey   = color.RGBA{120, 128, 150, 255}
	cDim    = color.RGBA{88, 95, 120, 255}
	cCyan   = color.RGBA{60, 200, 255, 255}
	cViolet = color.RGBA{165, 120, 255, 255}
	cOrange = color.RGBA{255, 150, 50, 255}
	cPink   = color.RGBA{255, 90, 160, 255}
	cGreen  = color.RGBA{80, 220, 120, 255}
	cRed    = color.RGBA{255, 60, 60, 255}

	joursLong = []string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}
	jours     = []string{"dim.", "lun.", "mar.", "mer.", "jeu.", "ven.", "sam."}
	mois      = []string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}
	moisCourt = []string{"janv.", "févr.", "mars", "avr.", "mai", "juin", "juil.", "août", "sept.", "oct.", "nov.", "déc."}
)

// alert fait virer la couleur d'une jauge au rouge au-delà de 80 %.
func alert(c color.RGBA, pct float64) color.RGBA {
	return lerpColor(c, cRed, (pct-80)/15)
}

func renderDashboard(w, h, page int, s *Stats) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fillRect(img, img.Bounds(), cBg)
	switch page % numPages {
	case 0:
		pageClock(img, s)
	case 1:
		pageGauges(img, s)
	case 2:
		pageHistory(img, s)
	}
	return img
}

// ---------- page 1 : horloge ----------

func pageClock(img *image.RGBA, s *Stats) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	t := s.Now
	sec := float64(t.Second()) / 59
	if w > h {
		big, med, small := face(40, true), face(16, true), face(11, false)
		hm, ss := t.Format("15:04"), t.Format(":05")
		x := (w - textWidth(big, hm) - textWidth(med, ss)) / 2
		drawString(img, big, x, 40, hm, cWhite)
		drawString(img, med, x+textWidth(big, hm), 40, ss, cCyan)
		drawCentered(img, small, w/2, 57, fmt.Sprintf("%s %d %s", joursLong[t.Weekday()], t.Day(), mois[t.Month()-1]), cGrey)
		drawCentered(img, face(9, false), w/2, 70, s.Host+"  ·  up "+fmtUptime(s.Uptime), cDim)
		progress(img, image.Rect(8, 74, w-8, 77), sec, cCyan)
		return
	}
	big, small := face(40, true), face(11, false)
	drawCentered(img, big, w/2, 42, t.Format("15"), cWhite)
	drawCentered(img, big, w/2, 80, t.Format("04"), cWhite)
	progress(img, image.Rect(10, 89, w-10, 92), sec, cCyan)
	drawCentered(img, face(14, true), w/2, 110, t.Format(":05"), cCyan)
	drawCentered(img, small, w/2, 127, fmt.Sprintf("%s %d", jours[t.Weekday()], t.Day()), cGrey)
	drawCentered(img, small, w/2, 140, moisCourt[t.Month()-1], cGrey)
	drawCentered(img, face(9, false), w/2, 154, "up "+fmtUptime(s.Uptime), cDim)
}

func progress(img *image.RGBA, r image.Rectangle, frac float64, c color.RGBA) {
	rad := float64(r.Dy()) / 2
	roundRect(img, r, rad, cTrack)
	if fill := int(float64(r.Dx())*frac + 0.5); fill >= r.Dy() {
		roundRect(img, image.Rect(r.Min.X, r.Min.Y, r.Min.X+fill, r.Max.Y), rad, c)
	}
}

// ---------- page 2 : jauges ----------

type gauge struct {
	label, value string
	pct          float64
	c            color.RGBA
}

func (s *Stats) gauges() []gauge {
	g := []gauge{
		{"CPU", fmt.Sprintf("%.0f%%", s.CPU), s.CPU, cCyan},
		{"RAM", fmt.Sprintf("%.0f%%", 100*s.RAMUsed/s.RAMTotal), 100 * s.RAMUsed / s.RAMTotal, cViolet},
	}
	if s.TempOK {
		g = append(g, gauge{"TEMP", fmt.Sprintf("%.0f°", s.CPUTemp), s.CPUTemp, cOrange})
	}
	if s.GPUOK {
		g = append(g, gauge{"GPU", fmt.Sprintf("%.0f%%", s.GPULoad), s.GPULoad, cPink})
	}
	return g
}

func pageGauges(img *image.RGBA, s *Stats) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	gs := s.gauges()
	lbl := face(9, true)
	if w > h {
		hdr := face(10, false)
		drawString(img, hdr, 4, 10, s.Host, cGrey)
		drawRight(img, face(10, true), w-4, 10, s.Now.Format("15:04"), cWhite)
		cell := float64(w) / float64(len(gs))
		d := min(cell-6, 48)
		for i, g := range gs {
			cx := cell*float64(i) + cell/2
			drawRing(img, cx, 16+d/2, d, g)
			drawCentered(img, lbl, int(cx), int(16+d)+10, g.label, cGrey)
		}
		return
	}
	cell := float64(h) / float64(len(gs))
	d := min(cell-14, 52, float64(w)-14)
	for i, g := range gs {
		cy := cell*float64(i) + (cell-10)/2
		drawRing(img, float64(w)/2, cy, d, g)
		drawCentered(img, lbl, w/2, int(cy+d/2)+10, g.label, cGrey)
	}
}

func drawRing(img *image.RGBA, cx, cy, d float64, g gauge) {
	thick := max(4, d/8)
	ring(img, cx, cy, d/2, thick, g.pct, alert(g.c, g.pct), cTrack)
	f := face(max(9, d*0.26), true)
	asc := f.Metrics().CapHeight.Ceil()
	drawCentered(img, f, int(cx+0.5), int(cy)+asc/2, g.value, cWhite)
}

// ---------- page 3 : historiques ----------

func pageHistory(img *image.RGBA, s *Stats) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	var cpuR, netR image.Rectangle
	switch {
	case !s.NetOK:
		cpuR = image.Rect(2, 2, w-2, h-2)
	case w > h:
		cpuR, netR = image.Rect(2, 2, w-2, h/2-1), image.Rect(2, h/2+1, w-2, h-2)
	default:
		cpuR, netR = image.Rect(2, 2, w-2, h/2-2), image.Rect(2, h/2+2, w-2, h-2)
	}

	panel(img, cpuR)
	chart := cpuR.Inset(2)
	chart.Min.Y += 12
	areaChart(img, chart, s.CPUHist, 100, alert(cCyan, s.CPU))
	f, fb := face(9, false), face(10, true)
	drawString(img, f, cpuR.Min.X+4, cpuR.Min.Y+11, "CPU", cGrey)
	drawRight(img, fb, cpuR.Max.X-4, cpuR.Min.Y+11, fmt.Sprintf("%.0f%%", s.CPU), cWhite)
	if w > h || !s.NetOK {
		drawCentered(img, f, (cpuR.Min.X+cpuR.Max.X)/2, cpuR.Min.Y+11, fmt.Sprintf("load %.2f", s.Load[0]), cDim)
	}
	if !s.NetOK {
		return
	}

	panel(img, netR)
	chart = netR.Inset(2)
	chart.Min.Y += 12
	peak := 1024.0 // échelle minimale : 1 Ko/s
	for i := range s.RxHist {
		peak = max(peak, s.RxHist[i], s.TxHist[i])
	}
	areaChart(img, chart, s.RxHist, peak*1.1, cGreen)
	lineChart(img, chart, s.TxHist, peak*1.1, cViolet)
	down, up := "↓"+fmtRate(s.RxRate), "↑"+fmtRate(s.TxRate)
	drawString(img, fb, netR.Min.X+4, netR.Min.Y+11, down, cGreen)
	drawRight(img, fb, netR.Max.X-4, netR.Min.Y+11, up, cViolet)
}

func panel(img *image.RGBA, r image.Rectangle) {
	roundRect(img, r, 4, cPanel)
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
