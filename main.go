package main

import (
	"context"
	"fmt"
	"log"
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

const accentColor = "#2EF8BB"

var (
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

type Model struct {
	targets []string

	bytes  *atomic.Int64
	ctx    context.Context
	cancel context.CancelFunc

	start  time.Time
	speed  float64
	speeds []float64
	peak   float64

	done     bool
	quitting bool
}

func NewModel(targets []string) Model {
	ctx, cancel := context.WithTimeout(context.Background(), duration)

	return Model{
		targets: targets,
		bytes:   &atomic.Int64{},
		ctx:     ctx,
		cancel:  cancel,
		start:   time.Now(),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tea.Tick(tickInterval, tickCmd), m.measure)
}

// measure kicks off the parallel downloads that feed our byte counter.
func (m Model) measure() tea.Msg {
	for _, url := range m.targets {
		go download(m.ctx, url, m.bytes)
	}
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			m.cancel()
			return m, tea.Quit
		}

	case tickMsg:
		elapsed := time.Since(m.start)
		m.speed = mbps(m.bytes.Load(), elapsed)
		m.speeds = append(m.speeds, m.speed)
		if m.speed > m.peak {
			m.peak = m.speed
		}

		if elapsed >= duration {
			m.done = true
			m.cancel()
			return m, tea.Quit
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
	s.WriteString(speedStyle.Render(fmt.Sprintf("%.1f", m.speed)))
	s.WriteString(unitStyle.Render(" Mbps"))
	s.WriteString("  ")
	s.WriteString(sparkStyle.Render(sparkline(m.speeds, sparkWidth)))
	if m.peak > 0 {
		s.WriteString(peakStyle.Render(fmt.Sprintf("  peak %.0f", m.peak)))
	}

	style := baseStyle
	if m.done {
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

func main() {
	urls, err := targets(connections)
	if err != nil {
		log.Fatal(err)
	}

	if _, err := tea.NewProgram(NewModel(urls)).Run(); err != nil {
		log.Fatal(err)
	}
}
