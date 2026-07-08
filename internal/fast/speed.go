package fast

import "time"

type speedSample struct {
	bytes    int64
	duration time.Duration
	time     time.Time
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

func movingMbps(samples []speedSample, window time.Duration) float64 {
	if len(samples) < 2 {
		return 0
	}

	end := samples[len(samples)-1].time
	start := end.Add(-window)
	bytes := int64(0)
	duration := time.Duration(0)
	for i := len(samples) - 1; i >= 0; i-- {
		if samples[i].time.Before(start) {
			break
		}
		bytes += samples[i].bytes
		duration += samples[i].duration
	}

	return mbps(bytes, duration)
}
