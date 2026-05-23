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
	"fmt"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	billingv1alpha1 "github.com/example/kube-billing/api/v1alpha1"
)

const SubscriptionFinalizer = "billing.cloud-native.io/finalizer"

// SubscriptionReconciler reconciles a Subscription object
type SubscriptionReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
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

	ctx, span := Tracer.Start(ctx, "ReconcileSubscription")
	defer span.End()

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

			// Декомент метрики активной подписки
			if sub.Status.State == "Active" {
				ActiveSubscriptions.Dec()
			}

			// Финальный биллинг — пропорциональное списание за оставшиеся дни
			// Получаем BillingPlan для расчёта
			var plan billingv1alpha1.BillingPlan
			if err := r.Get(ctx, types.NamespacedName{Name: sub.Spec.PlanRef, Namespace: req.Namespace}, &plan); err == nil {
				// Проверяем, был ли уже биллинг в текущем периоде
				if !sub.Status.NextBilling.IsZero() {
					// Считаем пропорциональную сумму за неиспользованные дни
					now := time.Now()
					if now.Before(sub.Status.NextBilling.Time) {
						// Ещё не наступил следующий период — считаем пропорционально
						remainingTime := sub.Status.NextBilling.Time.Sub(now)
						totalPeriod := time.Duration(plan.Spec.RequeueIntervalSeconds) * time.Second

						if totalPeriod > 0 && remainingTime > 0 {
							price, err := strconv.ParseFloat(plan.Spec.Price, 64)
							if err == nil {
								// Пропорциональный возврат (refund)
								refundAmount := price * (remainingTime.Seconds() / totalPeriod.Seconds())

								if refundAmount > 0 {
									log.Info("Final billing: calculating refund",
										"amount", fmt.Sprintf("%.2f", refundAmount),
										"remainingTime", remainingTime.String())

									// Генерация события о финальном биллинге
									r.Recorder.Event(&sub, "Normal", "FinalBilling",
										fmt.Sprintf("Final billing: refund of %s %s (pro-rated)",
											fmt.Sprintf("%.2f", refundAmount), plan.Spec.Currency))
								}
							}
						}
					}
				}
			}

			controllerutil.RemoveFinalizer(&sub, SubscriptionFinalizer)

			if err := r.Update(ctx, &sub); err != nil {
				return ctrl.Result{}, err
			}

			// Обновляем статус BillingPlan после удаления подписки
			// Получаем planRef из spec подписки
			if sub.Spec.PlanRef != "" {
				_ = r.updateBillingPlanStatus(ctx, sub.Spec.PlanRef, req.Namespace)
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

		// Устанавливаем условие BillingPlanNotFound
		meta.SetStatusCondition(&sub.Status.Conditions, metav1.Condition{
			Type:    billingv1alpha1.SubscriptionBillingPlanNotFound,
			Status:  metav1.ConditionTrue,
			Reason:  "BillingPlanNotFound",
			Message: fmt.Sprintf("Referenced BillingPlan '%s' does not exist", sub.Spec.PlanRef),
		})

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

		// Получаем интервал биллинга из плана (по умолчанию 30 секунд для тестирования)
		billingInterval := getBillingInterval(plan.Spec.RequeueIntervalSeconds)

		sub.Status.State = "Active"
		sub.Status.LastPayment = now
		sub.Status.NextBilling = metav1.NewTime(now.Add(billingInterval))
		sub.Status.ObservedGeneration = sub.Generation

		// Устанавливаем условие Active
		meta.SetStatusCondition(&sub.Status.Conditions, metav1.Condition{
			Type:    billingv1alpha1.SubscriptionActive,
			Status:  metav1.ConditionTrue,
			Reason:  "SubscriptionActivated",
			Message: "Subscription successfully activated",
		})

		// Снимаем условие BillingPlanNotFound если оно было
		meta.SetStatusCondition(&sub.Status.Conditions, metav1.Condition{
			Type:    billingv1alpha1.SubscriptionBillingPlanNotFound,
			Status:  metav1.ConditionFalse,
			Reason:  "BillingPlanFound",
			Message: "Referenced BillingPlan exists",
		})

		if err := r.Status().Update(ctx, &sub); err != nil {
			return ctrl.Result{}, err
		}

		// Обновляем статус BillingPlan
		_ = r.updateBillingPlanStatus(ctx, plan.Name, req.Namespace)

		return ctrl.Result{RequeueAfter: billingInterval}, nil
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

				// Инкремент метрики неудачных платежей
				PaymentFailures.Inc()

				// Перевод подписки в статус ошибки
				sub.Status.State = "PaymentError"
				sub.Status.ObservedGeneration = sub.Generation

				// Устанавливаем условие PaymentError
				meta.SetStatusCondition(&sub.Status.Conditions, metav1.Condition{
					Type:    billingv1alpha1.SubscriptionPaymentError,
					Status:  metav1.ConditionTrue,
					Reason:  "PaymentFailed",
					Message: fmt.Sprintf("Failed to process payment: %v", err),
				})

				if err := r.Status().Update(ctx, &sub); err != nil {
					return ctrl.Result{}, err
				}

				// Генерация события
				r.Recorder.Event(&sub, "Warning", "PaymentFailed",
					fmt.Sprintf("Failed to process payment: %v", err))

				// Повторная попытка через 5 минут
				return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
			}

			ctx2, paymentSpan := Tracer.Start(ctx, "ProcessPayment")

			RevenueTotal.Add(price)

			paymentSpan.End()
			// здесь будет реальный платежный gateway
			// сейчас просто симулируем

			// Получаем интервал биллинга из плана (по умолчанию 30 секунд для тестирования)
			billingInterval := getBillingInterval(plan.Spec.RequeueIntervalSeconds)

			sub.Status.LastPayment = metav1.Now()
			sub.Status.NextBilling = metav1.NewTime(now.Add(billingInterval))

			// Устанавливаем условие Active и снимаем PaymentError
			meta.SetStatusCondition(&sub.Status.Conditions, metav1.Condition{
				Type:    billingv1alpha1.SubscriptionActive,
				Status:  metav1.ConditionTrue,
				Reason:  "PaymentProcessed",
				Message: "Payment processed successfully",
			})
			meta.SetStatusCondition(&sub.Status.Conditions, metav1.Condition{
				Type:    billingv1alpha1.SubscriptionPaymentError,
				Status:  metav1.ConditionFalse,
				Reason:  "PaymentSuccess",
				Message: "Last payment was successful",
			})

			if err := r.Status().Update(ctx2, &sub); err != nil {
				return ctrl.Result{}, err
			}

			// Обновляем статус BillingPlan
			_ = r.updateBillingPlanStatus(ctx2, plan.Name, req.Namespace)

			// Генерация события об успешном платеже
			r.Recorder.Event(&sub, "Normal", "PaymentProcessed",
				fmt.Sprintf("Payment of %s %s processed successfully", plan.Spec.Price, plan.Spec.Currency))

			return ctrl.Result{RequeueAfter: billingInterval}, nil
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

	ctx, span := Tracer.Start(ctx, "MapPlanToSubscriptions")
	defer span.End()

	return requests
}

// updateBillingPlanStatus updates the status of a BillingPlan with current subscription count and revenue
func (r *SubscriptionReconciler) updateBillingPlanStatus(ctx context.Context, planName, namespace string) error {
	// Получаем BillingPlan
	plan := &billingv1alpha1.BillingPlan{}
	if err := r.Get(ctx, types.NamespacedName{Name: planName, Namespace: namespace}, plan); err != nil {
		return err
	}

	// Считаем активные подписки
	var subs billingv1alpha1.SubscriptionList
	if err := r.List(ctx, &subs, client.InNamespace(namespace)); err != nil {
		return err
	}

	activeCount := int32(0)
	totalRevenue := 0.0

	for _, sub := range subs.Items {
		if sub.Spec.PlanRef == planName {
			if sub.Status.State == "Active" {
				activeCount++
			}
			// Считаем revenue из всех платежей
			if !sub.Status.LastPayment.IsZero() {
				price, err := strconv.ParseFloat(plan.Spec.Price, 64)
				if err == nil {
					totalRevenue += price
				}
			}
		}
	}

	// Обновляем статус если изменился
	updated := false
	if plan.Status.ActiveSubscriptions != activeCount {
		plan.Status.ActiveSubscriptions = activeCount
		updated = true
	}

	// Форматируем revenue с 2 знаками после запятой
	revenueStr := fmt.Sprintf("%.2f", totalRevenue)
	if plan.Status.TotalRevenue != revenueStr {
		plan.Status.TotalRevenue = revenueStr
		updated = true
	}

	// Устанавливаем условие Available
	if activeCount > 0 {
		meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionTrue,
			Reason:  "HasActiveSubscriptions",
			Message: fmt.Sprintf("Plan has %d active subscriptions", activeCount),
		})
	} else {
		meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
			Type:    "Available",
			Status:  metav1.ConditionTrue,
			Reason:  "NoActiveSubscriptions",
			Message: "Plan has no active subscriptions",
		})
	}

	if updated {
		return r.Status().Update(ctx, plan)
	}

	return nil
}

// getBillingInterval returns the billing interval from the plan spec.
// If not specified, defaults to 30 seconds for testing purposes.
// For production, set to 2592000 (30 days) for monthly billing.
func getBillingInterval(intervalSeconds int) time.Duration {
	if intervalSeconds <= 0 {
		return 30 * time.Second // default for testing
	}
	return time.Duration(intervalSeconds) * time.Second
}
