#!/bin/bash
# HPA scale-up demo driver.
#
# Phase 1 (baseline, 20 s): no workload. Capture idle CPU + 1 replica.
# Phase 2 (load, 180 s): create 200 subscriptions against a BillingPlan
#                        whose requeueIntervalSeconds is 1 so every
#                        subscription re-reconciles every second.
# Phase 3 (drain, 40 s): delete subscriptions; CPU drops.
#
# Outputs:
#   hpa-timeseries.csv  -- t_s, replicas, ready, cpu_pct, mem_pct
#   hpa-events.csv      -- t_s, event_label

set -uo pipefail

OUT_DIR="${OUT_DIR:-/tmp/hpa}"
mkdir -p "$OUT_DIR"
TS_CSV="$OUT_DIR/hpa-timeseries.csv"
EV_CSV="$OUT_DIR/hpa-events.csv"
LOG="$OUT_DIR/hpa.log"

KUBE_REPO="${KUBE_REPO:-/Users/ebob/diploma/kube-billing}"
NS="${NS:-kube-billing-system}"
HPA_NAME="${HPA_NAME:-kube-billing-controller-manager-hpa}"
DEPLOY_NAME="${DEPLOY_NAME:-kube-billing-controller-manager}"

BASELINE_SECONDS=20
LOAD_SECONDS=180
DRAIN_SECONDS=40
N_SUBS=200
BATCH_C=10

echo "t_s,replicas,ready,cpu_pct,mem_pct" > "$TS_CSV"
echo "t_s,event" > "$EV_CSV"

t_start=$(date +%s)

now_s() {
    echo $(( $(date +%s) - t_start ))
}

log_event() {
    echo "$(now_s),$1" >> "$EV_CSV"
    echo "[$(now_s) s] $1" >&2
}

poll_state() {
    LOCK="$OUT_DIR/.poll.lock"
    touch "$LOCK"
    (
        while [ -e "$LOCK" ]; do
            t=$(now_s)
            hpa_json=$(kubectl get hpa -n "$NS" "$HPA_NAME" -o json 2>/dev/null || echo "{}")
            replicas=$(echo "$hpa_json" | jq -r '.status.currentReplicas // 0')
            cpu=$(echo "$hpa_json" | jq -r '
                (.status.currentMetrics // [])
                | map(select(.resource.name == "cpu"))
                | .[0].resource.current.averageUtilization // 0
            ')
            mem=$(echo "$hpa_json" | jq -r '
                (.status.currentMetrics // [])
                | map(select(.resource.name == "memory"))
                | .[0].resource.current.averageUtilization // 0
            ')
            ready=$(kubectl get deploy -n "$NS" "$DEPLOY_NAME" \
                -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo 0)
            ready=${ready:-0}
            echo "${t},${replicas},${ready},${cpu},${mem}" >> "$TS_CSV"
            sleep 2
        done
    ) &
    POLL_PID=$!
}

stop_poller() {
    rm -f "$OUT_DIR/.poll.lock"
    wait "$POLL_PID" 2>/dev/null
}

# --- Run ---
log_event "experiment_start"
poll_state

log_event "baseline_start"
sleep "$BASELINE_SECONDS"

log_event "load_start"
(cd "$KUBE_REPO" && go run ./bench \
    -n "$N_SUBS" -c "$BATCH_C" -plan basic -ns default \
    -prefix hpa -cleanup=false -timeout 30s \
    -out "$OUT_DIR/load-batch.csv" >> "$LOG" 2>&1)
log_event "load_subs_created"

sleep "$LOAD_SECONDS"

log_event "drain_start"
kubectl get subscriptions -n default -o name 2>/dev/null \
    | xargs -r kubectl delete -n default --wait=false >/dev/null 2>&1
sleep "$DRAIN_SECONDS"

stop_poller
log_event "experiment_end"

echo "timeseries: $TS_CSV"
echo "events:     $EV_CSV"
