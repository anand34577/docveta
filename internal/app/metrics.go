package app

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type metrics struct {
	reg      *prometheus.Registry
	requests *prometheus.HistogramVec
}

func newMetrics(a *App) *metrics {
	reg := prometheus.NewRegistry()
	m := &metrics{
		reg: reg,
		requests: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "docveta_http_request_duration_seconds",
			Help:    "HTTP request latency by route pattern.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		}, []string{"route", "method", "code"}),
	}
	reg.MustRegister(m.requests, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		&dbCollector{a: a})
	return m
}

func (m *metrics) observe(pattern, method string, status int, d time.Duration) {
	if pattern == "" {
		pattern = "unmatched"
	}
	m.requests.WithLabelValues(pattern, method, strconv.Itoa(status)).Observe(d.Seconds())
}

func (m *metrics) handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// dbCollector reports queue and corpus gauges at scrape time.
type dbCollector struct{ a *App }

var (
	descTasks     = prometheus.NewDesc("docveta_processing_tasks", "Processing tasks by status.", []string{"status"}, nil)
	descDocs      = prometheus.NewDesc("docveta_documents", "Documents (not in trash).", nil, nil)
	descWorkers   = prometheus.NewDesc("docveta_workers_online", "Processing workers seen in the last 2 minutes.", nil, nil)
	descFreeBytes = prometheus.NewDesc("docveta_storage_free_bytes", "Free bytes on the blob storage volume.", nil, nil)
)

func (c *dbCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descTasks
	ch <- descDocs
	ch <- descWorkers
	ch <- descFreeBytes
}

func (c *dbCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var queued, leased, docs, workers float64
	err := c.a.Pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM processing_tasks WHERE status='queued'),
		(SELECT count(*) FROM processing_tasks WHERE status='leased'),
		(SELECT count(*) FROM documents WHERE deleted_at IS NULL),
		(SELECT count(*) FROM workers WHERE enabled AND last_seen_at > now() - interval '2 minutes')`).Scan(&queued, &leased, &docs, &workers)
	if err != nil {
		return
	}
	ch <- prometheus.MustNewConstMetric(descTasks, prometheus.GaugeValue, queued, "queued")
	ch <- prometheus.MustNewConstMetric(descTasks, prometheus.GaugeValue, leased, "leased")
	ch <- prometheus.MustNewConstMetric(descDocs, prometheus.GaugeValue, docs)
	ch <- prometheus.MustNewConstMetric(descWorkers, prometheus.GaugeValue, workers)
	ch <- prometheus.MustNewConstMetric(descFreeBytes, prometheus.GaugeValue, float64(c.a.Store.FreeBytes()))
}
