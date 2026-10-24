package main

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGetSystemTimes       = kernel32.NewProc("GetSystemTimes")
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
)

type cpuSampler struct{ idle, total uint64 }

func ft(f syscall.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }

// Sample renvoie l'utilisation CPU (0-100) depuis l'appel précédent.
func (c *cpuSampler) Sample() float64 {
	var idle, kernel, user syscall.Filetime
	procGetSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	i, t := ft(idle), ft(kernel)+ft(user) // kernel inclut déjà le temps idle
	di, dt := i-c.idle, t-c.total
	c.idle, c.total = i, t
	if dt == 0 {
		return 0
	}
	return 100 * (1 - float64(di)/float64(dt))
}

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// Memory renvoie la RAM utilisée et totale, en Go.
func Memory() (used, total float64) {
	m := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m)))
	const gb = 1 << 30
	return float64(m.TotalPhys-m.AvailPhys) / gb, float64(m.TotalPhys) / gb
}

// GPU interroge nvidia-smi ; ok=false si absent.
func GPU() (load, temp float64, ok bool) {
	cmd := exec.Command("nvidia-smi", "--query-gpu=utilization.gpu,temperature.gpu", "--format=csv,noheader,nounits")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.Output()
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
