// Package image provides terminal image protocol detection and rendering.
// It wraps rasterm to abstract over Kitty graphics, iTerm2 inline images,
// and Sixel, with a ROOST_IMAGES override for explicit protocol selection.
//
// Typical usage:
//
//	cap := image.Detect()
//	if cap != image.CapNone {
//	    out := image.Render(img, w, h, cap)
//	    fmt.Print(out)
//	}
package image

import (
	"bytes"
	"image"
	"image/color/palette"
	"image/draw"
	"os"

	"github.com/BourgeoisBear/rasterm"
)

// Capability represents the terminal's inline-image support level.
type Capability int

const (
	CapNone   Capability = iota // no inline-image support (or explicitly disabled)
	CapKitty                    // Kitty graphics protocol
	CapITerm2                   // iTerm2 / WezTerm inline images
	CapSixel                    // Sixel graphics
)

func (c Capability) String() string {
	switch c {
	case CapKitty:
		return "kitty"
	case CapITerm2:
		return "iterm2"
	case CapSixel:
		return "sixel"
	default:
		return "none"
	}
}

// Detect returns the best image capability available in the current terminal.
// ROOST_IMAGES env var overrides automatic detection:
//
//	off     → CapNone
//	kitty   → CapKitty
//	iterm2  → CapITerm2
//	sixel   → CapSixel
//	auto    → autodetect (default behaviour when unset)
//
// Inside tmux (without passthrough configured), autodetect always returns
// CapNone to avoid garbled output.
func Detect() Capability {
	switch os.Getenv("ROOST_IMAGES") {
	case "off":
		return CapNone
	case "kitty":
		return CapKitty
	case "iterm2":
		return CapITerm2
	case "sixel":
		return CapSixel
	}
	// auto (or unset)
	if rasterm.IsTmuxScreen() {
		return CapNone
	}
	if rasterm.IsKittyCapable() {
		return CapKitty
	}
	if rasterm.IsItermCapable() {
		return CapITerm2
	}
	if ok, _ := rasterm.IsSixelCapable(); ok {
		return CapSixel
	}
	return CapNone
}

// Render encodes img as an inline terminal image escape sequence using the
// given capability. Returns "" for CapNone or on error.
func Render(img image.Image, cap Capability) string {
	if img == nil || cap == CapNone {
		return ""
	}
	var buf bytes.Buffer
	var err error
	switch cap {
	case CapKitty:
		err = rasterm.KittyWriteImage(&buf, img, rasterm.KittyImgOpts{})
	case CapITerm2:
		err = rasterm.ItermWriteImage(&buf, img)
	case CapSixel:
		err = rasterm.SixelWriteImage(&buf, toPaletted(img))
	}
	if err != nil {
		return ""
	}
	return buf.String()
}

// toPaletted converts any image.Image to a paletted image required by
// SixelWriteImage. Uses the standard Plan9 256-color palette so that pixel
// colors are faithfully approximated rather than all collapsing to black.
func toPaletted(src image.Image) *image.Paletted {
	bounds := src.Bounds()
	dst := image.NewPaletted(bounds, palette.Plan9)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Src)
	return dst
}
