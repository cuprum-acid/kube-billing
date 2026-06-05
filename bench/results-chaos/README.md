# kube-billing chaos test — 2026-06-05

Failure-injection experiment for the §6.5 "no failure injection" gap.
The setup is intentionally small (30 subscriptions per phase) so the
data is human-readable; the experimental design is the interesting
part, not the sample size.

## Procedure (three phases)

1. **Controller alive.** Start the controller as a *plain compiled
   binary* (`go build -o manager-binary ./cmd/main.go`) so it has a
   stable PID — `go run` exec's the binary into a child process,
   and `pkill -f cmd/main.go` only kills the parent, leaving the
   compiled binary alive. Create 30 subscriptions with the bench
   harness; all reach `status.state == Active` within a second.

2. **Controller killed.** `kill -KILL <pid>`. Wait for the process
   to exit. Immediately create 30 more subscriptions. The bench
   harness is invoked with `-timeout 1s` so it does not wait the
   default 2 minutes for a status that will not arrive; the
   subscriptions exist in the apiserver but their status is empty.
   Hold this state for ~12 seconds.

3. **Controller restored.** Restart the same binary. The new
   process inherits *no* in-memory state from the dead one --- its
   only inputs are (a) the CRD schema and (b) what etcd remembers
   about the namespace. It reconciles the 30 pending subscriptions
   from scratch, using their `spec` to compute the desired `status`.

A 500-millisecond poller (`kubectl get subscriptions -n default
-o json` plus a small `jq` aggregation) writes a timeseries CSV of
`(t_ms, total, active, pending)` throughout. Events
(`controller_kill_requested`, `controller_killed`,
`batch_start_p2`, …) are written to a separate CSV so the figure can
annotate them precisely.

## Files

| File | Schema |
| --- | --- |
| `chaos-timeseries.csv` | `t_ms,total,active,pending` (poll every 500 ms) |
| `chaos-events.csv` | `t_ms,event` (one row per controller-lifecycle event) |

## Headline findings

- **During the controller-down window** (between
  `controller_killed` and `controller_ready_pid_<new>`),
  Active count is flat at 30 while `total` rises to 60 as the
  phase-2 batch lands. No subscription transitions to Active.
- **Recovery latency:** between `controller_ready` (T=41.2 s in
  this run) and the last `pending → Active` transition (T=42.2 s)
  is approximately **1.0 second** for a backlog of 30 subscriptions
  on the M3 reference host with the default
  `--max-concurrent-reconciles=1`. This is the time the operator
  needs to "rebuild the world" purely from etcd state with no
  pre-warmed cache.
- The same property would hold for a backlog of N subscriptions
  scaled linearly: 30 N reconciles per second is the
  controller-runtime baseline measured by the throughput sweep in
  §5.4.1.

These three findings together back the §5.7 claim that the operator's
contract includes *declarative-state recovery* and pin the cost of
that recovery to a measurable number (1 second per 30 backlogged
resources).
