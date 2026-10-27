package bgp

import (
	"time"

	"github.com/nextmap-io/as-stats/internal/metrics"
)

func observeCommand(action string, err error, d time.Duration) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	metrics.BGPCommands.WithLabelValues(action, result).Inc()
	metrics.BGPCommandDuration.WithLabelValues(action).Observe(d.Seconds())
}
