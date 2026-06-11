# kube-billing controller resource sampling — 2026-06-11

Raw resource-utilisation timeseries backing §5.4 of the thesis, replacing
the earlier interactive estimates with committed, reproducible data.
Captured on the reference Apple M3 host against the Minikube cluster
(Kubernetes v1.29, metrics-server enabled), with the controller deployed
in-cluster via `make deploy` (requests 100m CPU / 128Mi, limits 500m /
256Mi, `--leader-elect` on, HPA 1–5 replicas present as configured).

## Procedure (`sample-resources.sh`)

A background sampler records `kubectl top pods -n kube-billing-system`
every 10 s (metrics-server readings are themselves time-averaged and lag
by up to its resolution window). Phases, delimited in `events.csv`:

1. **idle** — 180 s with no Subscription resources in the cluster.
2. **load** — a `bench-rsrc` BillingPlan with
   `requeueIntervalSeconds: 1` is created and the bench harness creates
   200 subscriptions (`-c 50 -cleanup=false`); the subscriptions then
   re-reconcile every second for 300 s, i.e. an offered load of
   ~200 reconciles/s on the leader, the same shape as the HPA
   experiment in `results-hpa/`.
3. **cleanup + recovery** — all bench subscriptions and the plan are
   deleted; 60 s of post-load samples.
4. **startup ×3** — `kubectl rollout restart` of the controller
   Deployment; startup time is computed per fresh pod as
   `Ready.lastTransitionTime − metadata.creationTimestamp`.

The HPA may scale the Deployment up during the load window (that is the
system as configured); all pods are recorded, and the thesis reports the
leader's figures.

## Files

| File | Schema |
| --- | --- |
| `resources.csv` | `epoch_s,pod,cpu_m,mem_Mi` (as reported by `kubectl top`) |
| `events.csv` | `epoch_s,event` phase boundaries |
| `startup.csv` | `trial,startup_s` |
| `load-run.log` | bench harness console output for the creation burst |

## Caveats

- `kubectl top` values are metrics-server aggregates, not instantaneous
  readings; short spikes inside one resolution window are smoothed.
- Startup time includes scheduling and image-pull-policy checks inside
  the cluster plus the readiness probe's `initialDelaySeconds: 5` /
  `periodSeconds: 10`, so it is dominated by probe cadence, not binary
  start latency.
