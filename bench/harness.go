// Command harness drives the kube-billing controller from outside the
// cluster, creating N Subscription resources concurrently and measuring
// the time between Create returning and the resource reaching
// status.state == "Active".
//
// This is the harness Chapter 5 of the thesis uses to measure end-to-end
// reconcile-driven latency. The corresponding HTTP-driver for the
// traditional backend lives in ../../backend-billing/bench.
//
// Usage:
//
//	go run ./bench -n 200 -c 50 -plan basic -ns default -out subs.csv
//
// Requirements: a reachable cluster (KUBECONFIG or in-cluster config), a
// BillingPlan with the chosen name already present in the namespace, and
// the controller running.
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	billingv1alpha1 "github.com/example/kube-billing/api/v1alpha1"
)

func main() {
	n := flag.Int("n", 200, "number of Subscription resources to create")
	c := flag.Int("c", 50, "concurrent creator goroutines")
	plan := flag.String("plan", "basic", "BillingPlan name in the namespace")
	ns := flag.String("ns", "default", "namespace")
	prefix := flag.String("prefix", "bench-sub", "Subscription name prefix")
	timeout := flag.Duration("timeout", 2*time.Minute, "max wait per subscription to reach Active")
	pollEvery := flag.Duration("poll", 100*time.Millisecond, "poll interval while waiting for Active")
	out := flag.String("out", "", "optional CSV path: name,create_ms,activate_ms,status")
	cleanup := flag.Bool("cleanup", true, "delete created subscriptions on exit")
	flag.Parse()

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(billingv1alpha1.AddToScheme(scheme))

	cfg := ctrl.GetConfigOrDie()
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Verify the BillingPlan exists before generating load.
	var bp billingv1alpha1.BillingPlan
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: *plan, Namespace: *ns}, &bp); err != nil {
		log.Fatalf(
			"BillingPlan %q in namespace %q not found: %v "+
				"(apply config/samples/billing_v1alpha1_billingplan.yaml first)",
			*plan, *ns, err,
		)
	}

	type sample struct {
		name          string
		createDur     time.Duration
		activateDur   time.Duration
		finalState    string
		reachedActive bool
	}
	results := make([]sample, *n)
	var (
		successCount int64
		failCount    int64
		jobs         = make(chan int, *n)
		wg           sync.WaitGroup
	)
	for i := 0; i < *n; i++ {
		jobs <- i
	}
	close(jobs)

	runStart := time.Now()
	for w := 0; w < *c; w++ {
		wg.Go(func() {
			for i := range jobs {
				name := fmt.Sprintf("%s-%d", *prefix, i)
				sub := &billingv1alpha1.Subscription{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: *ns},
					Spec:       billingv1alpha1.SubscriptionSpec{UserID: fmt.Sprintf("user-%d", i), PlanRef: *plan},
				}

				createStart := time.Now()
				if err := k8sClient.Create(ctx, sub); err != nil {
					if !apierrors.IsAlreadyExists(err) {
						atomic.AddInt64(&failCount, 1)
						results[i] = sample{name: name, finalState: fmt.Sprintf("create-error: %v", err)}
						continue
					}
				}
				createDur := time.Since(createStart)

				activateDeadline := time.Now().Add(*timeout)
				var observed billingv1alpha1.Subscription
				reached := false
				for time.Now().Before(activateDeadline) {
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: *ns}, &observed); err == nil {
						if observed.Status.State == "Active" {
							reached = true
							break
						}
					}
					time.Sleep(*pollEvery)
				}
				activateDur := time.Since(createStart)

				if reached {
					atomic.AddInt64(&successCount, 1)
				} else {
					atomic.AddInt64(&failCount, 1)
				}
				results[i] = sample{
					name:          name,
					createDur:     createDur,
					activateDur:   activateDur,
					finalState:    observed.Status.State,
					reachedActive: reached,
				}
			}
		})
	}
	wg.Wait()
	totalWall := time.Since(runStart)

	// Aggregate.
	creates := make([]time.Duration, 0, *n)
	activates := make([]time.Duration, 0, *n)
	for _, s := range results {
		if s.createDur > 0 {
			creates = append(creates, s.createDur)
		}
		if s.reachedActive {
			activates = append(activates, s.activateDur)
		}
	}
	slices.Sort(creates)
	slices.Sort(activates)

	fmt.Printf("n=%d c=%d ns=%s plan=%s wall=%s\n",
		*n, *c, *ns, *plan, totalWall.Round(time.Millisecond))
	fmt.Printf("create:   ok=%d throughput=%.1f req/s\n",
		len(creates), float64(len(creates))/totalWall.Seconds())
	if len(creates) > 0 {
		fmt.Printf("          p50=%s p95=%s p99=%s\n",
			pctile(creates, 0.5), pctile(creates, 0.95), pctile(creates, 0.99))
	}
	fmt.Printf("activate: ok=%d fail=%d converged=%.1f /s\n",
		successCount, failCount, float64(successCount)/totalWall.Seconds())
	if len(activates) > 0 {
		fmt.Printf("          p50=%s p95=%s p99=%s\n",
			pctile(activates, 0.5), pctile(activates, 0.95), pctile(activates, 0.99))
	}

	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			log.Fatal(err)
		}
		w := csv.NewWriter(f)
		_ = w.Write([]string{"name", "create_ms", "activate_ms", "final_state"})
		for _, s := range results {
			_ = w.Write([]string{
				s.name,
				fmt.Sprintf("%.3f", float64(s.createDur.Microseconds())/1000.0),
				fmt.Sprintf("%.3f", float64(s.activateDur.Microseconds())/1000.0),
				s.finalState,
			})
		}
		w.Flush()
		_ = f.Close()
	}

	if *cleanup {
		fmt.Printf("cleaning up %d subscriptions...\n", *n)
		for i := 0; i < *n; i++ {
			name := fmt.Sprintf("%s-%d", *prefix, i)
			sub := &billingv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: *ns}}
			_ = k8sClient.Delete(ctx, sub)
		}
	}
}

func pctile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := int(float64(len(sorted)-1) * p)
	return sorted[i]
}
