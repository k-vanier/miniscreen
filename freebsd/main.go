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
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/font"
	_ "golang.org/x/image/webp"

	xdraw "golang.org/x/image/draw"
)

const usage = `miniscreen - pilote pour l'écran WeAct Display FS 0.96" (160x80)

Usage : miniscreen [options] <commande> [arguments]

Commandes :
  info  [-interval 1s] [-rotate 8s] [-page N] [-light]
                                    tableau de bord : jauges CPU / RAM / temp, réseau (Ctrl+C pour quitter)
  image [-cover] [-loop N] <fichier>  affiche une image (png, jpg, gif animé, bmp, webp)
  text  [-color hex] [-bg hex] <texte>  affiche du texte, taille auto (\n = retour à la ligne)
  clear [-color hex]                remplit l'écran d'une couleur (noir par défaut)
  brightness <0-100>                règle la luminosité
  version                           version du firmware de l'écran
  ports                             liste les ports série

Options :
`

func main() {
	port := flag.String("port", "", "port série (ex. /dev/cuaU0) ; détecté automatiquement si vide")
	portrait := flag.Bool("portrait", false, "orientation portrait (80x160) au lieu de paysage")
	flip := flag.Bool("flip", false, "retourne l'affichage de 180°")
	bright := flag.Int("brightness", -1, "luminosité 0-100 à appliquer avant la commande")
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
		rotate := fs.Duration("rotate", 8*time.Second, "durée d'affichage de chaque page")
		page := fs.Int("page", 0, "page fixe : 1 jauges, 2 réseau (0 = rotation)")
		light := fs.Bool("light", false, "thème clair au lieu du thème sombre")
		fs.Parse(args)
		if *light {
			pal = lightPalette
		}
		check(runInfo(lcd, *interval, *rotate, *page-1))
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

func runInfo(lcd *LCD, interval, rotate time.Duration, fixedPage int) error {
	const histLen = 160
	push := func(h []float64, v float64) []float64 {
		h = append(h, v)
		return h[max(0, len(h)-histLen):]
	}
	var cpu cpuSampler
	cpu.Sample()
	host, _ := os.Hostname()
	host, _, _ = strings.Cut(host, ".")
	rx0, tx0, _ := NetBytes()
	last, start := time.Now(), time.Now()
	time.Sleep(200 * time.Millisecond)
	// À l'arrêt (Ctrl+C, service miniscreen stop, extinction), on éteint
	// l'écran plutôt que de laisser une heure figée.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	s := &Stats{Host: host}
	for {
		now := time.Now()
		dt := now.Sub(last).Seconds()
		last = now

		s.Uptime = Uptime()
		s.CPU = cpu.Sample()
		s.CPUTemp, s.TempOK = CPUTemp()
		s.RAMUsed, s.RAMTotal = Memory()
		s.GPULoad, s.GPUTemp, s.GPUOK = GPU()
		var rx, tx uint64
		if rx, tx, s.NetOK = NetBytes(); s.NetOK {
			s.RxRate, s.TxRate = float64(rx-rx0)/dt, float64(tx-tx0)/dt
			rx0, tx0 = rx, tx
		}
		s.RxHist = push(s.RxHist, s.RxRate)
		s.TxHist = push(s.TxHist, s.TxRate)

		page := max(0, fixedPage)
		if fixedPage < 0 && rotate > 0 {
			page = int(now.Sub(start)/rotate) % s.NumPages()
		}
		w, h := lcd.Size()
		if err := lcd.Draw(renderDashboard(w, h, page, s), 0, 0); err != nil {
			return err
		}
		select {
		case <-stop:
			return lcd.Fill(0, 0, 0)
		case <-time.After(interval - time.Since(now)):
		}
	}
}
