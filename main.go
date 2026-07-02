package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	// connections is the number of parallel downloads we use to saturate the
	// connection, the same as fast.com.
	connections = 5

	// duration is how long we measure the connection speed for.
	duration = 10 * time.Second

	// sparkWidth is the width, in cells, of the speed sparkline.
	sparkWidth = 20
)

const (
	downloadColor = "#2EF8BB"
	uploadColor   = "#BD52FF"
)

var (
	speedStyle   = lipgloss.NewStyle().Bold(true)
	unitStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	dlSparkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(downloadColor))
	ulSparkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(uploadColor))
	peakStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	baseStyle    = lipgloss.NewStyle().Padding(1, 2)
)

const tickInterval = time.Second / 10

type tickMsg time.Time

func tickCmd(t time.Time) tea.Msg {
	return tickMsg(t)
}

type Model struct {
	targets []string

	dlBytes  *atomic.Int64
	dlCtx    context.Context
	dlCancel context.CancelFunc
	dlStart  time.Time
	dlSpeed  float64
	dlSpeeds []float64
	dlPeak   float64
	dlDone   bool

	ulBytes  *atomic.Int64
	ulCtx    context.Context
	ulCancel context.CancelFunc
	ulStart  time.Time
	ulSpeed  float64
	ulSpeeds []float64
	ulPeak   float64
	ulDone   bool

	quitting bool
}

func NewModel(targets []string) Model {
	dlCtx, dlCancel := context.WithTimeout(context.Background(), duration)

	return Model{
		targets:  targets,
		dlBytes:  &atomic.Int64{},
		dlCtx:    dlCtx,
		dlCancel: dlCancel,
		dlStart:  time.Now(),
		ulBytes:  &atomic.Int64{},
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tea.Tick(tickInterval, tickCmd), m.measureDownload)
}

// measureDownload kicks off the parallel downloads that feed our byte counter
func (m Model) measureDownload() tea.Msg {
	for _, url := range m.targets {
		go download(m.dlCtx, url, m.dlBytes)
	}
	return nil
}

// measureUpload kicks off the parallel uploads that feed our upload byte counter
func (m Model) measureUpload() tea.Msg {
	for _, url := range m.targets {
		go upload(m.ulCtx, url, m.ulBytes)
	}
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			if m.dlCancel != nil {
				m.dlCancel()
			}
			if m.ulCancel != nil {
				m.ulCancel()
			}
			return m, tea.Quit
		}

	case tickMsg:
		if !m.dlDone {
			elapsed := time.Since(m.dlStart)
			m.dlSpeed = mbps(m.dlBytes.Load(), elapsed)
			m.dlSpeeds = append(m.dlSpeeds, m.dlSpeed)
			if m.dlSpeed > m.dlPeak {
				m.dlPeak = m.dlSpeed
			}

			if elapsed >= duration {
				m.dlDone = true
				if m.dlCancel != nil {
					m.dlCancel()
				}
				m.ulStart = time.Now()
				m.ulCtx, m.ulCancel = context.WithTimeout(context.Background(), duration)
				return m, tea.Batch(tea.Tick(tickInterval, tickCmd), m.measureUpload)
			}
		} else if !m.ulDone {
			elapsed := time.Since(m.ulStart)
			m.ulSpeed = mbps(m.ulBytes.Load(), elapsed)
			m.ulSpeeds = append(m.ulSpeeds, m.ulSpeed)
			if m.ulSpeed > m.ulPeak {
				m.ulPeak = m.ulSpeed
			}

			if elapsed >= duration {
				m.ulDone = true
				if m.ulCancel != nil {
					m.ulCancel()
				}
				return m, tea.Quit
			}
		}

		return m, tea.Tick(tickInterval, tickCmd)
	}

	return m, nil
}

func renderRow(currentSpeed float64, speeds []float64, peak float64, sparkStyle lipgloss.Style) string {
	var s strings.Builder
	// Cap each readout at 999.9 and switch to Gbps beyond that, keeping a fixed
	// width so the unit, sparkline, and peak never shift horizontally.
	speed, unit := scale(currentSpeed)
	s.WriteString(speedStyle.Render(fmt.Sprintf("%5.1f", speed)))
	s.WriteString(unitStyle.Render(" " + unit))
	s.WriteString(" ")
	s.WriteString(sparkStyle.Render(sparkline(speeds, peak, sparkWidth)))
	if peak > 0 {
		peakVal, peakUnit := scale(peak)
		// Only label the peak's unit when it differs from the live reading's.
		label := fmt.Sprintf("  peak %.0f", peakVal)
		if peakUnit != unit {
			label += " " + peakUnit
		}
		s.WriteString(peakStyle.Render(label))
	}
	return s.String()
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var s strings.Builder
	s.WriteString(renderRow(m.dlSpeed, m.dlSpeeds, m.dlPeak, dlSparkStyle))
	if m.dlDone {
		s.WriteString("\n")
		s.WriteString(renderRow(m.ulSpeed, m.ulSpeeds, m.ulPeak, ulSparkStyle))
	}

	style := baseStyle
	if m.ulDone {
		style = style.PaddingBottom(2)
	}
	return style.Render(s.String())
}

// mbps converts a number of bytes downloaded over a duration into megabits per
// second, the unit fast.com reports.
func mbps(bytes int64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(bytes) * 8 / d.Seconds() / 1e6
}

// scale converts a speed in Mbps to its display magnitude and unit, switching to
// Gbps once it would read past 999.9 Mbps so the value never exceeds "999.9".
func scale(speed float64) (float64, string) {
	if speed >= 999.95 {
		return speed / 1000, "Gbps"
	}
	return speed, "Mbps"
}

func main() {
	urls, err := targets(connections)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) {
			fmt.Fprintln(os.Stderr, "No internet connection.")
			os.Exit(1)
		}
		log.Fatal(err)
	}

	if _, err := tea.NewProgram(NewModel(urls)).Run(); err != nil {
		log.Fatal(err)
	}
}
