# kube-billing concurrency sweep — 2026-06-05

Sweep across `(workers, c)` for the empirical verification of the
"raise MaxConcurrentReconciles to drain the queue" prediction in
thesis §5.4. Captured on the M3 reference host against the same
Minikube cluster as `results-2026-06/`, kube-billing commit running
the new `--max-concurrent-reconciles` flag.

For each `(workers, c)` cell we run `bench/harness.go -n 100
-c $c -plan basic -ns default -prefix w$wc$c`, with the namespace
truncated of bench-* subscriptions between runs. The CSV schema is
the same as `results-2026-06/`: `name,create_ms,activate_ms,final_state`.

## Files

| File | Controller flag | Bench concurrency |
| --- | --- | --- |
| `w1-c1.csv` | `--max-concurrent-reconciles=1` | `-c 1` |
| `w1-c5.csv` | `--max-concurrent-reconciles=1` | `-c 5` |
| `w1-c10.csv` | `--max-concurrent-reconciles=1` | `-c 10` |
| `w1-c25.csv` | `--max-concurrent-reconciles=1` | `-c 25` |
| `w1-c50.csv` | `--max-concurrent-reconciles=1` | `-c 50` |
| `w1-c100.csv` | `--max-concurrent-reconciles=1` | `-c 100` |
| `w4-c*.csv` | `--max-concurrent-reconciles=4` | matching `-c` values |

## Finding

The §5.4 prediction (raising the concurrency knob from 1 to 4 should
compress the convergence latency by roughly 4× at c=50) is **falsified** by
this sweep. At c=50 the 4-worker p50 is *higher* than the 1-worker p50,
because the four parallel reconcile workers' `Status().Update` and
`Event` writes contend with the bench harness's own `Create()` traffic
for apiserver bandwidth on the single-node Minikube. The crossover sits
between c=10 and c=25:

| c | 1 worker p50 | 4 workers p50 | 4-worker create p95 |
| ---: | ---: | ---: | ---: |
|   1 | 113 ms | 113 ms | 8 ms |
|   5 | 109 ms | 110 ms | 10 ms |
|  10 | 108 ms | 108 ms | 109 ms |
|  25 | 215 ms | 129 ms | 92 ms |
|  50 | 356 ms | 565 ms | 197 ms |
| 100 | 673 ms | 950 ms | 144 ms |

The `create_ms` p95 column tells the apiserver-contention story
directly: at 4 workers the harness's own Create round-trip spikes from
~5 ms (baseline) to 197 ms at c=50, mid-sweep. The cure (more workers)
becomes a worse disease than the symptom (queue wait) once apiserver
saturation kicks in.

This sweep is the data behind Figure 5.4 in the thesis. The decomposition
discussion in §5.4 has been rewritten around the measured crossover.
