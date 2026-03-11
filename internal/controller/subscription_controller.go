/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	billingv1alpha1 "github.com/example/kube-billing/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const SubscriptionFinalizer = "billing.cloud-native.io/finalizer"

// SubscriptionReconciler reconciles a Subscription object
type SubscriptionReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=billing.cloud-native.io,resources=subscriptions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=billing.cloud-native.io,resources=subscriptions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=billing.cloud-native.io,resources=subscriptions/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Subscription object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.1/pkg/reconcile
func (r *SubscriptionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	log := log.FromContext(ctx)

	var sub billingv1alpha1.Subscription
	if err := r.Get(ctx, req.NamespacedName, &sub); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling subscription", "name", sub.Name)

	// ==============================
	// HANDLE DELETION
	// ==============================

	if !sub.ObjectMeta.DeletionTimestamp.IsZero() {

		if controllerutil.ContainsFinalizer(&sub, SubscriptionFinalizer) {

			log.Info("Running final billing before deletion")

			// здесь будет финальный биллинг
			// например списание последнего платежа

			controllerutil.RemoveFinalizer(&sub, SubscriptionFinalizer)

			if err := r.Update(ctx, &sub); err != nil {
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	}

	// ==============================
	// ADD FINALIZER
	// ==============================

	if !controllerutil.ContainsFinalizer(&sub, SubscriptionFinalizer) {

		controllerutil.AddFinalizer(&sub, SubscriptionFinalizer)

		if err := r.Update(ctx, &sub); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// ==============================
	// CHECK BILLING PLAN
	// ==============================

	var plan billingv1alpha1.BillingPlan

	if err := r.Get(ctx,
		types.NamespacedName{
			Name:      sub.Spec.PlanRef,
			Namespace: req.Namespace,
		},
		&plan,
	); err != nil {

		log.Error(err, "BillingPlan not found")

		sub.Status.State = "Error"
		_ = r.Status().Update(ctx, &sub)

		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	// ==============================
	// ACTIVATE SUBSCRIPTION
	// ==============================

	if sub.Status.State == "" {

		log.Info("Activating subscription")

		ActiveSubscriptions.Inc()

		now := metav1.Now()

		sub.Status.State = "Active"
		sub.Status.LastPayment = now
		sub.Status.NextBilling = metav1.NewTime(now.Add(30 * 24 * time.Hour))
		sub.Status.ObservedGeneration = sub.Generation

		if err := r.Status().Update(ctx, &sub); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: 30 * 24 * time.Hour}, nil
	}

	// ==============================
	// PERIODIC BILLING
	// ==============================

	if sub.Status.State == "Active" {

		now := time.Now()

		if !sub.Status.NextBilling.IsZero() && now.After(sub.Status.NextBilling.Time) {

			log.Info("Processing recurring payment", "subscription", sub.Name)

			price, err := strconv.ParseFloat(plan.Spec.Price, 64)
			if err != nil {
				log.Error(err, "Failed to parse plan price", "price", plan.Spec.Price)
				return ctrl.Result{}, err
			}
			RevenueTotal.Add(price)

			// здесь будет реальный платежный gateway
			// сейчас просто симулируем

			sub.Status.LastPayment = metav1.Now()
			sub.Status.NextBilling = metav1.NewTime(now.Add(30 * 24 * time.Hour))

			if err := r.Status().Update(ctx, &sub); err != nil {
				return ctrl.Result{}, err
			}

			// Пока замени: 30 * 24 * time.Hour на 30 * time.Second
			// Тогда будет: billing каждые 30 секунд
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}

		wait := time.Until(sub.Status.NextBilling.Time)

		log.Info("Next billing scheduled", "after", wait)

		return ctrl.Result{RequeueAfter: wait}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SubscriptionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&billingv1alpha1.Subscription{}).
		Named("subscription").
		Watches(
			&billingv1alpha1.BillingPlan{},
			handler.EnqueueRequestsFromMapFunc(r.mapPlanToSubscriptions),
		).
		Complete(r)
}

func (r *SubscriptionReconciler) mapPlanToSubscriptions(ctx context.Context, obj client.Object) []reconcile.Request {

	log := log.FromContext(ctx)

	plan := obj.(*billingv1alpha1.BillingPlan)

	var subs billingv1alpha1.SubscriptionList

	if err := r.List(ctx, &subs, client.InNamespace(plan.Namespace)); err != nil {
		log.Error(err, "unable to list subscriptions")
		return nil
	}

	var requests []reconcile.Request

	for _, sub := range subs.Items {

		if sub.Spec.PlanRef == plan.Name {

			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      sub.Name,
					Namespace: sub.Namespace,
				},
			})

		}
	}

	log.Info("BillingPlan change detected",
		"plan", plan.Name,
		"affectedSubscriptions", len(requests),
	)

	return requests
}
