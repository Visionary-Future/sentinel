package investigation

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	investigationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sentinel",
		Name:      "investigations_total",
		Help:      "Total number of investigations by final status.",
	}, []string{"status"})

	investigationDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "sentinel",
		Name:      "investigation_duration_seconds",
		Help:      "Duration of investigations in seconds.",
		Buckets:   []float64{5, 15, 30, 60, 120, 300, 600},
	})

	activeInvestigations = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "sentinel",
		Name:      "active_investigations",
		Help:      "Number of currently running investigations.",
	})

	llmTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sentinel",
		Name:      "llm_tokens_total",
		Help:      "Total LLM tokens consumed by investigations.",
	}, []string{"provider", "model"})

	llmCallErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sentinel",
		Name:      "llm_call_errors_total",
		Help:      "Total LLM call errors.",
	}, []string{"provider"})

	dedupSkipped = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "sentinel",
		Name:      "investigations_dedup_skipped_total",
		Help:      "Investigations skipped due to dedup.",
	})

	investigationCost = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "sentinel",
		Name:      "investigation_cost_dollars",
		Help:      "Estimated cost in USD per investigation by provider and model.",
	}, []string{"provider", "model"})

	investigationCostHist = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "sentinel",
		Name:      "investigation_cost_dollars_distribution",
		Help:      "Distribution of investigation costs in USD.",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 5.0},
	})

	investigationQuality = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "sentinel",
		Name:      "investigation_quality_score",
		Help:      "Quality score of completed investigations (0-100).",
		Buckets:   []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100},
	})
)
