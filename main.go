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
	// connections is the number of parallel transfers we use to saturate the
	// connection, the same as fast.com.
	connections = 5

	// downloadDuration is how long we measure download speed for.
	downloadDuration = 10 * time.Second

	// uploadDuration is how long we measure upload speed for.
	uploadDuration = 5 * time.Second

	// latencySamples is how many unloaded requests we time to get a ping reading.
	latencySamples = 5

	// sparkWidth is the width, in cells, of the speed sparklines.
	sparkWidth = 20
)

const accentColor = "#2EF8BB"

var (
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Width(10)
	speedStyle = lipgloss.NewStyle().Bold(true)
	unitStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sparkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(accentColor))
	peakStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	baseStyle  = lipgloss.NewStyle().Padding(1, 2)
)

const tickInterval = time.Second / 10

type tickMsg time.Time

func tickCmd(t time.Time) tea.Msg {
	return tickMsg(t)
}

// latencyMsg carries the measured ping once the latency phase completes.
type latencyMsg time.Duration

// phase tracks which stage of the test is currently running. They run in
// order: a quick ping, then download, then upload, matching fast.com.
type phase int

const (
	phaseLatency phase = iota
	phaseDownload
	phaseUpload
)

type Model struct {
	targets []target
	phase   phase

	bytes  *atomic.Int64
	ctx    context.Context
	cancel context.CancelFunc

	start   time.Time
	latency time.Duration

	downloadSpeed  float64
	downloadSpeeds []float64
	downloadPeak   float64

	uploadSpeed  float64
	uploadSpeeds []float64
	uploadPeak   float64

	done     bool
	quitting bool
}

func NewModel(targets []target) Model {
	return Model{targets: targets}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tea.Tick(tickInterval, tickCmd), m.measureLatency)
}

// measureLatency pings the first target server before the download starts.
func (m Model) measureLatency() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return latencyMsg(latency(ctx, m.targets[0].URL, latencySamples))
}

// measureDownload kicks off the parallel downloads that feed our byte counter.
func (m Model) measureDownload() tea.Msg {
	for _, t := range m.targets {
		go download(m.ctx, t.URL, m.bytes)
	}
	return nil
}

// measureUpload kicks off the parallel uploads that feed our byte counter.
func (m Model) measureUpload() tea.Msg {
	for _, t := range m.targets {
		go upload(m.ctx, t.URL, m.bytes)
	}
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}

	case latencyMsg:
		m.latency = time.Duration(msg)
		m.phase = phaseDownload
		m.start = time.Now()
		m.bytes = &atomic.Int64{}
		m.ctx, m.cancel = context.WithTimeout(context.Background(), downloadDuration)
		return m, m.measureDownload

	case tickMsg:
		switch m.phase {
		case phaseDownload:
			elapsed := time.Since(m.start)
			m.downloadSpeed = mbps(m.bytes.Load(), elapsed)
			m.downloadSpeeds = append(m.downloadSpeeds, m.downloadSpeed)
			if m.downloadSpeed > m.downloadPeak {
				m.downloadPeak = m.downloadSpeed
			}

			if elapsed >= downloadDuration {
				m.cancel()
				m.phase = phaseUpload
				m.start = time.Now()
				m.bytes = &atomic.Int64{}
				m.ctx, m.cancel = context.WithTimeout(context.Background(), uploadDuration)
				return m, tea.Batch(tea.Tick(tickInterval, tickCmd), m.measureUpload)
			}
			return m, tea.Tick(tickInterval, tickCmd)

		case phaseUpload:
			elapsed := time.Since(m.start)
			m.uploadSpeed = mbps(m.bytes.Load(), elapsed)
			m.uploadSpeeds = append(m.uploadSpeeds, m.uploadSpeed)
			if m.uploadSpeed > m.uploadPeak {
				m.uploadPeak = m.uploadSpeed
			}

			if elapsed >= uploadDuration {
				m.done = true
				m.cancel()
				return m, tea.Quit
			}
			return m, tea.Tick(tickInterval, tickCmd)
		}

		return m, tea.Tick(tickInterval, tickCmd)
	}

	return m, nil
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var s strings.Builder
	s.WriteString(labelStyle.Render("Server"))
	s.WriteString(m.targets[0].Location)

	s.WriteString("\n")
	s.WriteString(labelStyle.Render("Latency"))
	if m.phase == phaseLatency {
		s.WriteString(unitStyle.Render("measuring…"))
	} else {
		s.WriteString(fmt.Sprintf("%d ms", m.latency.Milliseconds()))
	}

	if m.phase >= phaseDownload {
		s.WriteString("\n")
		s.WriteString(labelStyle.Render("Download"))
		s.WriteString(speedLine(m.downloadSpeed, m.downloadSpeeds, m.downloadPeak))
	}

	if m.phase >= phaseUpload {
		s.WriteString("\n")
		s.WriteString(labelStyle.Render("Upload"))
		s.WriteString(speedLine(m.uploadSpeed, m.uploadSpeeds, m.uploadPeak))
	}

	style := baseStyle
	if m.done {
		style = style.PaddingBottom(2)
	}
	return style.Render(s.String())
}

// speedLine renders a live speed reading with its sparkline and peak, the
// shared layout for both the download and upload rows.
func speedLine(speed float64, speeds []float64, peak float64) string {
	var s strings.Builder
	// Cap each readout at 999.9 and switch to Gbps beyond that, keeping a fixed
	// width so the unit, sparkline, and peak never shift horizontally.
	value, unit := scale(speed)
	s.WriteString(speedStyle.Render(fmt.Sprintf("%5.1f", value)))
	s.WriteString(unitStyle.Render(" " + unit))
	s.WriteString(" ")
	s.WriteString(sparkStyle.Render(sparkline(speeds, peak, sparkWidth)))
	if peak > 0 {
		peakValue, peakUnit := scale(peak)
		label := fmt.Sprintf("  peak %.0f", peakValue)
		// Only label the peak's unit when it differs from the live reading's.
		if peakUnit != unit {
			label += " " + peakUnit
		}
		s.WriteString(peakStyle.Render(label))
	}
	return s.String()
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

const usage = `Usage: fast [-h]

Test your internet speed from the command line, powered by fast.com.

Options:
  -h, --help   Show this help message and exit.
`

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "-h" || arg == "--help" {
			fmt.Print(usage)
			return
		}
	}

	servers, err := targets(connections)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) {
			fmt.Fprintln(os.Stderr, "No internet connection.")
			os.Exit(1)
		}
		log.Fatal(err)
	}

	if _, err := tea.NewProgram(NewModel(servers)).Run(); err != nil {
		log.Fatal(err)
	}
}
