package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	sparkWidth = 20

	downloadColor = "#2EF8BB"
	uploadColor   = "#BD52FF"

	downloadLabel = "↓"
	uploadLabel   = "↑"
)

var (
	speedStyle   = lipgloss.NewStyle().Bold(true)
	unitStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	dlSparkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(downloadColor))
	ulSparkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(uploadColor))
	peakStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	baseStyle    = lipgloss.NewStyle().Padding(1, 2)
)

func renderRow(label string, currentSpeed float64, speeds []float64, peak float64, sparkStyle lipgloss.Style) string {
	var s strings.Builder
	s.WriteString(sparkStyle.Render(label))
	s.WriteString(" ")
	speed, unit := scale(currentSpeed)
	s.WriteString(speedStyle.Render(fmt.Sprintf("%5.1f", speed)))
	s.WriteString(unitStyle.Render(" " + unit))
	s.WriteString(" ")
	s.WriteString(sparkStyle.Render(sparkline(speeds, peak, sparkWidth)))
	if peak > 0 {
		peakVal, peakUnit := scale(peak)
		label := fmt.Sprintf("  peak %.0f", peakVal)
		if peakUnit != unit {
			label += " " + peakUnit
		}
		s.WriteString(peakStyle.Render(label))
	}
	return s.String()
}

func renderSummary(m Model) string {
	sep := unitStyle.Render(" • ")
	ping := unitStyle.Render("—")
	if m.ping > 0 {
		ping = speedStyle.Render(fmt.Sprintf("%d", m.ping.Milliseconds())) + unitStyle.Render(" ms")
	}
	return summarySpeed(downloadLabel, m.download.speed, dlSparkStyle) + sep +
		summarySpeed(uploadLabel, m.upload.speed, ulSparkStyle) + sep + ping
}

func summarySpeed(label string, speed float64, sparkStyle lipgloss.Style) string {
	value, unit := scale(speed)
	return sparkStyle.Render(label) + " " +
		speedStyle.Render(fmt.Sprintf("%.1f", value)) +
		unitStyle.Render(" "+unit)
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var content string
	switch m.phase {
	case phaseLoading, phaseDownloading:
		content = renderRow(downloadLabel, m.download.speed, m.download.samples, m.download.peak, dlSparkStyle)
	case phaseUploading, phaseMeasuringPing:
		content = renderRow(uploadLabel, m.upload.speed, m.upload.samples, m.upload.peak, ulSparkStyle)
	case phaseDone:
		content = renderSummary(m)
	}

	style := baseStyle
	if m.phase == phaseDone {
		style = style.PaddingBottom(2)
	}
	return style.Render(content)
}

func scale(speed float64) (float64, string) {
	if speed >= 999.95 {
		return speed / 1000, "Gbps"
	}
	return speed, "Mbps"
}
