package telemetry

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Atomic float64 helpers (CAS loop over int64-stored float64 bits)
// ---------------------------------------------------------------------------

func atomicAddFloat64(addr *atomic.Int64, val float64) {
	for {
		old := addr.Load()
		newVal := math.Float64frombits(uint64(old)) + val
		if addr.CompareAndSwap(old, int64(math.Float64bits(newVal))) {
			return
		}
	}
}

func atomicLoadFloat64(addr *atomic.Int64) float64 {
	return math.Float64frombits(uint64(addr.Load()))
}

// ---------------------------------------------------------------------------
// sync.Map counter helpers
// ---------------------------------------------------------------------------

func addToMap(m *sync.Map, key string, delta int64) {
	v, _ := m.LoadOrStore(key, &atomic.Int64{})
	v.(*atomic.Int64).Add(delta)
}

func setInMap(m *sync.Map, key string, val int64) {
	v, _ := m.LoadOrStore(key, &atomic.Int64{})
	v.(*atomic.Int64).Store(val)
}

// ---------------------------------------------------------------------------
// Histogram (lock-free, cumulative buckets)
// ---------------------------------------------------------------------------

type histogram struct {
	buckets []atomic.Int64 // cumulative: index i counts values <= boundaries[i]; last is +Inf
	sum     atomic.Int64   // float64 bits
	count   atomic.Int64
}

type histogramVec struct {
	boundaries []float64
	instances  sync.Map // label-key string → *histogram
}

func (hv *histogramVec) getOrCreate(labels string) *histogram {
	if v, ok := hv.instances.Load(labels); ok {
		return v.(*histogram)
	}
	h := &histogram{
		buckets: make([]atomic.Int64, len(hv.boundaries)+1),
	}
	actual, loaded := hv.instances.LoadOrStore(labels, h)
	if loaded {
		return actual.(*histogram)
	}
	return h
}

func (hv *histogramVec) observe(labels string, seconds float64) {
	h := hv.getOrCreate(labels)
	for i, b := range hv.boundaries {
		if seconds <= b {
			h.buckets[i].Add(1)
		}
	}
	h.buckets[len(hv.boundaries)].Add(1) // +Inf
	atomicAddFloat64(&h.sum, seconds)
	h.count.Add(1)
}

// ---------------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------------

// Metrics collects and exposes Prometheus-format metrics for the Nexus gateway.
type Metrics struct {
	startTime time.Time

	// Public for backward compatibility (server.go reads RequestsTotal.Load()).
	RequestsTotal atomic.Int64

	// Labeled counters — sync.Map[string]*atomic.Int64
	requestsByLabel   sync.Map // provider,model,tier,status
	cacheHitsByLayer  sync.Map // layer
	cacheMissesTotal  atomic.Int64
	tokensByDirection sync.Map // direction
	costByLabel       sync.Map // provider,tier  (value = microdollars)
	securityBlocks    sync.Map // reason
	cacheEvictions    sync.Map // layer
	synonymPromotions atomic.Int64

	// Histograms
	requestDuration   histogramVec
	cacheLookup       histogramVec
	embeddingDuration histogramVec

	// Gauges
	cacheEntries   sync.Map // layer
	activeRequests atomic.Int64

	// Compression + cascade + eval metrics
	compressionTokensSaved atomic.Int64
	cascadeAttempts        sync.Map // result (accepted|escalated)
	evalConfidence         histogramVec
}

// NewMetrics creates an initialised Metrics collector.
func NewMetrics() *Metrics {
	return &Metrics{
		startTime: time.Now(),
		requestDuration: histogramVec{
			boundaries: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0},
		},
		cacheLookup: histogramVec{
			boundaries: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1},
		},
		embeddingDuration: histogramVec{
			boundaries: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1.0},
		},
		evalConfidence: histogramVec{
			boundaries: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
		},
	}
}

// ---------------------------------------------------------------------------
// Recording methods
// ---------------------------------------------------------------------------

// RecordRequest records a completed request.
// Signature is kept for backward compatibility with server.go.
func (m *Metrics) RecordRequest(provider, model, tier string, tokens int, costDollars float64, latencyMs int64, cacheHit bool) {
	m.RequestsTotal.Add(1)

	status := "ok"
	if cacheHit {
		status = "cache_hit"
		m.RecordCacheHit(model) // model carries the cache layer on hits
	} else {
		m.RecordCacheMiss()
	}

	key := fmt.Sprintf(`provider="%s",model="%s",tier="%s",status="%s"`, provider, model, tier, status)
	addToMap(&m.requestsByLabel, key, 1)

	if tokens > 0 {
		addToMap(&m.tokensByDirection, `direction="total"`, int64(tokens))
	}

	if costDollars > 0 {
		costKey := fmt.Sprintf(`provider="%s",tier="%s"`, provider, tier)
		addToMap(&m.costByLabel, costKey, int64(costDollars*1_000_000))
	}

	seconds := float64(latencyMs) / 1000.0
	m.requestDuration.observe(fmt.Sprintf(`tier="%s"`, tier), seconds)
}

// RecordCacheHit records a cache hit for the given layer (l1, bm25, semantic).
func (m *Metrics) RecordCacheHit(layer string) {
	addToMap(&m.cacheHitsByLayer, fmt.Sprintf(`layer="%s"`, layer), 1)
}

// RecordCacheMiss records a cache miss.
func (m *Metrics) RecordCacheMiss() {
	m.cacheMissesTotal.Add(1)
}

// RecordCacheEviction records a cache eviction for the given layer.
func (m *Metrics) RecordCacheEviction(layer string) {
	addToMap(&m.cacheEvictions, fmt.Sprintf(`layer="%s"`, layer), 1)
}

// RecordSecurityBlock records a security block event.
func (m *Metrics) RecordSecurityBlock(reason string) {
	addToMap(&m.securityBlocks, fmt.Sprintf(`reason="%s"`, reason), 1)
}

// RecordSynonymPromotion records a synonym learning event.
func (m *Metrics) RecordSynonymPromotion() {
	m.synonymPromotions.Add(1)
}

// RecordDuration records an observation in the named histogram.
// For "request": tier is a tier label. For "cache_lookup": tier is a layer label.
func (m *Metrics) RecordDuration(name string, tier string, seconds float64) {
	switch name {
	case "request":
		labels := ""
		if tier != "" {
			labels = fmt.Sprintf(`tier="%s"`, tier)
		}
		m.requestDuration.observe(labels, seconds)
	case "cache_lookup":
		labels := ""
		if tier != "" {
			labels = fmt.Sprintf(`layer="%s"`, tier)
		}
		m.cacheLookup.observe(labels, seconds)
	case "embedding":
		m.embeddingDuration.observe("", seconds)
	}
}

// RecordCacheLookup records a cache lookup duration for the given layer.
func (m *Metrics) RecordCacheLookup(layer string, seconds float64) {
	m.cacheLookup.observe(fmt.Sprintf(`layer="%s"`, layer), seconds)
}

// RecordEmbedding records an embedding generation duration.
func (m *Metrics) RecordEmbedding(seconds float64) {
	m.embeddingDuration.observe("", seconds)
}

// SetCacheEntries sets the current cache entry count for a layer.
func (m *Metrics) SetCacheEntries(layer string, count int64) {
	setInMap(&m.cacheEntries, fmt.Sprintf(`layer="%s"`, layer), count)
}

// IncActiveRequests increments the in-flight request gauge.
func (m *Metrics) IncActiveRequests() { m.activeRequests.Add(1) }

// DecActiveRequests decrements the in-flight request gauge.
func (m *Metrics) DecActiveRequests() { m.activeRequests.Add(-1) }

// RecordCompressionSaved records tokens saved by compression.
func (m *Metrics) RecordCompressionSaved(tokensSaved int) {
	m.compressionTokensSaved.Add(int64(tokensSaved))
}

// RecordCascadeAttempt records a cascade attempt with the given result (accepted or escalated).
func (m *Metrics) RecordCascadeAttempt(result string) {
	addToMap(&m.cascadeAttempts, fmt.Sprintf(`result="%s"`, result), 1)
}

// RecordEvalConfidence records a confidence score observation.
func (m *Metrics) RecordEvalConfidence(score float64) {
	m.evalConfidence.observe("", score)
}

// ---------------------------------------------------------------------------
// Prometheus exposition helpers
// ---------------------------------------------------------------------------

type mapEntry struct {
	key   string
	value *atomic.Int64
}

func sortedEntries(m *sync.Map) []mapEntry {
	var entries []mapEntry
	m.Range(func(k, v any) bool {
		entries = append(entries, mapEntry{key: k.(string), value: v.(*atomic.Int64)})
		return true
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	return entries
}

func sumMap(m *sync.Map) int64 {
	var total int64
	m.Range(func(_, v any) bool {
		total += v.(*atomic.Int64).Load()
		return true
	})
	return total
}

func fmtFloat(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// writeLabeledInt writes a metric family whose values are int64 counters.
func writeLabeledInt(b *strings.Builder, name, help, typ string, m *sync.Map) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
	for _, e := range sortedEntries(m) {
		fmt.Fprintf(b, "%s{%s} %d\n", name, e.key, e.value.Load())
	}
	b.WriteByte('\n')
}

// writeLabeledFloat writes a metric family whose raw int64 values are divided by divisor.
func writeLabeledFloat(b *strings.Builder, name, help, typ string, m *sync.Map, divisor float64) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
	for _, e := range sortedEntries(m) {
		fmt.Fprintf(b, "%s{%s} %s\n", name, e.key, fmtFloat(float64(e.value.Load())/divisor))
	}
	b.WriteByte('\n')
}

func writeSimpleInt(b *strings.Builder, name, help, typ string, val int64) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n%s %d\n\n", name, help, name, typ, name, val)
}

func writeSimpleFloat(b *strings.Builder, name, help, typ string, val float64) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n%s %s\n\n", name, help, name, typ, name, fmtFloat(val))
}

func writeHistogramFamily(b *strings.Builder, name, help string, hv *histogramVec) {
	var keys []string
	hv.instances.Range(func(k, _ any) bool {
		keys = append(keys, k.(string))
		return true
	})
	if len(keys) == 0 {
		return
	}
	sort.Strings(keys)

	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s histogram\n", name, help, name)
	for _, labelStr := range keys {
		v, _ := hv.instances.Load(labelStr)
		h := v.(*histogram)

		for i, bound := range hv.boundaries {
			le := fmtFloat(bound)
			if labelStr != "" {
				fmt.Fprintf(b, "%s_bucket{%s,le=\"%s\"} %d\n", name, labelStr, le, h.buckets[i].Load())
			} else {
				fmt.Fprintf(b, "%s_bucket{le=\"%s\"} %d\n", name, le, h.buckets[i].Load())
			}
		}
		// +Inf
		if labelStr != "" {
			fmt.Fprintf(b, "%s_bucket{%s,le=\"+Inf\"} %d\n", name, labelStr, h.buckets[len(hv.boundaries)].Load())
			fmt.Fprintf(b, "%s_sum{%s} %s\n", name, labelStr, fmtFloat(atomicLoadFloat64(&h.sum)))
			fmt.Fprintf(b, "%s_count{%s} %d\n", name, labelStr, h.count.Load())
		} else {
			fmt.Fprintf(b, "%s_bucket{le=\"+Inf\"} %d\n", name, h.buckets[len(hv.boundaries)].Load())
			fmt.Fprintf(b, "%s_sum %s\n", name, fmtFloat(atomicLoadFloat64(&h.sum)))
			fmt.Fprintf(b, "%s_count %d\n", name, h.count.Load())
		}
	}
	b.WriteByte('\n')
}

// ---------------------------------------------------------------------------
// Handler — Prometheus exposition endpoint
// ---------------------------------------------------------------------------

// Handler returns an HTTP handler that serves metrics in Prometheus text format.
func (m *Metrics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		var b strings.Builder

		// ---- Counters ----
		writeLabeledInt(&b, "nexus_requests_total", "Total requests processed", "counter", &m.requestsByLabel)
		writeLabeledInt(&b, "nexus_cache_hits_total", "Cache hits by layer", "counter", &m.cacheHitsByLayer)
		writeSimpleInt(&b, "nexus_cache_misses_total", "Total cache misses", "counter", m.cacheMissesTotal.Load())
		writeLabeledInt(&b, "nexus_tokens_total", "Tokens processed by direction", "counter", &m.tokensByDirection)
		writeLabeledFloat(&b, "nexus_cost_dollars_total", "Total cost in dollars", "counter", &m.costByLabel, 1_000_000)
		writeLabeledInt(&b, "nexus_security_blocks_total", "Security blocks by reason", "counter", &m.securityBlocks)
		writeLabeledInt(&b, "nexus_cache_evictions_total", "Cache evictions by layer", "counter", &m.cacheEvictions)
		writeSimpleInt(&b, "nexus_synonym_promotions_total", "Synonym promotion events", "counter", m.synonymPromotions.Load())

		// ---- Histograms ----
		writeHistogramFamily(&b, "nexus_request_duration_seconds", "Request latency histogram", &m.requestDuration)
		writeHistogramFamily(&b, "nexus_cache_lookup_duration_seconds", "Cache lookup latency", &m.cacheLookup)
		writeHistogramFamily(&b, "nexus_embedding_duration_seconds", "Embedding generation latency", &m.embeddingDuration)
		writeHistogramFamily(&b, "nexus_eval_confidence_score", "Eval confidence score distribution", &m.evalConfidence)

		// ---- New counters ----
		writeSimpleInt(&b, "nexus_compression_tokens_saved_total", "Tokens saved by compression", "counter", m.compressionTokensSaved.Load())
		writeLabeledInt(&b, "nexus_cascade_attempts_total", "Cascade attempts by result", "counter", &m.cascadeAttempts)

		// ---- Gauges ----
		writeLabeledInt(&b, "nexus_cache_entries", "Current cache entries per layer", "gauge", &m.cacheEntries)
		writeSimpleInt(&b, "nexus_active_requests", "Current in-flight requests", "gauge", m.activeRequests.Load())

		uptime := time.Since(m.startTime).Seconds()
		writeSimpleFloat(&b, "nexus_uptime_seconds", "Time since gateway start", "gauge", uptime)

		totalHits := sumMap(&m.cacheHitsByLayer)
		totalMisses := m.cacheMissesTotal.Load()
		total := totalHits + totalMisses
		var hitRate float64
		if total > 0 {
			hitRate = float64(totalHits) / float64(total)
		}
		writeSimpleFloat(&b, "nexus_cache_hit_rate", "Cache hit rate", "gauge", hitRate)

		w.Write([]byte(b.String()))
	}
}
