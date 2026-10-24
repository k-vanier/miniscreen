package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	_ "golang.org/x/image/webp"

	xdraw "golang.org/x/image/draw"
)

const usage = `miniscreen - pilote pour l'écran WeAct Display FS 0.96" (160x80)

Usage : miniscreen [options] <commande> [arguments]

Commandes :
  info  [-interval 1s]              horloge + CPU / RAM / GPU en continu (Ctrl+C pour quitter)
  image [-cover] [-loop N] <fichier>  affiche une image (png, jpg, gif animé, bmp, webp)
  text  [-color hex] [-bg hex] <texte>  affiche du texte, taille auto (\n = retour à la ligne)
  clear [-color hex]                remplit l'écran d'une couleur (noir par défaut)
  brightness <0-100>                règle la luminosité
  version                           version du firmware de l'écran
  ports                             liste les ports série
  debug                             envoie des carrés de taille croissante (diagnostic)

Options :
`

func main() {
	port := flag.String("port", "", "port série (ex. /dev/cuaU0) ; détecté automatiquement si vide")
	portrait := flag.Bool("portrait", false, "orientation portrait (80x160) au lieu de paysage")
	flip := flag.Bool("flip", false, "retourne l'affichage de 180°")
	bright := flag.Int("brightness", -1, "luminosité 0-100 à appliquer avant la commande")
	chunk := flag.Int("chunk", 0, "octets de pixels par écriture (0 = 4 lignes)")
	chunkDelay := flag.Duration("chunkdelay", 0, "pause après chaque écriture de pixels (ex. 5ms)")
	noDrain := flag.Bool("nodrain", false, "n'attend pas la fin de chaque écriture")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	cmd, args := flag.Arg(0), flag.Args()[1:]

	if cmd == "ports" {
		check(listPorts())
		return
	}

	lcd, err := OpenLCD(*port)
	check(err)
	defer lcd.Close()
	lcd.Chunk, lcd.ChunkDelay, lcd.NoDrain = *chunk, *chunkDelay, *noDrain

	var o byte = Landscape
	if *portrait {
		o = Portrait
	}
	if *flip {
		o++ // Portrait->ReversePortrait, Landscape->ReverseLandscape
	}
	check(lcd.SetOrientation(o))
	if *bright >= 0 {
		check(lcd.SetBrightness(*bright))
	}

	switch cmd {
	case "info":
		fs := flag.NewFlagSet("info", flag.ExitOnError)
		interval := fs.Duration("interval", time.Second, "intervalle de rafraîchissement")
		fs.Parse(args)
		check(runInfo(lcd, *interval))
	case "image":
		fs := flag.NewFlagSet("image", flag.ExitOnError)
		cover := fs.Bool("cover", false, "remplit tout l'écran en rognant (au lieu d'ajouter des bandes noires)")
		loops := fs.Int("loop", 0, "nombre de boucles pour un GIF animé (0 = infini)")
		fs.Parse(args)
		if fs.NArg() != 1 {
			fail("image : il faut un fichier")
		}
		check(showImage(lcd, fs.Arg(0), *cover, *loops))
	case "text":
		fs := flag.NewFlagSet("text", flag.ExitOnError)
		fg := fs.String("color", "ffffff", "couleur du texte (hex RRGGBB)")
		bg := fs.String("bg", "000000", "couleur de fond (hex RRGGBB)")
		fs.Parse(args)
		txt := strings.ReplaceAll(strings.Join(fs.Args(), " "), `\n`, "\n")
		check(showText(lcd, txt, parseHex(*fg), parseHex(*bg)))
	case "clear":
		fs := flag.NewFlagSet("clear", flag.ExitOnError)
		c := fs.String("color", "000000", "couleur (hex RRGGBB)")
		fs.Parse(args)
		col := parseHex(*c)
		check(lcd.Fill(col.R, col.G, col.B))
	case "brightness":
		if len(args) != 1 {
			fail("brightness : il faut une valeur 0-100")
		}
		v, err := strconv.Atoi(args[0])
		check(err)
		check(lcd.SetBrightness(v))
	case "debug":
		check(runDebug(lcd))
	case "version":
		v, err := lcd.Version()
		check(err)
		fmt.Println(v)
	default:
		fail("commande inconnue : " + cmd)
	}
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "erreur :", msg)
	os.Exit(1)
}

func listPorts() error {
	ports := ListPorts()
	if len(ports) == 0 {
		fmt.Println("aucun port série USB (ucom) trouvé")
	}
	for _, p := range ports {
		tag := ""
		if isScreen(p) {
			tag = "  <- écran 0.96\""
		}
		fmt.Printf("%-12s %-9s VID=%s PID=%s SN=%s%s\n", p.Name, p.Driver, p.VID, p.PID, p.Serial, tag)
	}
	return nil
}

func parseHex(s string) color.RGBA {
	s = strings.TrimPrefix(s, "#")
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil || len(s) != 6 {
		fail("couleur invalide : " + s)
	}
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
}

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
	check(err)
	f, err := opentype.NewFace(ft, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	check(err)
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

func newCanvas(lcd *LCD, bg color.Color) *image.RGBA {
	w, h := lcd.Size()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	return img
}

// ---------- image ----------

// fit redimensionne src pour tenir dans w x h (ou le couvrir si cover).
func fit(dst *image.RGBA, src image.Image) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	sb := src.Bounds()
	sx, sy := float64(w)/float64(sb.Dx()), float64(h)/float64(sb.Dy())
	s := min(sx, sy)
	if coverMode {
		s = max(sx, sy)
	}
	nw, nh := int(float64(sb.Dx())*s+0.5), int(float64(sb.Dy())*s+0.5)
	r := image.Rect((w-nw)/2, (h-nh)/2, (w-nw)/2+nw, (h-nh)/2+nh)
	xdraw.CatmullRom.Scale(dst, r, src, sb, xdraw.Over, nil)
}

var coverMode bool

func showImage(lcd *LCD, path string, cover bool, loops int) error {
	coverMode = cover
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if strings.EqualFold(filepath.Ext(path), ".gif") {
		g, err := gif.DecodeAll(f)
		if err != nil {
			return err
		}
		if len(g.Image) > 1 {
			return playGIF(lcd, g, loops)
		}
		f.Seek(0, 0)
	}
	src, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	canvas := newCanvas(lcd, color.Black)
	fit(canvas, src)
	return lcd.Draw(canvas, 0, 0)
}

func playGIF(lcd *LCD, g *gif.GIF, loops int) error {
	// Pré-calcule les images composées et redimensionnées.
	full := image.NewRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
	frames := make([]*image.RGBA, len(g.Image))
	for i, fr := range g.Image {
		prev := image.NewRGBA(full.Bounds())
		copy(prev.Pix, full.Pix)
		draw.Draw(full, fr.Bounds(), fr, fr.Bounds().Min, draw.Over)
		frames[i] = newCanvas(lcd, color.Black)
		fit(frames[i], full)
		switch g.Disposal[i] {
		case gif.DisposalBackground:
			draw.Draw(full, fr.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			full = prev
		}
	}
	for n := 0; loops == 0 || n < loops; n++ {
		for i, fr := range frames {
			start := time.Now()
			if err := lcd.Draw(fr, 0, 0); err != nil {
				return err
			}
			delay := time.Duration(max(g.Delay[i], 2)) * 10 * time.Millisecond
			time.Sleep(delay - time.Since(start))
		}
	}
	return nil
}

// ---------- texte ----------

func wrap(f font.Face, txt string, width int) []string {
	var lines []string
	for _, para := range strings.Split(txt, "\n") {
		line := ""
		for _, w := range strings.Fields(para) {
			try := w
			if line != "" {
				try = line + " " + w
			}
			if textWidth(f, try) <= width || line == "" {
				line = try
			} else {
				lines = append(lines, line)
				line = w
			}
		}
		lines = append(lines, line)
	}
	return lines
}

func showText(lcd *LCD, txt string, fg, bg color.RGBA) error {
	canvas := newCanvas(lcd, bg)
	w, h := lcd.Size()
	const pad = 2
	var f font.Face
	var lines []string
	var lh int
	for size := 64.0; size >= 8; size-- {
		f = face(size, true)
		lines = wrap(f, txt, w-2*pad)
		lh = f.Metrics().Height.Ceil()
		widest := 0
		for _, l := range lines {
			widest = max(widest, textWidth(f, l))
		}
		if lh*len(lines) <= h-2*pad && widest <= w-2*pad {
			break
		}
	}
	asc := f.Metrics().Ascent.Ceil()
	y := (h - lh*len(lines)) / 2
	for _, l := range lines {
		drawString(canvas, f, (w-textWidth(f, l))/2, y+asc, l, fg)
		y += lh
	}
	return lcd.Draw(canvas, 0, 0)
}

// ---------- tableau de bord ----------

var (
	jours  = []string{"dim.", "lun.", "mar.", "mer.", "jeu.", "ven.", "sam."}
	cWhite = color.RGBA{255, 255, 255, 255}
	cGrey  = color.RGBA{140, 140, 150, 255}
	cTrack = color.RGBA{40, 40, 48, 255}
	cCyan  = color.RGBA{80, 200, 255, 255}
)

func levelColor(p float64) color.RGBA {
	switch {
	case p < 60:
		return color.RGBA{60, 210, 90, 255}
	case p < 85:
		return color.RGBA{255, 170, 30, 255}
	default:
		return color.RGBA{240, 60, 50, 255}
	}
}

// bar dessine une jauge : sur une ligne en paysage, et en portrait
// libellé + valeur au-dessus d'une barre pleine largeur.
func bar(dst *image.RGBA, f font.Face, y int, label string, pct float64, value string) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	drawString(dst, f, 3, y+10, label, cGrey)
	drawString(dst, f, w-2-textWidth(f, value), y+10, value, cWhite)
	x0, x1, top := 32, w-42, y+2
	if h > w {
		x0, x1, top = 3, w-3, y+13
	}
	draw.Draw(dst, image.Rect(x0, top, x1, top+8), image.NewUniform(cTrack), image.Point{}, draw.Src)
	fill := x0 + int(float64(x1-x0)*max(0, min(100, pct))/100)
	draw.Draw(dst, image.Rect(x0, top, fill, top+8), image.NewUniform(levelColor(pct)), image.Point{}, draw.Src)
}

func runInfo(lcd *LCD, interval time.Duration) error {
	var cpu cpuSampler
	cpu.Sample()
	time.Sleep(200 * time.Millisecond)
	big, small, lbl := face(22, true), face(11, false), face(11, true)
	for {
		now := time.Now()
		c := newCanvas(lcd, color.Black)
		w, h := c.Bounds().Dx(), c.Bounds().Dy()

		date := fmt.Sprintf("%s %s", jours[now.Weekday()], now.Format("02/01"))
		if h > w { // portrait : heure et date centrées l'une sous l'autre
			hm := now.Format("15:04")
			drawString(c, big, (w-textWidth(big, hm))/2, 26, hm, cWhite)
			drawString(c, small, (w-textWidth(small, date))/2, 42, date, cGrey)
		} else {
			drawString(c, big, 3, 22, now.Format("15:04:05"), cWhite)
			drawString(c, small, w-2-textWidth(small, date), 10, date, cGrey)
		}

		load, temp, gpuOK := GPU()
		if gpuOK && h <= w {
			t := fmt.Sprintf("%.0f°C", temp)
			drawString(c, small, w-2-textWidth(small, t), 22, t, cCyan)
		}

		cp := cpu.Sample()
		used, total := Memory()
		y, step := h-50, 16
		if h > w {
			y, step = 54, 34
		}
		bar(c, lbl, y, "CPU", cp, fmt.Sprintf("%.0f%%", cp))
		bar(c, lbl, y+step, "RAM", 100*used/total, fmt.Sprintf("%.1fG", used))
		if gpuOK {
			bar(c, lbl, y+2*step, "GPU", load, fmt.Sprintf("%.0f%%", load))
		}

		if err := lcd.Draw(c, 0, 0); err != nil {
			return err
		}
		time.Sleep(interval - time.Since(now))
	}
}

// ---------- diagnostic ----------

// runDebug affiche des carrés pleins de plus en plus grands en (0,0), chacun
// d'une couleur différente : la dernière couleur visible donne la taille
// de bitmap à partir de laquelle l'envoi échoue.
func runDebug(lcd *LCD) error {
	v, err := lcd.Version()
	fmt.Printf("firmware : %q (err=%v)\n", v, err)
	fmt.Printf("chunk=%d chunkdelay=%v nodrain=%v\n", lcd.Chunk, lcd.ChunkDelay, lcd.NoDrain)
	if err := lcd.Fill(0, 0, 0); err != nil {
		return err
	}
	fmt.Println("écran noir (commande Fill)")
	time.Sleep(time.Second)
	steps := []struct {
		size int
		name string
		c    color.RGBA
	}{
		{2, "blanc", color.RGBA{255, 255, 255, 255}},
		{4, "vert", color.RGBA{0, 255, 0, 255}},
		{8, "bleu", color.RGBA{0, 0, 255, 255}},
		{16, "jaune", color.RGBA{255, 255, 0, 255}},
		{24, "cyan", color.RGBA{0, 255, 255, 255}},
		{32, "magenta", color.RGBA{255, 0, 255, 255}},
		{48, "orange", color.RGBA{255, 128, 0, 255}},
		{64, "rose", color.RGBA{255, 128, 192, 255}},
		{80, "vert clair", color.RGBA{128, 255, 128, 255}},
	}
	for _, s := range steps {
		img := image.NewUniform(s.c)
		sq := image.NewRGBA(image.Rect(0, 0, s.size, s.size))
		draw.Draw(sq, sq.Bounds(), img, image.Point{}, draw.Src)
		start := time.Now()
		err := lcd.Draw(sq, 0, 0)
		fmt.Printf("carré %2dx%-2d %-10s %5d octets  %v  err=%v\n", s.size, s.size, s.name, s.size*s.size*2, time.Since(start).Round(time.Millisecond), err)
		time.Sleep(2 * time.Second)
	}
	return nil
}
