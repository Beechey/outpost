package ui

import (
	"strings"

	"rsc.io/qr"
)

// QR draws text as a QR code for a phone's camera, two rows of modules per
// line of half blocks, dark on light with the quiet zone scanners need.
func QR(text string) string {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return ""
	}
	const quiet = 2
	dark := func(x, y int) bool {
		x, y = x-quiet, y-quiet
		return x >= 0 && y >= 0 && x < code.Size && y < code.Size && code.Black(x, y)
	}
	size := code.Size + 2*quiet
	var b strings.Builder
	for y := 0; y < size; y += 2 {
		b.WriteString("  ")
		for x := 0; x < size; x++ {
			switch top, bottom := dark(x, y), dark(x, y+1); {
			case top && bottom:
				b.WriteString(" ")
			case top:
				b.WriteString("▄")
			case bottom:
				b.WriteString("▀")
			default:
				b.WriteString("█")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}
