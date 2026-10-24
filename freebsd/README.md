# miniscreenBsd

Command-line driver for the WeAct Studio **Display FS 0.96 Inch** USB screen
(160x80 IPS, USB CDC serial, VID `1A86` / PID `FE0C`), for FreeBSD.
It shows a live clock + CPU / RAM / GPU dashboard, images, animated GIFs or text.

This is the FreeBSD port of [miniscreenWin](../miniscreenWin): same commands and rendering,
only port detection and system readings change.

The serial protocol comes from
[turing-smart-screen-python](https://github.com/mathoudebine/turing-smart-screen-python)
(`lcd_comm_weact_b.py`): each command ends with `0x0A`, 16-bit values are little-endian,
pixels are sent as RGB565 little-endian.

## Build

Requires Go 1.26 or later (`pkg install go`). On FreeBSD:

```
go build -o miniscreen .
```

Or cross-compile from another machine (no cgo needed):

```
GOOS=freebsd GOARCH=amd64 go build -o miniscreen .
```

The binary is not committed (see `.gitignore`).

## FreeBSD setup

The screen is a USB CDC ACM device, handled by the `umodem(4)` driver, which creates
`/dev/cuaU0` (and `/dev/ttyU0`). Check it is attached:

```
usbconfig | grep -i 1a86
sysctl dev.umodem
```

If `umodem` is not loaded: `kldload umodem` (add `umodem_load="YES"` to `/boot/loader.conf`
to make it permanent).

`/dev/cuaU*` belongs to `uucp:dialer`, so add your user to the `dialer` group
(`pw groupmod dialer -m $USER`, then log in again) or run as root.

## Service at boot

`rc.d/miniscreen` runs the dashboard at boot under `daemon(8)`. It restarts miniscreen
5 s after any exit (screen unplugged, not attached yet) and sends its output to syslog
(`/var/log/messages`, tag `miniscreen`). When the service stops, the screen is blanked.

Install, as root, from this folder after `go build`:

```
install -m 755 miniscreen /usr/local/bin/miniscreen
install -m 555 rc.d/miniscreen /usr/local/etc/rc.d/miniscreen
sysrc miniscreen_enable=YES
service miniscreen start
```

Optional settings (`sysrc`):

| Variable | Default | Meaning |
|---|---|---|
| `miniscreen_args` | `info` | Command and options, e.g. `"-brightness 60 info -rotate 10s"` |
| `miniscreen_bin` | `/usr/local/bin/miniscreen` | Path of the binary |

To have the CPU temperature from boot: `sysrc kld_list+=coretemp` (or `amdtemp`).
`service miniscreen stop|restart|status` control it. After rebuilding, run the
`install -m 755` line again, then `service miniscreen restart`.

## Usage

```
miniscreen [options] <command> [arguments]
```

| Command | What it does |
|---|---|
| `info [-interval 1s] [-rotate 8s] [-page N]` | Live dashboard with three pages: clock, gauges, history (see below). Ctrl+C to quit. |
| `image [-cover] [-loop N] <file>` | Shows an image (png, jpg, bmp, webp, animated GIF). Fitted with black bars by default; `-cover` fills the screen and crops. `-loop` = number of GIF loops (0 = forever). |
| `text [-color hex] [-bg hex] <text>` | Shows text, font size chosen automatically to fit. `\n` = new line. |
| `clear [-color hex]` | Fills the screen with a colour (black by default). |
| `brightness <0-100>` | Sets the brightness. |
| `version` | Prints the screen firmware version. |
| `ports` | Lists the USB serial ports (ucom drivers) and marks the screen. |

Global options (before the command):

| Option | Meaning |
|---|---|
| `-port /dev/cuaU0` | Serial port (`cuaU0` alone also works); found automatically by VID/PID if empty. |
| `-portrait` | Portrait orientation (80x160) instead of landscape. |
| `-flip` | Rotates the display by 180°. |
| `-brightness N` | Sets the brightness (0-100) before running the command. |

Examples:

```
miniscreen info
miniscreen -portrait info
miniscreen -brightness 40 image -cover photo.jpg
miniscreen image -loop 3 anim.gif
miniscreen text -color ffcc00 "Hello\nworld"
miniscreen clear -color 202040
```

## Dashboard

`info` cycles through three pages (every 8 s by default, `-rotate` to change it,
`-page N` to stay on one page). Each page has a landscape and a portrait layout.

1. **Clock:** big time with seconds, full date, hostname and uptime,
   plus a bar that fills over the minute.
2. **Gauges:** ring gauges for CPU, RAM, CPU temperature and GPU, each in its own
   colour, turning red above 80 %. The temperature and GPU rings only appear when
   the reading exists.
3. **History:** CPU load over the last ~160 s with the 1-minute load average,
   and network traffic (download as a green area, upload as a violet line).

Sources:

| Reading | Source |
|---|---|
| CPU | `kern.cp_time` |
| RAM | `hw.physmem` and `vm.stats.vm.*` (available = free + inactive + laundry pages, as in `top`; the ZFS ARC counts as used) |
| CPU temperature | `dev.cpu.N.temperature`, needs `kldload coretemp` (Intel) or `kldload amdtemp` (AMD) |
| Load average, uptime | `vm.loadavg`, `kern.boottime` |
| Network | `netstat -ibn --libxo json`, all interfaces except loopback |
| GPU | `nvidia-smi` (`nvidia-driver` package) |

## Differences from miniscreenWin

- `go.bug.st/serial`'s USB enumerator is not implemented on FreeBSD, so `ports.go` reads
  `dev.<driver>.<n>.ttyname` and `dev.<driver>.<n>.%pnpinfo` for the ucom drivers instead.
- `sysinfo.go` uses sysctl instead of the Windows API.
- The serial port is opened with DTR and RTS on, as Windows does, and every write waits
  until it has left (`tcdrain`); without that the screen ignores bitmaps.
  Calls interrupted by a signal (EINTR) are retried.
- The code builds for FreeBSD only (it calls `golang.org/x/sys/unix` sysctl helpers).

## Files

| File | Content |
|---|---|
| `lcd.go` | Screen protocol: port detection, orientation, brightness, fill, bitmap upload |
| `ports.go` | USB serial port listing through sysctl |
| `sysinfo.go` | CPU, RAM, temperature, load, uptime, network and GPU readings |
| `draw.go` | Fonts and anti-aliased drawing: text, rounded rectangles, rings, charts |
| `dashboard.go` | Dashboard pages, independent of the screen and of FreeBSD |
| `main.go` | Commands, image and GIF rendering, text layout, dashboard loop |
| `rc.d/miniscreen` | FreeBSD rc.d script to run the dashboard at boot |
