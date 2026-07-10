package main

import (
	"testing"
	"time"
)

func TestMeasurementCapsElapsedTime(t *testing.T) {
	start := time.Now()
	m := newMeasurement()
	m.start(start, time.Second)
	t.Cleanup(m.stop)
	m.bytes.Store(125_000)

	if elapsed := m.sample(start.Add(2 * time.Second)); elapsed != 2*time.Second {
		t.Fatalf("elapsed = %s, want 2s", elapsed)
	}
	if m.speed != 1 {
		t.Fatalf("speed = %f, want 1 Mbps", m.speed)
	}
	if m.peak != m.speed {
		t.Fatalf("peak = %f, want %f", m.peak, m.speed)
	}
}

func TestModelPhases(t *testing.T) {
	model := NewModel()
	updated, cmd := model.Update(targetsMsg{urls: []string{"https://example.com"}})
	model = updated.(Model)
	t.Cleanup(model.download.stop)
	if model.phase != phaseDownloading || cmd == nil {
		t.Fatalf("targets phase = %d, command nil = %t", model.phase, cmd == nil)
	}

	updated, cmd = model.updateTick(model.download.started.Add(duration))
	model = updated.(Model)
	t.Cleanup(model.upload.stop)
	if model.phase != phaseUploading || cmd == nil {
		t.Fatalf("download phase = %d, command nil = %t", model.phase, cmd == nil)
	}

	updated, cmd = model.updateTick(model.upload.started.Add(duration))
	model = updated.(Model)
	if model.phase != phaseMeasuringPing || cmd == nil {
		t.Fatalf("upload phase = %d, command nil = %t", model.phase, cmd == nil)
	}

	const ping = 25 * time.Millisecond
	updated, cmd = model.Update(pingMsg{duration: ping})
	model = updated.(Model)
	if model.phase != phaseDone || model.ping != ping || cmd == nil {
		t.Fatalf("ping phase = %d, ping = %s, command nil = %t", model.phase, model.ping, cmd == nil)
	}
}

func TestScale(t *testing.T) {
	tests := []struct {
		speed float64
		value float64
		unit  string
	}{
		{speed: 999.94, value: 999.94, unit: "Mbps"},
		{speed: 999.95, value: 0.99995, unit: "Gbps"},
		{speed: 2500, value: 2.5, unit: "Gbps"},
	}

	for _, test := range tests {
		value, unit := scale(test.speed)
		if value != test.value || unit != test.unit {
			t.Errorf("scale(%f) = (%f, %q), want (%f, %q)", test.speed, value, unit, test.value, test.unit)
		}
	}
}
