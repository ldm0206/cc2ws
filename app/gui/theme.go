//go:build windows || darwin

package gui

import (
	_ "embed"
	"image"
	"image/color"

	"gioui.org/font/opentype"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

//go:embed assets/NotoSansSC-Regular.otf
var notoSansSC []byte

func loadTheme() *material.Theme {
	th := material.NewTheme()
	if faces, err := opentype.ParseCollection(notoSansSC); err == nil && len(faces) > 0 {
		th.Shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(faces))
	}
	th.Palette = material.Palette{
		Bg:         color.NRGBA{R: 24, G: 26, B: 32, A: 255},
		Fg:         color.NRGBA{R: 235, G: 237, B: 240, A: 255},
		ContrastBg: color.NRGBA{R: 38, G: 96, B: 212, A: 255},
		ContrastFg: color.NRGBA{R: 255, G: 255, B: 255, A: 255},
	}
	th.TextSize = unit.Sp(15)
	return th
}

func statusColor(running bool, errMsg string) color.NRGBA {
	if errMsg != "" {
		return color.NRGBA{R: 220, G: 80, B: 80, A: 255}
	}
	if running {
		return color.NRGBA{R: 80, G: 190, B: 110, A: 255}
	}
	return color.NRGBA{R: 120, G: 124, B: 130, A: 255}
}

func dot(gtx layout.Context, c color.NRGBA) layout.Dimensions {
	const r = 5
	defer clip.RRect{Rect: image.Rect(0, 0, r*2, r*2), SE: r, SW: r, NW: r, NE: r}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, c)
	return layout.Dimensions{Size: image.Pt(r*2, r*2)}
}
