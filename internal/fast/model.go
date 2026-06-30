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

type latencyMsg struct {
	latency time.Duration
	err     error
}

type phase int

const (
	phaseLatency phase = iota
	phaseDownload
	phaseUpload
	phaseDone
)

func tickCmd(t time.Time) tea.Msg {
	return tickMsg(t)
}

type model struct {
	targets []target

	bytes       *atomic.Int64
	uploadBytes *atomic.Int64
	ctx         context.Context
	cancel      context.CancelFunc

	phase      phase
	phaseStart time.Time
	last       speedSample
	speed      float64
	samples    []speedSample
	speeds     []float64
	peak       float64

	uploadLast    speedSample
	uploadSpeed   float64
	uploadSamples []speedSample
	uploadSpeeds  []float64
	uploadPeak    float64

	latency time.Duration
	server  string
	down    bool
	up      bool

	done     bool
	quitting bool
}

func newModel(targets []target, opts options) model {
	ctx, cancel := context.WithCancel(context.Background())
	start := time.Now()

	return model{
		targets:     targets,
		bytes:       &atomic.Int64{},
		uploadBytes: &atomic.Int64{},
		ctx:         ctx,
		cancel:      cancel,
		phase:       phaseLatency,
		phaseStart:  start,
		last:        speedSample{time: start},
		uploadLast:  speedSample{time: start},
		server:      targetLabel(targets),
		down:        opts.down,
		up:          opts.up,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tea.Tick(tickInterval, tickCmd), m.measureLatency)
}

func (m model) measureLatency() tea.Msg {
	latency, err := ping(m.ctx, m.targets)
	return latencyMsg{latency: latency, err: err}
}

func (m model) startDownload() tea.Msg {
	ctx, cancel := context.WithTimeout(m.ctx, duration)
	defer cancel()
	for _, target := range m.targets {
		go download(ctx, target.URL, m.bytes)
	}
	<-ctx.Done()
	return nil
}

func (m model) startUpload() tea.Msg {
	ctx, cancel := context.WithTimeout(m.ctx, duration)
	defer cancel()
	for _, target := range m.targets {
		go upload(ctx, target.URL, m.uploadBytes)
	}
	<-ctx.Done()
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

	case latencyMsg:
		if msg.err == nil {
			m.latency = msg.latency
		}
		now := time.Now()
		if m.down {
			m.phase = phaseDownload
			m.phaseStart = now
			m.last = speedSample{time: now}
			return m, m.startDownload
		}
		m.phase = phaseUpload
		m.phaseStart = now
		m.uploadLast = speedSample{time: now}
		return m, m.startUpload

	case tickMsg:
		now := time.Now()
		switch m.phase {
		case phaseLatency:
			return m, tea.Tick(tickInterval, tickCmd)

		case phaseDownload:
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

			if now.Sub(m.phaseStart) >= duration {
				if !m.up {
					m.phase = phaseDone
					m.done = true
					m.cancel()
					return m, tea.Quit
				}
				m.phase = phaseUpload
				m.phaseStart = now
				m.uploadLast = speedSample{time: now}
				return m, tea.Batch(m.startUpload, tea.Tick(tickInterval, tickCmd))
			}

		case phaseUpload:
			total := m.uploadBytes.Load()
			m.uploadSamples = append(m.uploadSamples, speedSample{
				bytes:    total - m.uploadLast.bytes,
				duration: now.Sub(m.uploadLast.time),
				time:     now,
			})
			m.uploadLast = speedSample{bytes: total, time: now}
			m.uploadSpeed = movingMbps(m.uploadSamples, time.Second)
			m.uploadSpeeds = append(m.uploadSpeeds, m.uploadSpeed)
			if m.uploadSpeed > m.uploadPeak {
				m.uploadPeak = m.uploadSpeed
			}

			if now.Sub(m.phaseStart) >= duration {
				m.phase = phaseDone
				m.done = true
				m.cancel()
				return m, tea.Quit
			}
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
	if m.latency > 0 {
		s.WriteString(metaStyle.Render(fmt.Sprintf("ping %.0f ms", float64(m.latency)/float64(time.Millisecond))))
	} else {
		s.WriteString(metaStyle.Render("ping -- ms"))
	}
	s.WriteString("\n")

	if m.down {
		s.WriteString(speedLine("down", m.speed, m.speeds, m.peak, true))
	}
	if m.down && m.up {
		s.WriteString("\n")
	}
	if m.up {
		s.WriteString(speedLine("up", m.uploadSpeed, m.uploadSpeeds, m.uploadPeak, m.phase == phaseUpload || m.phase == phaseDone))
	}

	s.WriteString("\n")
	s.WriteString(metaStyle.Render("server " + m.server))

	style := baseStyle
	if m.done {
		style = style.PaddingBottom(2)
	}
	return style.Render(s.String())
}

func speedLine(label string, speed float64, values []float64, peak float64, show bool) string {
	var s strings.Builder
	s.WriteString(metaStyle.Render(fmt.Sprintf("%-4s ", label)))
	if !show {
		s.WriteString(speedStyle.Render("   --"))
		s.WriteString(unitStyle.Render(" Mbps"))
		s.WriteString(" ")
		s.WriteString(sparkStyle.Render(strings.Repeat(" ", sparkWidth)))
		return s.String()
	}

	// Cap each readout at 999.9 and switch to Gbps beyond that, keeping a fixed
	// width so the unit, sparkline, and peak never shift horizontally.
	scaledSpeed, unit := scale(speed)
	s.WriteString(speedStyle.Render(fmt.Sprintf("%5.1f", scaledSpeed)))
	s.WriteString(unitStyle.Render(" " + unit))
	s.WriteString(" ")
	s.WriteString(sparkStyle.Render(sparkline(values, sparkWidth)))
	if peak > 0 {
		scaledPeak, peakUnit := scale(peak)
		peakLabel := fmt.Sprintf("  peak %.0f", scaledPeak)
		// Only label the peak's unit when it differs from the live reading's.
		if peakUnit != unit {
			peakLabel += " " + peakUnit
		}
		s.WriteString(peakStyle.Render(peakLabel))
	}
	return s.String()
}
