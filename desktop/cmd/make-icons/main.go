// make-icons writes the browser extension's PNG icons from the same drawing as the tray icon, and with
// -app the app's own icon (the exe, Start menu, Settings → Apps: the mark on a blue tile with a sparkle)
// at every size Windows asks for. With
// -preview it also writes a sheet of every tray state on light and dark taskbars, for checking by eye.
//
//	go run ./cmd/make-icons -app cmd/circle-lens/winres ../browser-extension/icons
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"

	"circlelens/internal/icon"
)

func main() {
	preview := flag.String("preview", "", "also write a preview sheet of the tray icon states to this PNG")
	app := flag.String("app", "", "also write the app icon's sizes (icon-16.png ... icon-256.png) to this folder")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: make-icons [-preview sheet.png] <output folder>")
		os.Exit(2)
	}
	folder := flag.Arg(0)
	check(os.MkdirAll(folder, 0o755))
	// The browser toolbar can be light or dark: the brand blue reads on both, with a pale glass.
	extension := icon.Style{Ink: icon.Accent, Glass: color.NRGBA{R: 0x25, G: 0x63, B: 0xeb, A: 0x30}}
	for _, size := range []int{16, 32, 48, 128} {
		path := filepath.Join(folder, fmt.Sprintf("icon-%d.png", size))
		// The toolbar sizes are the mark alone, like other toolbar icons; the extensions page and the
		// store show the app icon.
		picture := icon.Draw(size, extension)
		if size >= 48 {
			picture = icon.DrawApp(size)
		}
		check(writePNG(path, picture))
		fmt.Println("wrote", path)
	}
	if *app != "" {
		check(os.MkdirAll(*app, 0o755))
		for _, size := range []int{16, 20, 24, 32, 40, 48, 64, 96, 128, 256} {
			path := filepath.Join(*app, fmt.Sprintf("icon-%d.png", size))
			check(writePNG(path, icon.DrawApp(size)))
		}
		fmt.Println("wrote the app icon to", *app)
	}
	if *preview != "" {
		check(writePNG(*preview, sheet()))
		fmt.Println("wrote", *preview)
	}
}

// sheet lays out the tray states (watching, open, paused) at 16, 20, 24, 32 and 48 px, on a dark and
// a light taskbar, each drawn at 4× so the pixels can be judged.
func sheet() image.Image {
	sizes := []int{16, 20, 24, 32, 48}
	type taskbar struct {
		background color.NRGBA
		ink        color.NRGBA
	}
	taskbars := []taskbar{{color.NRGBA{0x1c, 0x1c, 0x1c, 255}, color.NRGBA{255, 255, 255, 255}}, {color.NRGBA{0xee, 0xee, 0xee, 255}, color.NRGBA{0x1a, 0x1a, 0x1a, 255}}}
	accent := color.NRGBA{0x00, 0x78, 0xd4, 255}
	const zoom, cell = 4, 48*4 + 16
	out := image.NewNRGBA(image.Rect(0, 0, cell*len(sizes), cell*3*len(taskbars)))
	for t, bar := range taskbars {
		states := []icon.Style{
			{Ink: bar.ink},
			{Ink: bar.ink, Glass: accent, GlassInk: color.NRGBA{255, 255, 255, 255}},
			{Ink: bar.ink, Paused: true},
		}
		for s, style := range states {
			row := (t*len(states) + s) * cell
			draw.Draw(out, image.Rect(0, row, out.Bounds().Dx(), row+cell), &image.Uniform{bar.background}, image.Point{}, draw.Src)
			for i, size := range sizes {
				picture := icon.Draw(size, style)
				for y := 0; y < size*zoom; y++ {
					for x := 0; x < size*zoom; x++ {
						c := picture.NRGBAAt(x/zoom, y/zoom)
						if c.A == 0 {
							continue
						}
						px, py := i*cell+8+x, row+8+y
						under := out.NRGBAAt(px, py)
						a := float64(c.A) / 255
						out.SetNRGBA(px, py, color.NRGBA{
							R: uint8(float64(c.R)*a + float64(under.R)*(1-a)),
							G: uint8(float64(c.G)*a + float64(under.G)*(1-a)),
							B: uint8(float64(c.B)*a + float64(under.B)*(1-a)), A: 255,
						})
					}
				}
			}
		}
	}
	return out
}

func writePNG(path string, picture image.Image) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(file, picture); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
