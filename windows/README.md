# miniscreenWin

Command-line driver for the WeAct Studio **Display FS 0.96 Inch** USB screen
(160x80 IPS, USB CDC serial, VID `1A86` / PID `FE0C`), for Windows.
It shows a live clock + CPU / RAM / GPU dashboard, images, animated GIFs or text.

The serial protocol comes from
[turing-smart-screen-python](https://github.com/mathoudebine/turing-smart-screen-python)
(`lcd_comm_weact_b.py`): each command ends with `0x0A`, 16-bit values are little-endian,
pixels are sent as RGB565 little-endian.

## Build

Requires Go 1.26 or later.

```
go build -o miniscreen.exe .
```

The `.exe` is not committed (see `.gitignore`).

## Usage

```
miniscreen [options] <command> [arguments]
```

| Command | What it does |
|---|---|
| `info [-interval 1s]` | Live dashboard: clock, date, CPU / RAM / GPU bars, GPU temperature. Ctrl+C to quit. |
| `image [-cover] [-loop N] <file>` | Shows an image (png, jpg, bmp, webp, animated GIF). Fitted with black bars by default; `-cover` fills the screen and crops. `-loop` = number of GIF loops (0 = forever). |
| `text [-color hex] [-bg hex] <text>` | Shows text, font size chosen automatically to fit. `\n` = new line. |
| `clear [-color hex]` | Fills the screen with a colour (black by default). |
| `brightness <0-100>` | Sets the brightness. |
| `version` | Prints the screen firmware version. |
| `ports` | Lists the serial ports and marks the screen. |

Global options (before the command):

| Option | Meaning |
|---|---|
| `-port COM9` | Serial port; found automatically by VID/PID if empty. |
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

- **Landscape:** time with seconds on the left, date and GPU temperature on the right,
  CPU / RAM / GPU bars below.
- **Portrait:** time and date centred at the top, then one block per metric
  (label and value above a full-width bar).

Bars turn green below 60 %, orange below 85 %, red above.
CPU and RAM come from the Windows API (`GetSystemTimes`, `GlobalMemoryStatusEx`);
GPU load and temperature from `nvidia-smi`. Without an NVIDIA GPU the GPU line is simply hidden.

## Files

| File | Content |
|---|---|
| `lcd.go` | Screen protocol: port detection, orientation, brightness, fill, bitmap upload |
| `sysinfo.go` | CPU, RAM and GPU readings |
| `main.go` | Commands, image and GIF rendering, text layout, dashboard |
