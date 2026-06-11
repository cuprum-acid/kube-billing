#!/bin/bash
# Resource-utilisation sampling for the kube-billing controller (§5.4).
# Phases: idle 180s -> sustained reconcile load 300s (200 subscriptions on a
# requeueIntervalSeconds=1 plan) -> cleanup -> 3x rollout-restart startup timing.
# Outputs: resources.csv (epoch_s,pod,cpu_m,mem_Mi), events.csv, startup.csv.
set -e
cd "$(dirname "$0")"
NS=kube-billing-system
RES=resources.csv; EV=events.csv; SU=startup.csv
echo "epoch_s,pod,cpu_m,mem_Mi" > $RES
echo "epoch_s,event" > $EV
echo "trial,startup_s" > $SU
ev() { echo "$(date +%s),$1" >> $EV; }

EXISTING=$(kubectl get subscriptions -n default --no-headers 2>/dev/null | wc -l | tr -d ' ')
if [ "$EXISTING" != "0" ]; then echo "default ns has $EXISTING subscriptions; aborting"; exit 1; fi

( while true; do
    TS=$(date +%s)
    kubectl top pods -n $NS --no-headers 2>/dev/null \
      | awk -v ts=$TS '{gsub(/m$/,"",$2); gsub(/Mi$/,"",$3); print ts","$1","$2","$3}' >> $RES
    sleep 10
  done ) & SAMPLER=$!
trap "kill $SAMPLER 2>/dev/null" EXIT

ev idle_start;  sleep 180;  ev idle_end

kubectl apply -f - <<'YAML' > /dev/null
apiVersion: billing.cloud-native.io/v1alpha1
kind: BillingPlan
metadata:
  name: bench-rsrc
  namespace: default
spec:
  price: "10.00"
  currency: USD
  billingPeriod: monthly
  requeueIntervalSeconds: 1
YAML
ev plan_created

ev load_subs_create_start
( cd ../.. && go run ./bench -n 200 -c 50 -plan bench-rsrc -prefix bench-rsrc-sub -cleanup=false -timeout 2m ) > load-run.log 2>&1
ev load_subs_created
sleep 300
ev load_end

kubectl delete subscriptions --all -n default --wait=true > /dev/null 2>&1
kubectl delete billingplan bench-rsrc -n default > /dev/null 2>&1
ev cleanup_done
sleep 60
ev recovery_end

kill $SAMPLER 2>/dev/null; trap - EXIT

for i in 1 2 3; do
  kubectl rollout restart deploy/kube-billing-controller-manager -n $NS > /dev/null
  kubectl rollout status deploy/kube-billing-controller-manager -n $NS --timeout=180s > /dev/null
  POD=$(kubectl get pods -n $NS --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1:].metadata.name}')
  CRE=$(kubectl get pod $POD -n $NS -o jsonpath='{.metadata.creationTimestamp}')
  RDY=$(kubectl get pod $POD -n $NS -o jsonpath='{.status.conditions[?(@.type=="Ready")].lastTransitionTime}')
  S=$(python3 -c "from datetime import datetime; f=lambda s: datetime.fromisoformat(s.replace('Z','+00:00')).timestamp(); print(round(f('$RDY')-f('$CRE'),1))")
  echo "$i,$S" >> $SU
  sleep 10
done
echo DONE
