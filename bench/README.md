# kube-billing benchmark harness

This directory contains the load harness that produces the end-to-end
reconcile-latency and throughput numbers Chapter 5 of the thesis reports
for `kube-billing`. The companion HTTP harness for `backend-billing` lives
in [`../../backend-billing/bench`](../../backend-billing/bench).

## What the harness measures

For each Subscription:

1. record `t_create_start`
2. `k8sClient.Create()` the resource
3. record `t_create_end`  →  reports `create_ms = t_create_end - t_create_start`
4. poll the resource's `.status.state` until it reads `"Active"` or
   `timeout` expires
5. record `t_active`  →  reports `activate_ms = t_active - t_create_start`

`create_ms` corresponds to the cost of an apiserver write plus admission;
`activate_ms` adds the time it takes the reconciler to observe the watch
event, run the activation branch, and persist the status update. The two
columns are reported separately because they answer different questions:
the first is "how fast can a user push a resource", the second is "how
fast does the operator drive the resource to its target state". The
Chapter 5 latency table reports `activate_ms` percentiles.

The harness records each subscription individually, not in fixed
reporting windows, to avoid coordinated omission (Tene, "How NOT to
measure latency", Strange Loop 2015).

## Prerequisites

- A reachable Kubernetes cluster (Minikube, Kind, or a real cluster).
  `KUBECONFIG` must point at it.
- The CRDs installed: `make install` from the project root.
- The controller running: `make run` (out-of-cluster) or `make deploy`
  (in-cluster).
- A `BillingPlan` named `basic` in the target namespace:

  ```
  kubectl apply -f config/samples/billing_v1alpha1_billingplan.yaml
  ```

## Running

```
go run ./bench \
    -n 200 -c 50 \
    -plan basic -ns default \
    -out subscriptions.csv
```

Output:

```
n=200 c=50 ns=default plan=basic wall=8.213s
create:   ok=200 throughput=24.4 req/s
          p50=22ms p95=58ms p99=110ms
activate: ok=200 fail=0 converged=24.4 /s
          p50=110ms p95=240ms p99=380ms
```

The CSV captures `name,create_ms,activate_ms,final_state` for downstream
analysis.

## Reproducing the thesis numbers

The numbers in Chapter 5 were captured on:

- Apple M3 host (4 P + 4 E cores), 8 GB unified memory, macOS 15.7.7
- Minikube v1.33.1 with Kubernetes v1.29.0, started with
  `minikube start --cpus=4 --memory=6g --driver=docker`
- containerd 1.7.0
- kube-billing at the commit recorded in Chapter 5
- 10 repeated runs, the first run after a 60-second controller warm-up
  discarded; percentiles aggregated across the remaining runs

To regenerate them on a clean host:

```
minikube start --cpus=4 --memory=6g
make install deploy
kubectl apply -f config/samples/billing_v1alpha1_billingplan.yaml
# wait for controller to be Ready
for i in $(seq 1 10); do
    go run ./bench -n 200 -c 50 -plan basic -ns default \
        -out bench/results/activate-run$i.csv
done
```

## Notes

- The harness is intentionally narrow: it measures `Create -> Active`. It
  does not exercise the billing-cycle path (that requires waiting at least
  `RequeueIntervalSeconds`, default 30 s) or the deletion path. Future
  work could extend the harness to cover both.
- Throughput here is *converged* status updates per second, not
  apiserver requests per second. The Chapter 5 discussion of
  construct mismatch between this number and the backend's HTTP
  throughput applies.
