#!/bin/bash
# Chaos test for kube-billing self-healing.
#
# Three phases:
#   1. controller alive, create 30 subs, watch them all reach Active
#   2. controller killed, create 30 more subs, watch them stay pending
#   3. controller restarted, watch the backlog drain to Active
#
# Outputs (in $OUT_DIR):
#   chaos-timeseries.csv  -- t_ms,total,active,pending (every 500 ms)
#   chaos-events.csv      -- t_ms,event_label

set -uo pipefail

OUT_DIR="${OUT_DIR:-/tmp/chaos}"
mkdir -p "$OUT_DIR"
TS_CSV="$OUT_DIR/chaos-timeseries.csv"
EV_CSV="$OUT_DIR/chaos-events.csv"
CTRL_LOG="$OUT_DIR/controller.log"
KUBE_REPO="${KUBE_REPO:-/Users/ebob/diploma/kube-billing}"
BINARY="${BINARY:-$OUT_DIR/manager-binary}"

if [ ! -x "$BINARY" ]; then
    echo "Prebuilt controller binary not found at $BINARY; aborting." >&2
    exit 1
fi

echo "t_ms,total,active,pending" > "$TS_CSV"
echo "t_ms,event" > "$EV_CSV"

t_start_ms=$(($(date +%s%N) / 1000000))

now_ms() {
    echo $(( ($(date +%s%N) / 1000000) - t_start_ms ))
}

log_event() {
    echo "$(now_ms),$1" >> "$EV_CSV"
    echo "[$(now_ms) ms] $1" >&2
}

CTRL_PID=""

start_controller() {
    log_event "controller_start"
    "$BINARY" \
        --metrics-bind-address=:8082 \
        --metrics-secure=false \
        --health-probe-bind-address=:8083 \
        --max-concurrent-reconciles=1 \
        >> "$CTRL_LOG" 2>&1 &
    CTRL_PID=$!
    until curl -sS -m 2 http://localhost:8083/readyz 2>/dev/null | grep -q ok; do
        sleep 0.5
    done
    log_event "controller_ready_pid_$CTRL_PID"
}

kill_controller() {
    log_event "controller_kill_requested_pid_$CTRL_PID"
    kill -KILL "$CTRL_PID" 2>/dev/null
    wait "$CTRL_PID" 2>/dev/null
    log_event "controller_killed"
}

poll_state() {
    LOCK="$OUT_DIR/.poll.lock"
    touch "$LOCK"
    (
        while [ -e "$LOCK" ]; do
            line=$(kubectl get subscriptions -n default -o json 2>/dev/null \
                | jq -r '
                    .items as $items |
                    ($items | length) as $total |
                    ([$items[] | select(.status.state == "Active")] | length) as $active |
                    "\($total),\($active),\($total - $active)"
                ')
            if [ -n "$line" ]; then
                echo "$(now_ms),$line" >> "$TS_CSV"
            fi
            sleep 0.5
        done
    ) &
    POLL_PID=$!
}

stop_poller() {
    rm -f "$OUT_DIR/.poll.lock"
    wait "$POLL_PID" 2>/dev/null
}

create_batch() {
    local prefix=$1
    local n=$2
    local c=$3
    local timeout=${4:-2m}
    log_event "batch_start_${prefix}_n${n}_c${c}_t${timeout}"
    (cd "$KUBE_REPO" && go run ./bench \
        -n "$n" -c "$c" -plan basic -ns default \
        -prefix "$prefix" -cleanup=false \
        -timeout "$timeout" \
        -out "$OUT_DIR/batch-${prefix}.csv" >> "$CTRL_LOG" 2>&1)
    log_event "batch_end_${prefix}"
}

# --- Run ---

# Clean any leftover subscriptions
kubectl get subscriptions -n default -o name 2>/dev/null \
    | xargs -r kubectl delete -n default --wait=false >/dev/null 2>&1
sleep 2

start_controller
poll_state

# Phase 1: controller alive, 30 subs
create_batch "p1" 30 5
sleep 8   # long enough for all phase-1 subs to reach Active

# Phase 2: kill controller, then 30 more subs.
# Use a 1-second per-sub timeout because the controller is dead and
# the default 2-minute timeout would hang the script. The
# subscriptions still get created via the apiserver; they just stay
# in the initial empty-status state until the controller restarts.
kill_controller
create_batch "p2" 30 5 1s
sleep 12   # window during which backlog accrues

# Phase 3: restart controller
start_controller
sleep 18   # backlog drains

stop_poller
log_event "experiment_end"

echo "timeseries: $TS_CSV"
echo "events:     $EV_CSV"
