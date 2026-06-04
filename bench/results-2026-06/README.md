# kube-billing bench results — 2026-06-04

Raw per-subscription CSVs from `bench/harness.go`, captured on the
reference Apple M3 host (8 GB unified memory, macOS 15.7.7) against
Minikube v1.33.1 (Kubernetes v1.32.0) at kube-billing commit
`00dfbcb`. The controller ran out-of-cluster via `go run ./cmd/main.go
--metrics-bind-address=:8082 --metrics-secure=false
--health-probe-bind-address=:8083` so the metrics endpoint didn't
collide with the backend-billing API on port 8080.

CSV schema: `name,create_ms,activate_ms,final_state`.
`create_ms` is the apiserver `Create()` round-trip; `activate_ms`
is from `Create()` returning to the watch event reporting
`status.state == Active`.

## Files

Three independent trials (n=200, c=50, plan=basic, namespace=default)
captured back-to-back so any background work-queue warm-up is visible
across trials.

| File | Invocation |
| --- | --- |
| `kube-activate.csv` | `go run ./bench -n 200 -c 50 -plan basic -ns default -out kube-activate.csv` |
| `kube-activate-2.csv` | `... -out kube-activate-2.csv -prefix bench2` |
| `kube-activate-3.csv` | `... -out kube-activate-3.csv -prefix bench3` |

## Notes

The controller was started fresh (no warm informer caches) before
trial 1. Trial-to-trial p50 stddev for the activate latency is ±52 ms
across the three trials, which is the headline noise floor for the
"to status Active" measurement on this hardware.
