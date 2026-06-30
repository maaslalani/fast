package fast

import "time"

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
