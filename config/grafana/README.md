# Grafana dashboard

`billing-dashboard.json` is a Grafana 11 dashboard that visualises the
kube-billing controller's business and runtime metrics.

## Panels

- **Active subscriptions** – current value of `billing_active_subscriptions`
- **Revenue (cumulative)** – `billing_revenue_total` (currency-agnostic; see
  the thesis limitations)
- **Payment failures** – cumulative `billing_payment_failures`
- **Reconcile rate** – `rate(controller_runtime_reconcile_total[1m])` broken
  down by `controller` and `result`
- **Work-queue depth and add rate** – `workqueue_depth` and
  `rate(workqueue_adds_total[1m])` by queue name
- **Reconcile latency percentiles** – p50 / p95 / p99 derived from
  `controller_runtime_reconcile_time_seconds_bucket`
- **Reconcile errors and payment failures** – per-second error rates

## Importing

The dashboard expects a Prometheus datasource with the UID `prometheus`
(the default that kube-prometheus-stack provisions). To import it
manually:

```sh
curl -s -u admin:<password> \
    -X POST http://grafana.local/api/dashboards/db \
    -H 'Content-Type: application/json' \
    --data-binary @<(jq '{dashboard: ., overwrite: true}' config/grafana/billing-dashboard.json)
```

Or via the UI: **Dashboards → New → Import** and paste the JSON.

## Required scrapes

The kube-billing controller exposes `/metrics` on port 8443 when the
default kustomization's `manager_metrics_patch.yaml` is applied. The
`ServiceMonitor` in `config/prometheus/monitor.yaml` (commented out in
the default kustomization, see `config/default/kustomization.yaml`)
scrapes that endpoint via the prometheus-operator. Enable it when you
want this dashboard to populate.
