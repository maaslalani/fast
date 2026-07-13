package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	connections    = 5
	duration       = 10 * time.Second
	latencyTimeout = 5 * time.Second
	warmupLead     = 2 * time.Second
	tickInterval   = time.Second / 10
)

type phase uint8

const (
	phaseLoading phase = iota
	phaseDownloading
	phaseUploading
	phaseMeasuringPing
	phaseDone
)

type tickMsg time.Time

func tickCmd(t time.Time) tea.Msg {
	return tickMsg(t)
}

func nextTick() tea.Cmd {
	return tea.Tick(tickInterval, tickCmd)
}

type measurement struct {
	bytes    *atomic.Int64
	ctx      context.Context
	cancel   context.CancelFunc
	started  time.Time
	duration time.Duration
	speed    float64
	samples  []float64
	peak     float64
}

func newMeasurement() measurement {
	return measurement{bytes: &atomic.Int64{}}
}

func (m *measurement) start(ctx context.Context, now time.Time, duration time.Duration) {
	m.stop()
	m.bytes.Store(0)
	m.started = now
	m.duration = duration
	m.speed = 0
	m.samples = make([]float64, 0, int(duration/tickInterval)+1)
	m.peak = 0
	m.ctx, m.cancel = context.WithDeadline(ctx, now.Add(duration))
}

func (m *measurement) sample(now time.Time) time.Duration {
	elapsed := now.Sub(m.started)
	measured := min(max(elapsed, 0), m.duration)
	m.speed = mbps(m.bytes.Load(), measured)
	m.samples = append(m.samples, m.speed)
	m.peak = max(m.peak, m.speed)
	return elapsed
}

func (m *measurement) stop() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

type transferFunc func(context.Context, string, *atomic.Int64)

type dependencies struct {
	targets  func(context.Context, int) ([]string, error)
	download transferFunc
	upload   transferFunc
	warm     func(context.Context, string)
	latency  func(context.Context, string) (time.Duration, error)
}

func defaultDependencies() dependencies {
	service := newFastService()
	return dependencies{
		targets:  service.targets,
		download: service.download,
		upload:   service.upload,
		warm:     service.warm,
		latency:  service.latency,
	}
}

type Model struct {
	deps   dependencies
	ctx    context.Context
	cancel context.CancelFunc
	now    func() time.Time

	targets []string
	err     error
	phase   phase

	download measurement
	upload   measurement

	ping         time.Duration
	uploadWarmed bool
	quitting     bool
}

func NewModel() Model {
	return newModel(defaultDependencies())
}

func newModel(deps dependencies) Model {
	ctx, cancel := context.WithCancel(context.Background())
	return Model{
		deps:     deps,
		ctx:      ctx,
		cancel:   cancel,
		now:      time.Now,
		phase:    phaseLoading,
		download: newMeasurement(),
		upload:   newMeasurement(),
	}
}

func (m *Model) stop() {
	m.download.stop()
	m.upload.stop()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

func (m Model) Init() tea.Cmd {
	return m.fetchTargets
}

type targetsMsg struct {
	urls []string
	err  error
}

func (m Model) fetchTargets() tea.Msg {
	ctx, cancel := context.WithTimeout(m.ctx, metadataTimeout)
	defer cancel()
	urls, err := m.deps.targets(ctx, connections)
	return targetsMsg{urls: urls, err: err}
}

func startTransfers(ctx context.Context, targets []string, total *atomic.Int64, transfer transferFunc) tea.Msg {
	var wg sync.WaitGroup
	for _, target := range targets {
		wg.Add(1)
		go func(target string) {
			defer wg.Done()
			transfer(ctx, target, total)
		}(target)
	}
	wg.Wait()
	return nil
}

func (m Model) measureDownload() tea.Msg {
	return startTransfers(m.download.ctx, m.targets, m.download.bytes, m.deps.download)
}

func (m Model) measureUpload() tea.Msg {
	return startTransfers(m.upload.ctx, m.targets, m.upload.bytes, m.deps.upload)
}

func (m Model) warmUpload() tea.Msg {
	ctx, cancel := context.WithDeadline(m.ctx, m.download.started.Add(duration))
	defer cancel()

	var wg sync.WaitGroup
	for _, target := range m.targets {
		wg.Add(1)
		go func(target string) {
			defer wg.Done()
			m.deps.warm(ctx, target)
		}(target)
	}
	wg.Wait()
	return nil
}

type pingMsg struct {
	duration time.Duration
	err      error
}

func (m Model) measurePing() tea.Msg {
	if len(m.targets) == 0 {
		return pingMsg{err: errors.New("no targets")}
	}
	ctx, cancel := context.WithTimeout(m.ctx, latencyTimeout)
	defer cancel()
	d, err := m.deps.latency(ctx, m.targets[0])
	return pingMsg{duration: d, err: err}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.quitting = true
			m.stop()
			return m, tea.Quit
		}

	case targetsMsg:
		if msg.err != nil {
			m.err = msg.err
			m.quitting = true
			m.stop()
			return m, tea.Quit
		}
		if len(msg.urls) == 0 {
			m.err = errors.New("no targets")
			m.quitting = true
			m.stop()
			return m, tea.Quit
		}

		m.targets = msg.urls
		m.phase = phaseDownloading
		m.download.start(m.ctx, m.now(), duration)
		return m, tea.Batch(nextTick(), m.measureDownload)

	case pingMsg:
		if msg.err == nil {
			m.ping = msg.duration
		}
		m.phase = phaseDone
		m.stop()
		return m, tea.Quit

	case tickMsg:
		return m.updateTick(time.Time(msg))
	}

	return m, nil
}

func (m Model) updateTick(now time.Time) (tea.Model, tea.Cmd) {
	switch m.phase {
	case phaseDownloading:
		elapsed := m.download.sample(now)
		if elapsed >= duration {
			m.download.stop()
			m.phase = phaseUploading
			m.upload.start(m.ctx, now, duration)
			return m, tea.Batch(nextTick(), m.measureUpload)
		}
		if !m.uploadWarmed && elapsed >= duration-warmupLead {
			m.uploadWarmed = true
			return m, tea.Batch(nextTick(), m.warmUpload)
		}
		return m, nextTick()

	case phaseUploading:
		if m.upload.sample(now) >= duration {
			m.upload.stop()
			m.phase = phaseMeasuringPing
			return m, m.measurePing
		}
		return m, nextTick()
	}
	return m, nil
}

func mbps(bytes int64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(bytes) * 8 / d.Seconds() / 1e6
}
