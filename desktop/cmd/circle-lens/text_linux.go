//go:build linux

package main

// Text for the lens's zoom pill. Linux has no system text API a pure-Go program can call, so it's drawn
// with golang.org/x/image (pure Go, from the Go team) from a font that's already installed: the one
// fontconfig picks for a medium sans-serif, or the first of a few common ones. No font is bundled.

import (
	"image"
	"math"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type textFace struct {
	font  *opentype.Font
	faces map[float64]font.Face
}

// commonFonts are tried when fontconfig isn't there to ask.
var commonFonts = []string{
	"/usr/share/fonts/truetype/ubuntu/Ubuntu-M.ttf",
	"/usr/share/fonts/cantarell/Cantarell-VF.otf",
	"/usr/share/fonts/truetype/noto/NotoSans-Medium.ttf",
	"/usr/share/fonts/noto/NotoSans-Medium.ttf",
	"/usr/share/fonts/google-noto/NotoSans-Medium.ttf",
	"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
	"/usr/share/fonts/TTF/DejaVuSans.ttf",
	"/usr/share/fonts/dejavu/DejaVuSans.ttf",
}

func loadTextFace() *textFace {
	candidates := commonFonts
	if out, err := exec.Command("fc-match", "-f", "%{file}", "sans-serif:weight=medium").Output(); err == nil && len(out) > 0 {
		candidates = append([]string{strings.TrimSpace(string(out))}, candidates...)
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if parsed, err := opentype.Parse(data); err == nil {
			return &textFace{font: parsed, faces: map[float64]font.Face{}}
		}
	}
	return nil
}

func (s *x11System) text(line string, pixels float64) ([]byte, int, int) {
	s.fontOnce.Do(func() { s.fontFace = loadTextFace() })
	if s.fontFace == nil {
		return nil, 0, 0
	}
	return s.fontFace.draw(line, pixels)
}

// draw renders a line in white on nothing; the coverage is the alpha.
func (t *textFace) draw(line string, pixels float64) ([]byte, int, int) {
	size := math.Round(pixels*4) / 4
	face := t.faces[size]
	if face == nil {
		var err error
		face, err = opentype.NewFace(t.font, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return nil, 0, 0
		}
		t.faces[size] = face
	}
	metrics := face.Metrics()
	width := font.MeasureString(face, line).Ceil()
	height := (metrics.Ascent + metrics.Descent).Ceil()
	if width <= 0 || height <= 0 {
		return nil, 0, 0
	}
	canvas := image.NewAlpha(image.Rect(0, 0, width, height))
	drawer := font.Drawer{Dst: canvas, Src: image.Opaque, Face: face, Dot: fixed.Point26_6{Y: metrics.Ascent}}
	drawer.DrawString(line)
	return canvas.Pix, width, height
}
