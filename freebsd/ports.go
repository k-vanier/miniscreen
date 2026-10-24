package main

// Énumération des ports série USB sous FreeBSD. L'énumérateur de go.bug.st/serial
// n'est pas implémenté pour FreeBSD : on lit les sysctl des pilotes ucom
// (dev.<pilote>.<n>.%pnpinfo et dev.<pilote>.<n>.ttyname).

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/sys/unix"
)

type PortInfo struct {
	Name     string // ex. /dev/cuaU0
	Driver   string // ex. umodem0
	VID, PID string // ex. 1A86, FE0C
	Serial   string
}

// Pilotes FreeBSD basés sur ucom(4) ; l'écran (CDC ACM) est pris par umodem.
var ucomDrivers = []string{"umodem", "uftdi", "uplcom", "uslcom", "uchcom", "u3g", "ubsa", "umct", "uark", "ucycom"}

var (
	reVendor  = regexp.MustCompile(`vendor=0x([0-9a-fA-F]{4})`)
	reProduct = regexp.MustCompile(`product=0x([0-9a-fA-F]{4})`)
	reSernum  = regexp.MustCompile(`sernum="([^"]*)"`)
)

func ListPorts() []PortInfo {
	var ports []PortInfo
	for _, drv := range ucomDrivers {
		for n := 0; n < 32; n++ { // les numéros d'unité peuvent avoir des trous
			base := fmt.Sprintf("dev.%s.%d.", drv, n)
			tty, err := unix.Sysctl(base + "ttyname")
			if err != nil || tty == "" {
				continue
			}
			pnp, _ := unix.Sysctl(base + "%pnpinfo")
			p := PortInfo{Name: "/dev/cua" + tty, Driver: fmt.Sprint(drv, n)}
			if m := reVendor.FindStringSubmatch(pnp); m != nil {
				p.VID = strings.ToUpper(m[1])
			}
			if m := reProduct.FindStringSubmatch(pnp); m != nil {
				p.PID = strings.ToUpper(m[1])
			}
			if m := reSernum.FindStringSubmatch(pnp); m != nil {
				p.Serial = m[1]
			}
			ports = append(ports, p)
		}
	}
	return ports
}
