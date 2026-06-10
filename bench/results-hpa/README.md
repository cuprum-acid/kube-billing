# kube-billing HPA scale-up demo — 2026-06-10

Empirical test of the *automatic scaling* property the thesis asserts
for kube-billing. The experiment deploys the controller in-cluster
(not `make run`, because HPA only works for in-cluster Deployments),
sets the BillingPlan reconcile interval to 1 second so 200 active
subscriptions produce sustained reconcile work, and watches the
HorizontalPodAutoscaler decide how many replicas to spin up.

## Procedure

1. Build the manager image into Minikube's docker:
   ```sh
   eval $(minikube docker-env)
   docker build -t kube-billing:hpa-test .
   make deploy IMG=kube-billing:hpa-test
   minikube addons enable metrics-server   # HPA needs this
   ```

2. Reduce the basic BillingPlan's reconcile interval so each
   Subscription re-reconciles every second under steady-state:
   ```sh
   kubectl patch billingplan basic -n default --type=merge \
       -p '{"spec":{"requeueIntervalSeconds":1}}'
   ```

3. Run `hpa-driver.sh`. It polls the HPA every 2 s for 280 s,
   capturing `(t_s, currentReplicas, readyReplicas,
   cpu_utilization_pct, memory_utilization_pct)` into
   `hpa-timeseries.csv`. Three phases:
   - 0–20 s baseline (idle, 1 replica)
   - 20–204 s load (200 active subscriptions reconciling every 1 s)
   - 204–284 s drain (subscriptions deleted; CPU drops; HPA does
     not scale down within the window because the default
     `scaleDown.stabilizationWindowSeconds` is 300)

## Files

| File | Contents |
| --- | --- |
| `hpa-driver.sh` | Driver script (parameterised on `$OUT_DIR`, `$NS`, `$HPA_NAME`). |
| `hpa-timeseries.csv` | `t_s,replicas,ready,cpu_pct,mem_pct` (2 s resolution) |
| `hpa-events.csv` | `t_s,event` (baseline/load/drain markers) |
| `per-pod-cpu.txt` | Snapshot of `kubectl top pods` at the steady-state plateau, showing the leader pod consuming roughly 2× the CPU of the two standby pods. |

## Findings

- **HPA does scale up.** At T≈100 s, the average CPU utilisation
  crosses ~225 % of the 80 % target and the HPA's
  `desiredReplicas = ceil(currentReplicas × currentMetric /
  targetMetric)` formula gives `ceil(1 × 225/80) = 3`. Replicas
  jump 1 → 3 in a single decision window.
- **But the throughput does not increase.** The kube-billing
  controller-manager is deployed with `--leader-elect`. Of the
  three pods, only the elected leader reconciles; the other two
  hold the leader-election lease passively. The `kubectl top
  pods` snapshot at the plateau shows leader = 4 m CPU /
  23 Mi memory, standbys = 2 m / 9 Mi each. The standbys are
  paying their idle informer cost plus health probes, no more.
- **Scaling buys fault-domain redundancy, not horizontal
  throughput.** The HPA-driven scale-up gives the kube-billing
  workload three independent fault domains: any one pod can crash
  and another takes over within the leader-election lease timeout
  (`renewDeadline` default = 10 s). Equivalent throughput growth
  would require turning leader-election off and either (a)
  partitioning the workload across controllers, or (b) accepting
  that all controllers race on every `Status().Update` write and
  rely on optimistic-concurrency retries to converge.

The headline finding is non-obvious because the kube-billing
manifests advertise HPA prominently in §3.3.10 and §4.2.10 of the
thesis without addressing the interaction with leader election;
this experiment makes the interaction explicit.
