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

// skin holds the active Material Design color tokens. material.Theme.Palette
// only exposes Bg/Fg/ContrastBg/ContrastFg, so the rest of the MD3 surface
// ramp is carried here and applied where widgets are drawn.
type skin struct {
	pal     material.Palette // Bg / Fg / ContrastBg / ContrastFg
	card    color.NRGBA      // surface-container (raised panels)
	outline color.NRGBA      // input outlines, dividers
	input   color.NRGBA      // input field fill
	navBg   color.NRGBA      // inactive nav pill background
	hint    color.NRGBA      // placeholder / secondary text
	subtle  color.NRGBA      // muted version text
}

func darkSkin() skin {
	return skin{
		pal: material.Palette{
			Bg:         color.NRGBA{R: 0x12, G: 0x12, B: 0x15, A: 0xff},
			Fg:         color.NRGBA{R: 0xE6, G: 0xE1, B: 0xE9, A: 0xff},
			ContrastBg: color.NRGBA{R: 0x8A, G: 0xA9, B: 0xFF, A: 0xff},
			ContrastFg: color.NRGBA{R: 0x00, G: 0x29, B: 0x6E, A: 0xff},
		},
		card:    color.NRGBA{R: 0x1E, G: 0x1E, B: 0x24, A: 0xff},
		outline: color.NRGBA{R: 0x49, G: 0x45, B: 0x4E, A: 0xff},
		input:   color.NRGBA{R: 0x2A, G: 0x2A, B: 0x31, A: 0xff},
		navBg:   color.NRGBA{R: 0x2A, G: 0x2A, B: 0x31, A: 0xff},
		hint:    color.NRGBA{R: 0x9C, G: 0x97, B: 0xA4, A: 0xff},
		subtle:  color.NRGBA{R: 0x9C, G: 0x97, B: 0xA4, A: 0xff},
	}
}

func lightSkin() skin {
	return skin{
		pal: material.Palette{
			Bg:         color.NRGBA{R: 0xFE, G: 0xF7, B: 0xFF, A: 0xff},
			Fg:         color.NRGBA{R: 0x1D, G: 0x1B, B: 0x20, A: 0xff},
			ContrastBg: color.NRGBA{R: 0x4D, G: 0x5B, B: 0xEE, A: 0xff},
			ContrastFg: color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xff},
		},
		card:    color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xff},
		outline: color.NRGBA{R: 0xC9, G: 0xC5, B: 0xD4, A: 0xff},
		input:   color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xff},
		navBg:   color.NRGBA{R: 0xE7, G: 0xE0, B: 0xEC, A: 0xff},
		hint:    color.NRGBA{R: 0x79, G: 0x75, B: 0x80, A: 0xff},
		subtle:  color.NRGBA{R: 0x6A, G: 0x66, B: 0x72, A: 0xff},
	}
}

func skinFor(mode string) skin {
	if mode == "light" {
		return lightSkin()
	}
	return darkSkin()
}

func newTheme() *material.Theme {
	th := material.NewTheme()
	if faces, err := opentype.ParseCollection(notoSansSC); err == nil && len(faces) > 0 {
		th.Shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(faces))
	}
	th.TextSize = unit.Sp(16)
	return th
}

func applySkin(th *material.Theme, s skin) {
	th.Palette = s.pal
}

func statusColor(s skin, running bool, errMsg string) color.NRGBA {
	if errMsg != "" {
		return color.NRGBA{R: 0xE2, G: 0x55, B: 0x55, A: 0xff} // MD error
	}
	if running {
		return color.NRGBA{R: 0x4E, G: 0xC9, B: 0x6F, A: 0xff} // green
	}
	return s.hint
}

func dot(gtx layout.Context, c color.NRGBA) layout.Dimensions {
	const r = 5
	defer clip.RRect{Rect: image.Rect(0, 0, r*2, r*2), SE: r, SW: r, NW: r, NE: r}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, c)
	return layout.Dimensions{Size: image.Pt(r*2, r*2)}
}

// surface draws w atop a rounded, raised panel in the active skin's card color.
func (p *pages) surface(gtx layout.Context, w layout.Widget) layout.Dimensions {
	card := p.skin.card
	return layout.Stack{Alignment: layout.NW}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			r := clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Min}, 12)
			paint.FillShape(gtx.Ops, card, r.Op(gtx.Ops))
			return layout.Dimensions{Size: gtx.Constraints.Min}
		}),
		layout.Stacked(w),
	)
}
