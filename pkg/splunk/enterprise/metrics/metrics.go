package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	UpgradeStartTime = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "splunk_upgrade_start_time",
		Help: "Unix timestamp when the SHC upgrade started",
	})
	UpgradeEndTime = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "splunk_upgrade_end_time",
		Help: "Unix timestamp when the SHC upgrade ended",
	})
	ShortSearchSuccessCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "splunk_short_search_success_total",
			Help: "Total number of successful short searches per search head",
		},
		[]string{"sh_name"},
	)
	ShortSearchFailureCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "splunk_short_search_failure_total",
			Help: "Total number of failed short searches per search head",
		},
		[]string{"sh_name"},
	)
	TotalSearchSuccessCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "splunk_total_search_success_total",
			Help: "Total number of successful total searches per search head",
		},
		[]string{"sh_name"},
	)
	TotalSearchFailureCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "splunk_total_search_failure_total",
			Help: "Total number of failed total searches per search head",
		},
		[]string{"sh_name"},
	)
)

func init() {
	prometheus.MustRegister(UpgradeStartTime)
	prometheus.MustRegister(UpgradeEndTime)
	prometheus.MustRegister(ShortSearchSuccessCounter)
	prometheus.MustRegister(ShortSearchFailureCounter)
	prometheus.MustRegister(TotalSearchSuccessCounter)
	prometheus.MustRegister(TotalSearchFailureCounter)
}
