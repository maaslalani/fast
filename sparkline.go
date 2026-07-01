package main

import "strings"

// dots maps a pixel within a braille cell (x in 0..1, y in 0..3) to the bit it
// sets in the cell's byte. A braille rune packs a 2x4 grid of dots, so each
// cell gives us two columns and four levels to draw with.
var dots = [4][2]byte{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

// sparkline renders values as a single row of braille, width cells wide. Each
// column is filled from the bottom up to its value, so the series reads like a
// tiny bar chart that climbs as the speed ramps up.
func sparkline(values []float64, peak float64, width int) string {
	cols, rows := width*2, 4
	cells := make([]byte, width)

	if len(values) > 1 && peak > 0 {
		for px := range cols {
			// Resample the series across the full width, interpolating between
			// samples so the line stays smooth when we only have a few.
			pos := float64(px) / float64(cols-1) * float64(len(values)-1)
			i := int(pos)
			v := values[i]
			if i+1 < len(values) {
				v += (values[i+1] - values[i]) * (pos - float64(i))
			}

			height := int(v/peak*float64(rows) + 0.5)
			for y := rows - height; y < rows; y++ {
				cells[px/2] |= dots[y][px%2]
			}
		}
	}

	var b strings.Builder
	for _, bits := range cells {
		if bits == 0 {
			b.WriteByte(' ')
		} else {
			b.WriteRune(rune(0x2800 + int(bits)))
		}
	}
	return b.String()
}
