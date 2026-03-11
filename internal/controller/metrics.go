package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	ActiveSubscriptions = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "billing_active_subscriptions",
			Help: "Number of active subscriptions",
		},
	)

	RevenueTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "billing_revenue_total",
			Help: "Total revenue processed by billing",
		},
	)

	PaymentFailures = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "billing_payment_failures",
			Help: "Number of failed payments",
		},
	)
)

func init() {

	metrics.Registry.MustRegister(ActiveSubscriptions)
	metrics.Registry.MustRegister(RevenueTotal)
	metrics.Registry.MustRegister(PaymentFailures)

}
