// Package metrics defines global Prometheus metrics for the collector and API.
//
// Both binaries import this package and register the metrics they need. The
// collector instruments the flow pipeline (decode, enrich, write, alert engine)
// and the API instruments HTTP handlers and ClickHouse queries.
//
// All metrics use the "asstats_" prefix so they're easy to find in a
// multi-service Prometheus.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ── Collector: flow pipeline ────────────────────────────────────────────────

var FlowsReceived = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "asstats_flows_received_total",
	Help: "Total number of flows decoded from routers.",
}, []string{"protocol"}) // "netflow5", "netflow9", "ipfix", "sflow"

var FlowsWritten = promauto.NewCounter(prometheus.CounterOpts{
	Name: "asstats_flows_written_total",
	Help: "Total number of flows written to ClickHouse.",
})

var BatchWriteDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "asstats_batch_write_duration_seconds",
	Help:    "Time spent flushing a single batch to ClickHouse.",
	Buckets: prometheus.ExponentialBuckets(0.01, 2, 10), // 10ms → 10s
})

var BatchSize = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "asstats_batch_size",
	Help:    "Number of flows per batch write.",
	Buckets: prometheus.ExponentialBuckets(100, 2, 12), // 100 → 400k
})

var DecodeErrors = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "asstats_decode_errors_total",
	Help: "Total number of flow decode errors.",
}, []string{"protocol"})

// BatchWriteErrors and FlowsDropped make a persistent ingestion failure visible.
// Without them a ClickHouse that refuses every INSERT (disk full, credentials,
// schema drift) is silent: the success counters simply stop advancing, which is
// indistinguishable from "no traffic". Alert on
// rate(asstats_batch_write_errors_total[5m]) > 0.
var BatchWriteErrors = promauto.NewCounter(prometheus.CounterOpts{
	Name: "asstats_batch_write_errors_total",
	Help: "Total number of batch writes to ClickHouse that failed.",
})

var FlowsDropped = promauto.NewCounter(prometheus.CounterOpts{
	Name: "asstats_flows_dropped_total",
	Help: "Total number of decoded flows discarded because their batch write failed.",
})

// TimestampsClamped counts flows whose exporter-supplied timestamp was outside
// the accepted window and rewritten to receive time. A steadily rising value
// means a router has a broken clock (or packets are being spoofed) — both
// distort partitioning and retention on flows_raw.
var TimestampsClamped = promauto.NewCounter(prometheus.CounterOpts{
	Name: "asstats_timestamps_clamped_total",
	Help: "Flows whose out-of-range exporter timestamp was replaced with receive time.",
})

// ── Collector: alert engine ─────────────────────────────────────────────────

var AlertEvaluations = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "asstats_alert_evaluations_total",
	Help: "Number of rule evaluations by the alert engine.",
}, []string{"rule_type"})

var AlertsFired = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "asstats_alerts_fired_total",
	Help: "Number of new alerts inserted (not heartbeats).",
}, []string{"severity"})

var AlertEngineCycleDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "asstats_alert_engine_cycle_duration_seconds",
	Help:    "Duration of one full alert engine evaluation cycle.",
	Buckets: prometheus.ExponentialBuckets(0.05, 2, 8), // 50ms → 12s
})

var CooldownMapSize = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "asstats_cooldown_map_size",
	Help: "Current number of entries in the alert engine cooldown map.",
})

// ── API: HTTP ───────────────────────────────────────────────────────────────

var HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "asstats_http_requests_total",
	Help: "Total HTTP requests by method, route pattern, and status code.",
}, []string{"method", "route", "code"})

var HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "asstats_http_request_duration_seconds",
	Help:    "HTTP request latency by method and route pattern.",
	Buckets: prometheus.DefBuckets,
}, []string{"method", "route"})
