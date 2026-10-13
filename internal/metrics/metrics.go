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

// FlowsBothInterfacesLinked counts flows whose input and output interfaces
// both map to configured links (transit between two of your links). Such a
// flow is credited to one link only per record; a high rate together with
// ingress+egress monitoring on the same uplinks means the exporter sends each
// packet twice, which only flowDirection (field 61) can disambiguate.
var FlowsBothInterfacesLinked = promauto.NewCounter(prometheus.CounterOpts{
	Name: "asstats_flows_both_interfaces_linked_total",
	Help: "Flows whose input and output interfaces both match configured links.",
})

// FlowSetsNoTemplate counts v9/IPFIX data sets discarded because their
// template has not been received from that exporter. It is counted in sets,
// not flows: without the template the record count cannot be known. Expected
// for one template refresh period after a collector restart; a rate that
// persists means the exporter's templates never reach the collector.
var FlowSetsNoTemplate = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "asstats_flowsets_dropped_no_template_total",
	Help: "v9/IPFIX data sets dropped because their template is unknown (sets, not flows).",
}, []string{"router"})

// SamplingRate is the 1-in-N sampling rate applied to the last packet decoded
// from each exporter. A change here changes every byte count derived from that
// router, so it is worth graphing next to traffic.
var SamplingRate = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "asstats_sampling_rate",
	Help: "Sampling rate (1-in-N) applied to the most recent flows of each exporter.",
}, []string{"router"})

// PipelineQueueLength / PipelineQueueCapacity expose the fill level of the
// collector's internal channels, sampled every few seconds. A queue pinned at
// capacity locates the bottleneck: packets → decoders, flows → enricher,
// enriched → batch writer / ClickHouse.
var PipelineQueueLength = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "asstats_pipeline_queue_length",
	Help: "Items currently queued in a collector pipeline channel.",
}, []string{"queue"})

var PipelineQueueCapacity = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "asstats_pipeline_queue_capacity",
	Help: "Capacity of a collector pipeline channel.",
}, []string{"queue"})

// UDPReceiveBuffer is the socket receive buffer the kernel actually granted
// (on Linux, half the SO_RCVBUF readback). Linux silently caps the request at
// net.core.rmem_max.
var UDPReceiveBuffer = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "asstats_udp_receive_buffer_bytes",
	Help: "UDP socket receive buffer size granted by the kernel.",
}, []string{"listener"})

// UDPSocketDrops is the kernel's cumulative drop count for a listener socket
// (Linux only): datagrams lost because the receive buffer was full, before
// the collector could read them. Use increase() on it like a counter; it
// resets when the socket is reopened.
var UDPSocketDrops = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "asstats_udp_socket_drops",
	Help: "Datagrams dropped by the kernel on a listener socket since it was opened (Linux).",
}, []string{"listener"})

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
