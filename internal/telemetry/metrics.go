package telemetry

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
)

type Metrics struct {
	RequestsTotal    atomic.Int64
	CacheHits        atomic.Int64
	CacheMisses      atomic.Int64
	TotalTokens      atomic.Int64
	TotalCostMicros  atomic.Int64 // cost in microdollars for atomic ops

	routingDecisions sync.Map // tier -> count
	providerRequests sync.Map // provider -> count
	latencyBuckets   sync.Map // bucket -> count
}

func NewMetrics() *Metrics {
	return &Metrics{}
}

func (m *Metrics) RecordRequest(provider, model, tier string, tokens int, costDollars float64, latencyMs int64, cacheHit bool) {
	m.RequestsTotal.Add(1)
	m.TotalTokens.Add(int64(tokens))
	m.TotalCostMicros.Add(int64(costDollars * 1_000_000))

	if cacheHit {
		m.CacheHits.Add(1)
	} else {
		m.CacheMisses.Add(1)
	}

	// Track routing decisions by tier
	if v, ok := m.routingDecisions.Load(tier); ok {
		counter := v.(*atomic.Int64)
		counter.Add(1)
	} else {
		counter := &atomic.Int64{}
		counter.Add(1)
		m.routingDecisions.Store(tier, counter)
	}

	// Track provider requests
	key := provider + "/" + model
	if v, ok := m.providerRequests.Load(key); ok {
		counter := v.(*atomic.Int64)
		counter.Add(1)
	} else {
		counter := &atomic.Int64{}
		counter.Add(1)
		m.providerRequests.Store(key, counter)
	}

	// Latency buckets
	bucket := latencyBucket(latencyMs)
	if v, ok := m.latencyBuckets.Load(bucket); ok {
		counter := v.(*atomic.Int64)
		counter.Add(1)
	} else {
		counter := &atomic.Int64{}
		counter.Add(1)
		m.latencyBuckets.Store(bucket, counter)
	}
}

func latencyBucket(ms int64) string {
	switch {
	case ms < 100:
		return "lt100ms"
	case ms < 500:
		return "lt500ms"
	case ms < 1000:
		return "lt1s"
	case ms < 5000:
		return "lt5s"
	case ms < 10000:
		return "lt10s"
	default:
		return "gt10s"
	}
}

func (m *Metrics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")

		totalCost := float64(m.TotalCostMicros.Load()) / 1_000_000.0

		fmt.Fprintf(w, "# HELP nexus_requests_total Total number of requests processed\n")
		fmt.Fprintf(w, "nexus_requests_total %d\n", m.RequestsTotal.Load())
		fmt.Fprintf(w, "# HELP nexus_cache_hits_total Total cache hits\n")
		fmt.Fprintf(w, "nexus_cache_hits_total %d\n", m.CacheHits.Load())
		fmt.Fprintf(w, "# HELP nexus_cache_misses_total Total cache misses\n")
		fmt.Fprintf(w, "nexus_cache_misses_total %d\n", m.CacheMisses.Load())
		fmt.Fprintf(w, "# HELP nexus_tokens_total Total tokens processed\n")
		fmt.Fprintf(w, "nexus_tokens_total %d\n", m.TotalTokens.Load())
		fmt.Fprintf(w, "# HELP nexus_cost_dollars_total Total cost in dollars\n")
		fmt.Fprintf(w, "nexus_cost_dollars_total %.6f\n", totalCost)

		fmt.Fprintf(w, "# HELP nexus_routing_decisions_total Routing decisions by tier\n")
		m.routingDecisions.Range(func(key, value any) bool {
			counter := value.(*atomic.Int64)
			fmt.Fprintf(w, "nexus_routing_decisions_total{tier=%q} %d\n", key.(string), counter.Load())
			return true
		})

		fmt.Fprintf(w, "# HELP nexus_provider_requests_total Requests by provider/model\n")
		m.providerRequests.Range(func(key, value any) bool {
			counter := value.(*atomic.Int64)
			fmt.Fprintf(w, "nexus_provider_requests_total{provider_model=%q} %d\n", key.(string), counter.Load())
			return true
		})

		fmt.Fprintf(w, "# HELP nexus_latency_bucket Request count by latency bucket\n")
		m.latencyBuckets.Range(func(key, value any) bool {
			counter := value.(*atomic.Int64)
			fmt.Fprintf(w, "nexus_latency_bucket{bucket=%q} %d\n", key.(string), counter.Load())
			return true
		})
	}
}
