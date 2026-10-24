package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

type cpuSampler struct{ idle, total uint64 }

// Sample renvoie l'utilisation CPU (0-100) depuis l'appel précédent.
// kern.cp_time = 5 compteurs C long : user, nice, sys, intr, idle.
func (c *cpuSampler) Sample() float64 {
	raw, err := unix.SysctlRaw("kern.cp_time")
	sz := int(unsafe.Sizeof(uintptr(0))) // taille d'un long C
	if err != nil || len(raw) < 5*sz {
		return 0
	}
	var t, i uint64
	for k := 0; k < 5; k++ {
		var v uint64
		if sz == 8 {
			v = binary.NativeEndian.Uint64(raw[k*8:])
		} else {
			v = uint64(binary.NativeEndian.Uint32(raw[k*4:]))
		}
		t += v
		if k == 4 {
			i = v
		}
	}
	di, dt := i-c.idle, t-c.total
	c.idle, c.total = i, t
	if dt == 0 {
		return 0
	}
	return 100 * (1 - float64(di)/float64(dt))
}

// Memory renvoie la RAM utilisée et totale, en Go.
// Disponible = pages libres + inactives + laundry (comme top) ; l'ARC ZFS compte comme utilisé.
func Memory() (used, total float64) {
	phys, err := unix.SysctlUint64("hw.physmem")
	if err != nil {
		return 0, 1
	}
	page, _ := unix.SysctlUint32("hw.pagesize")
	var avail uint64
	for _, s := range []string{"v_free_count", "v_inactive_count", "v_laundry_count"} {
		n, _ := unix.SysctlUint32("vm.stats.vm." + s)
		avail += uint64(n)
	}
	avail *= uint64(page)
	const gb = 1 << 30
	return float64(phys-min(avail, phys)) / gb, float64(phys) / gb
}

// GPU interroge nvidia-smi (paquet nvidia-driver) ; ok=false si absent.
func GPU() (load, temp float64, ok bool) {
	out, err := exec.Command("nvidia-smi", "--query-gpu=utilization.gpu,temperature.gpu", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return 0, 0, false
	}
	f := strings.Split(strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]), ",")
	if len(f) < 2 {
		return 0, 0, false
	}
	load, err1 := strconv.ParseFloat(strings.TrimSpace(f[0]), 64)
	temp, err2 := strconv.ParseFloat(strings.TrimSpace(f[1]), 64)
	return load, temp, err1 == nil && err2 == nil
}

// CPUTemp renvoie la température CPU la plus haute (°C), lue dans
// dev.cpu.N.temperature (modules coretemp ou amdtemp) ; ok=false si absente.
func CPUTemp() (temp float64, ok bool) {
	for n := 0; n < runtime.NumCPU(); n++ {
		dk, err := unix.SysctlUint32(fmt.Sprintf("dev.cpu.%d.temperature", n)) // dixièmes de kelvin
		if err != nil {
			break
		}
		temp, ok = max(temp, (float64(int32(dk))-2731)/10), true
	}
	return temp, ok
}

// LoadAvg renvoie la charge moyenne sur 1, 5 et 15 minutes (vm.loadavg,
// trois fixpt_t en virgule fixe sur FSHIFT = 11 bits).
func LoadAvg() (l [3]float64) {
	raw, err := unix.SysctlRaw("vm.loadavg")
	if err != nil || len(raw) < 12 {
		return l
	}
	for i := range l {
		l[i] = float64(binary.NativeEndian.Uint32(raw[i*4:])) / 2048
	}
	return l
}

// Uptime renvoie le temps écoulé depuis le démarrage (kern.boottime).
func Uptime() time.Duration {
	tv, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return 0
	}
	return time.Since(time.Unix(int64(tv.Sec), int64(tv.Usec)*1000))
}

// NetBytes renvoie le total d'octets reçus et envoyés par les interfaces
// (hors loopback), d'après netstat -ibn --libxo json ; ok=false en cas d'échec.
func NetBytes() (rx, tx uint64, ok bool) {
	out, err := exec.Command("netstat", "-ibn", "--libxo", "json").Output()
	if err != nil {
		return 0, 0, false
	}
	var v struct {
		Statistics struct {
			Interface []struct {
				Name    string `json:"name"`
				Network string `json:"network"`
				Rx      uint64 `json:"received-bytes"`
				Tx      uint64 `json:"sent-bytes"`
			} `json:"interface"`
		} `json:"statistics"`
	}
	if json.Unmarshal(out, &v) != nil {
		return 0, 0, false
	}
	for _, i := range v.Statistics.Interface {
		if strings.HasPrefix(i.Network, "<Link") && !strings.HasPrefix(i.Name, "lo") {
			rx, tx, ok = rx+i.Rx, tx+i.Tx, true
		}
	}
	return rx, tx, ok
}
