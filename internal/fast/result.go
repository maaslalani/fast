package fast

import (
	"context"
	"fmt"
	"io"
	"math"
	"sync/atomic"
	"time"
)

type result struct {
	Client   *clientInfo  `json:"client,omitempty"`
	Server   string       `json:"server"`
	Ping     pingResult   `json:"ping"`
	Download *speedResult `json:"download,omitempty"`
	Upload   *speedResult `json:"upload,omitempty"`
}

type pingResult struct {
	UnloadedMS *float64 `json:"unloaded_ms,omitempty"`
	LoadedMS   *float64 `json:"loaded_ms,omitempty"`
}

type speedResult struct {
	Mbps     float64 `json:"mbps"`
	PeakMbps float64 `json:"peak_mbps"`
}

type transferResult struct {
	speed         float64
	peak          float64
	loadedPing    time.Duration
	hasLoadedPing bool
}

func runTest(config testConfig, opts options) (result, error) {
	if len(config.Targets) == 0 {
		return result{}, fmt.Errorf("no speed test targets")
	}

	ctx := context.Background()
	output := result{Server: targetLabel(config.Targets)}
	if config.Client.Label() != "" {
		output.Client = &config.Client
	}
	if latency, err := ping(ctx, config.Targets); err == nil {
		output.Ping.UnloadedMS = roundedLatency(latency)
	}

	if opts.down {
		transfer := measureTransfer(ctx, config.Targets, opts.duration, download)
		output.Download = newSpeedResult(transfer)
		if transfer.hasLoadedPing {
			output.Ping.LoadedMS = roundedLatency(transfer.loadedPing)
		}
	}
	if opts.up {
		transfer := measureTransfer(ctx, config.Targets, opts.duration, upload)
		output.Upload = newSpeedResult(transfer)
		if transfer.hasLoadedPing && (output.Ping.LoadedMS == nil || transfer.loadedPing > durationFromMS(*output.Ping.LoadedMS)) {
			output.Ping.LoadedMS = roundedLatency(transfer.loadedPing)
		}
	}

	return output, nil
}

func measureTransfer(ctx context.Context, targets []target, duration time.Duration, transfer func(context.Context, string, *atomic.Int64)) transferResult {
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	total := &atomic.Int64{}
	for _, target := range targets {
		go transfer(ctx, target.URL, total)
	}
	latency := make(chan time.Duration, 1)
	go func() {
		loadedLatency, err := ping(ctx, targets)
		if err == nil {
			latency <- loadedLatency
		}
	}()

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	last := speedSample{time: time.Now()}
	samples := []speedSample{}
	result := transferResult{}
	for {
		select {
		case loadedLatency := <-latency:
			result.loadedPing = loadedLatency
			result.hasLoadedPing = true
		case now := <-ticker.C:
			totalBytes := total.Load()
			samples = append(samples, speedSample{
				bytes:    totalBytes - last.bytes,
				duration: now.Sub(last.time),
				time:     now,
			})
			last = speedSample{bytes: totalBytes, time: now}
			result.speed = movingMbps(samples, time.Second)
			if result.speed > result.peak {
				result.peak = result.speed
			}
		case <-ctx.Done():
			return result
		}
	}
}

func newSpeedResult(transfer transferResult) *speedResult {
	return &speedResult{
		Mbps:     round(transfer.speed),
		PeakMbps: round(transfer.peak),
	}
}

func printResult(w io.Writer, result result) error {
	if _, err := fmt.Fprintf(w, "ping unloaded %s ms  loaded %s ms\n", floatLabel(result.Ping.UnloadedMS), floatLabel(result.Ping.LoadedMS)); err != nil {
		return err
	}
	if result.Download != nil {
		if _, err := fmt.Fprintln(w, speedResultLine("down", *result.Download)); err != nil {
			return err
		}
	}
	if result.Upload != nil {
		if _, err := fmt.Fprintln(w, speedResultLine("up", *result.Upload)); err != nil {
			return err
		}
	}
	if result.Client != nil {
		if _, err := fmt.Fprintf(w, "client %s\n", result.Client.Label()); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "server %s\n", result.Server)
	return err
}

func speedResultLine(label string, result speedResult) string {
	speed, unit := scale(result.Mbps)
	peak, peakUnit := scale(result.PeakMbps)
	line := fmt.Sprintf("%-4s %.1f %s", label, speed, unit)
	if result.PeakMbps > 0 {
		line += fmt.Sprintf(" (peak %.0f", peak)
		if peakUnit != unit {
			line += " " + peakUnit
		}
		line += ")"
	}
	return line
}

func floatLabel(value *float64) string {
	if value == nil {
		return "--"
	}
	return fmt.Sprintf("%.1f", *value)
}

func roundedLatency(latency time.Duration) *float64 {
	rounded := round(float64(latency) / float64(time.Millisecond))
	return &rounded
}

func durationFromMS(value float64) time.Duration {
	return time.Duration(value * float64(time.Millisecond))
}

func round(value float64) float64 {
	return math.Round(value*10) / 10
}
