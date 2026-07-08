package fast

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	// connections is the number of parallel downloads we use to saturate the
	// connection. fast.com uses up to eight parallel downloads.
	connections = 8

	// duration is how long we measure the connection speed for.
	duration = 10 * time.Second

	// sparkWidth is the width, in cells, of the speed sparkline.
	sparkWidth = 20

	tickInterval = time.Second / 10
	accentColor  = "#2EF8BB"
)

var (
	speedStyle = lipgloss.NewStyle().Bold(true)
	unitStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sparkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(accentColor))
	peakStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	metaStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	baseStyle  = lipgloss.NewStyle().Padding(1, 2)
)

type tickMsg time.Time

func tickCmd(t time.Time) tea.Msg {
	return tickMsg(t)
}

type model struct {
	targets []target

	bytes  *atomic.Int64
	ctx    context.Context
	cancel context.CancelFunc

	start      time.Time
	last       speedSample
	speed      float64
	samples    []speedSample
	speeds     []float64
	peak       float64
	client     string
	server     string
	showClient bool
	showServer bool

	done     bool
	quitting bool
}

func newModel(config testConfig, opts options) model {
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	start := time.Now()

	return model{
		targets:    config.Targets,
		bytes:      &atomic.Int64{},
		ctx:        ctx,
		cancel:     cancel,
		start:      start,
		last:       speedSample{time: start},
		client:     config.Client.Label(),
		server:     targetLabel(config.Targets),
		showClient: opts.client,
		showServer: opts.server,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tea.Tick(tickInterval, tickCmd), m.measure)
}

// measure kicks off the parallel downloads that feed our byte counter.
func (m model) measure() tea.Msg {
	for _, target := range m.targets {
		go download(m.ctx, target.URL, m.bytes)
	}
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			m.cancel()
			return m, tea.Quit
		}

	case tickMsg:
		now := time.Now()
		total := m.bytes.Load()
		m.samples = append(m.samples, speedSample{
			bytes:    total - m.last.bytes,
			duration: now.Sub(m.last.time),
			time:     now,
		})
		m.last = speedSample{bytes: total, time: now}
		m.speed = movingMbps(m.samples, time.Second)
		m.speeds = append(m.speeds, m.speed)
		if m.speed > m.peak {
			m.peak = m.speed
		}

		elapsed := now.Sub(m.start)
		if elapsed >= duration {
			m.done = true
			m.cancel()
			return m, tea.Quit
		}

		return m, tea.Tick(tickInterval, tickCmd)
	}

	return m, nil
}

func (m model) View() string {
	if m.quitting {
		return ""
	}

	var s strings.Builder
	// Cap each readout at 999.9 and switch to Gbps beyond that, keeping a fixed
	// width so the unit, sparkline, and peak never shift horizontally.
	speed, unit := scale(m.speed)
	s.WriteString(speedStyle.Render(fmt.Sprintf("%5.1f", speed)))
	s.WriteString(unitStyle.Render(" " + unit))
	s.WriteString(" ")
	s.WriteString(sparkStyle.Render(sparkline(m.speeds, m.peak, sparkWidth)))
	if m.peak > 0 {
		peak, peakUnit := scale(m.peak)
		label := fmt.Sprintf("  peak %.0f", peak)
		// Only label the peak's unit when it differs from the live reading's.
		if peakUnit != unit {
			label += " " + peakUnit
		}
		s.WriteString(peakStyle.Render(label))
	}
	if (m.showClient && m.client != "") || (m.showServer && m.server != "") {
		s.WriteString("\n")
	}
	if m.showClient && m.client != "" {
		s.WriteString(metaStyle.Render("client " + m.client))
		s.WriteString("\n")
	}
	if m.showServer && m.server != "" {
		s.WriteString(metaStyle.Render("server " + m.server))
	}

	style := baseStyle
	if m.done {
		style = style.PaddingBottom(2)
	}
	return style.Render(s.String())
}
