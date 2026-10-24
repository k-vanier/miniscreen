package main

// Pilote de l'écran WeAct Studio "Display FS 0.96 Inch" (80x160, USB CDC série).
// Protocole repris de turing-smart-screen-python (lcd_comm_weact_b.py) :
// chaque commande se termine par 0x0A, valeurs 16 bits en little-endian,
// pixels en RGB565 little-endian.

import (
	"errors"
	"fmt"
	"image"
	"strings"
	"time"

	"go.bug.st/serial"
	"golang.org/x/sys/unix"
)

const (
	cmdSetOrientation = 0x02
	cmdSetBrightness  = 0x03
	cmdFull           = 0x04
	cmdSetBitmap      = 0x05
	cmdSystemVersion  = 0x42
	cmdRead           = 0x80
	cmdEnd            = 0x0A

	nativeW = 80
	nativeH = 160
)

const (
	Portrait         = 0
	ReversePortrait  = 1
	Landscape        = 2
	ReverseLandscape = 3
)

type LCD struct {
	port        serial.Port
	orientation byte

	// Réglages d'envoi des bitmaps (diagnostic FreeBSD).
	Chunk      int           // octets par écriture de pixels (0 = 4 lignes)
	ChunkDelay time.Duration // pause après chaque écriture de pixels
	NoDrain    bool          // n'attend pas la fin de chaque écriture
}

func isScreen(p PortInfo) bool { return p.VID == "1A86" && p.PID == "FE0C" }

// FindPort cherche l'écran par son VID/PID USB (1A86:FE0C).
func FindPort() (string, error) {
	for _, p := range ListPorts() {
		if isScreen(p) {
			return p.Name, nil
		}
	}
	return "", errors.New("écran WeAct 0.96\" introuvable (VID 1A86 / PID FE0C) ; voir « miniscreen ports » et usbconfig")
}

func OpenLCD(name string) (*LCD, error) {
	if name == "" {
		var err error
		if name, err = FindPort(); err != nil {
			return nil, err
		}
	} else if !strings.HasPrefix(name, "/") {
		name = "/dev/" + name // -port cuaU0
	}
	// DTR et RTS actifs comme sous Windows (go.bug.st/serial ne les force pas
	// sous Unix) : l'écran CDC n'affiche rien sans DTR.
	mode := &serial.Mode{BaudRate: 115200, InitialStatusBits: &serial.ModemOutputBits{DTR: true, RTS: true}}
	// Sous FreeBSD, le TIOCSETA d'umodem attend une requête USB et peut être
	// interrompu par un signal (EINTR) ; go.bug.st/serial ne le relance pas.
	var p serial.Port
	var err error
	for try := 0; try < 10; try++ {
		p, err = serial.Open(name, mode)
		if err == nil || !strings.Contains(err.Error(), unix.EINTR.Error()) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		return nil, fmt.Errorf("ouverture de %s : %w", name, err)
	}
	p.SetReadTimeout(time.Second)
	p.ResetInputBuffer()
	return &LCD{port: p}, nil
}

func (l *LCD) Close() error { return l.port.Close() }

// write envoie tout b puis, sauf NoDrain, attend qu'il soit parti (tcdrain),
// comme une écriture synchrone sous Windows. La bibliothèque ne relance
// ni les écritures partielles ni les appels interrompus par un signal.
func (l *LCD) write(b []byte) error {
	for len(b) > 0 {
		n, err := l.port.Write(b)
		b = b[n:]
		if err == unix.EINTR || err == unix.EAGAIN {
			continue
		}
		if err != nil {
			return err
		}
	}
	for !l.NoDrain {
		err := l.port.Drain()
		if err != unix.EINTR {
			return err
		}
	}
	return nil
}

// Version renvoie la chaîne de version du firmware (réponse de 19 octets).
func (l *LCD) Version() (string, error) {
	l.port.ResetInputBuffer()
	if err := l.write([]byte{cmdSystemVersion | cmdRead, cmdEnd}); err != nil {
		return "", err
	}
	buf := make([]byte, 19)
	n := 0
	for n < len(buf) {
		k, err := l.port.Read(buf[n:])
		if err != nil {
			return "", err
		}
		if k == 0 {
			break // timeout
		}
		n += k
	}
	if n < 9 {
		return "", fmt.Errorf("réponse de version trop courte (%d octets)", n)
	}
	return strings.TrimRight(string(buf[1:9]), "\x00 "), nil
}

func (l *LCD) Size() (int, int) {
	if l.orientation == Landscape || l.orientation == ReverseLandscape {
		return nativeH, nativeW
	}
	return nativeW, nativeH
}

func (l *LCD) SetOrientation(o byte) error {
	l.orientation = o
	return l.write([]byte{cmdSetOrientation, o, cmdEnd})
}

// SetBrightness règle la luminosité (0-100 %), avec une transition de 1 s.
func (l *LCD) SetBrightness(pct int) error {
	pct = max(0, min(100, pct))
	level := byte(pct * 255 / 100)
	const ms = 1000
	return l.write([]byte{cmdSetBrightness, level, ms & 0xFF, ms >> 8, cmdEnd})
}

func rgb565(r, g, b uint32) uint16 {
	return uint16((r>>11)<<11 | (g>>10)<<5 | b>>11)
}

// Fill remplit tout l'écran d'une couleur (composantes 0-255).
func (l *LCD) Fill(r, g, b uint8) error {
	w, h := l.Size()
	c := rgb565(uint32(r)<<8, uint32(g)<<8, uint32(b)<<8)
	return l.write([]byte{
		cmdFull,
		0, 0, 0, 0,
		byte(w - 1), byte((w - 1) >> 8), byte(h - 1), byte((h - 1) >> 8),
		byte(c), byte(c >> 8),
		cmdEnd,
	})
}

// Draw affiche img à la position (x, y), en le rognant aux bords de l'écran.
func (l *LCD) Draw(img image.Image, x, y int) error {
	sw, sh := l.Size()
	b := img.Bounds()
	w, h := min(b.Dx(), sw-x), min(b.Dy(), sh-y)
	if x < 0 || y < 0 || w <= 0 || h <= 0 {
		return fmt.Errorf("image hors écran (%d,%d %dx%d)", x, y, b.Dx(), b.Dy())
	}
	x1, y1 := x+w-1, y+h-1
	hdr := []byte{
		cmdSetBitmap,
		byte(x), byte(x >> 8), byte(y), byte(y >> 8),
		byte(x1), byte(x1 >> 8), byte(y1), byte(y1 >> 8),
		cmdEnd,
	}
	data := make([]byte, 0, w*h*2)
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			r, g, bl, _ := img.At(b.Min.X+i, b.Min.Y+j).RGBA()
			c := rgb565(r, g, bl)
			data = append(data, byte(c), byte(c>>8))
		}
	}
	if err := l.write(hdr); err != nil {
		return err
	}
	chunk := l.Chunk
	if chunk <= 0 {
		chunk = w * 4
	}
	for len(data) > 0 {
		n := min(chunk, len(data))
		if err := l.write(data[:n]); err != nil {
			return err
		}
		data = data[n:]
		time.Sleep(l.ChunkDelay)
	}
	return nil
}
