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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	billingv1alpha1 "github.com/example/kube-billing/api/v1alpha1"
)

var _ = Describe("Subscription Controller", func() {
	var (
		ctx       context.Context
		plan      *billingv1alpha1.BillingPlan
		sub       *billingv1alpha1.Subscription
		planName  string
		subName   string
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = "default"
		planName = "test-plan-" + randomString()
		subName = "test-sub-" + randomString()

		plan = &billingv1alpha1.BillingPlan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      planName,
				Namespace: namespace,
			},
			Spec: billingv1alpha1.BillingPlanSpec{
				Price:                  "10.00",
				Currency:               "USD",
				BillingPeriod:          "monthly",
				RequeueIntervalSeconds: 30,
			},
		}
		Expect(k8sClient.Create(ctx, plan)).To(Succeed())
	})

	AfterEach(func() {
		if sub != nil {
			_ = k8sClient.Delete(ctx, sub)
		}
		_ = k8sClient.Delete(ctx, plan)
	})

	Context("Subscription Activation", func() {
		It("should activate subscription when BillingPlan exists", func() {
			By("Creating a Subscription")
			sub = &billingv1alpha1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      subName,
					Namespace: namespace,
				},
				Spec: billingv1alpha1.SubscriptionSpec{
					UserID:  "user1",
					PlanRef: planName,
				},
			}
			Expect(k8sClient.Create(ctx, sub)).To(Succeed())

			By("Reconciling the subscription multiple times")
			controllerReconciler := &SubscriptionReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(100),
			}

			// First reconcile: add finalizer
			_, _ = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      subName,
					Namespace: namespace,
				},
			})

			// Second reconcile: activation
			_, _ = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      subName,
					Namespace: namespace,
				},
			})

			By("Verifying subscription is activated")
			Eventually(func(g Gomega) {
				updated := &billingv1alpha1.Subscription{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: subName, Namespace: namespace}, updated)).To(Succeed())
				g.Expect(updated.Status.State).To(Equal("Active"))
				g.Expect(updated.Status.LastPayment).ToNot(BeZero())
				g.Expect(updated.Status.NextBilling).ToNot(BeZero())
			}, time.Second*10, time.Millisecond*500).Should(Succeed())
		})

		It("should add finalizer to subscription", func() {
			By("Creating a Subscription")
			sub = &billingv1alpha1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      subName,
					Namespace: namespace,
				},
				Spec: billingv1alpha1.SubscriptionSpec{
					UserID:  "user1",
					PlanRef: planName,
				},
			}
			Expect(k8sClient.Create(ctx, sub)).To(Succeed())

			By("Reconciling")
			controllerReconciler := &SubscriptionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, _ = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: subName, Namespace: namespace},
			})

			By("Verifying finalizer is added")
			Eventually(func(g Gomega) {
				updated := &billingv1alpha1.Subscription{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: subName, Namespace: namespace}, updated)).To(Succeed())
				g.Expect(updated.Finalizers).To(ContainElement("billing.cloud-native.io/finalizer"))
			}, time.Second*10, time.Millisecond*500).Should(Succeed())
		})
	})

	Context("BillingPlan Not Found", func() {
		It("should set subscription state to Error when BillingPlan is missing", func() {
			By("Creating a Subscription with non-existent plan reference")
			subNameNotFound := "test-sub-notfound-" + randomString()
			sub = &billingv1alpha1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      subNameNotFound,
					Namespace: namespace,
				},
				Spec: billingv1alpha1.SubscriptionSpec{
					UserID:  "user1",
					PlanRef: "non-existent-plan",
				},
			}
			Expect(k8sClient.Create(ctx, sub)).To(Succeed())

			By("Reconciling multiple times to trigger status update")
			controllerReconciler := &SubscriptionReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(100),
			}
			// First reconcile: add finalizer
			_, _ = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: subNameNotFound, Namespace: namespace},
			})
			// Second reconcile: plan lookup -> Error state
			_, _ = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: subNameNotFound, Namespace: namespace},
			})

			By("Verifying subscription state is Error")
			Eventually(func(g Gomega) {
				updated := &billingv1alpha1.Subscription{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: subNameNotFound, Namespace: namespace}, updated)).To(Succeed())
				g.Expect(updated.Status.State).To(Equal("Error"))
			}, time.Second*10, time.Millisecond*500).Should(Succeed())
		})
	})

	Context("Periodic Billing", func() {
		It("should process recurring payment after billing interval", func() {
			By("Creating a Subscription")
			subNameBilling := "test-sub-billing-" + randomString()
			sub = &billingv1alpha1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      subNameBilling,
					Namespace: namespace,
				},
				Spec: billingv1alpha1.SubscriptionSpec{
					UserID:  "user1",
					PlanRef: planName,
				},
			}
			Expect(k8sClient.Create(ctx, sub)).To(Succeed())

			By("Activating subscription with multiple reconcile calls")
			controllerReconciler := &SubscriptionReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(100),
			}

			// First reconcile: add finalizer
			_, _ = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: subNameBilling, Namespace: namespace},
			})

			// Second reconcile: activation
			_, _ = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: subNameBilling, Namespace: namespace},
			})

			By("Waiting for activation")
			Eventually(func(g Gomega) {
				updated := &billingv1alpha1.Subscription{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: subNameBilling, Namespace: namespace}, updated)).To(Succeed())
				g.Expect(updated.Status.State).To(Equal("Active"))
			}, time.Second*10, time.Millisecond*500).Should(Succeed())

			By("Simulating time passage by updating NextBilling to the past")
			// Push NextBilling into the past to trigger an immediate billing cycle
			updatedSub := &billingv1alpha1.Subscription{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: subNameBilling, Namespace: namespace}, updatedSub)).To(Succeed())
			updatedSub.Status.NextBilling = metav1.NewTime(time.Now().Add(-1 * time.Hour))
			Expect(k8sClient.Status().Update(ctx, updatedSub)).To(Succeed())

			By("Reconciling again to trigger billing")
			_, _ = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: subNameBilling, Namespace: namespace},
			})

			By("Verifying payment was processed")
			Eventually(func(g Gomega) {
				updated := &billingv1alpha1.Subscription{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: subNameBilling, Namespace: namespace}, updated)).To(Succeed())
				g.Expect(updated.Status.LastPayment).ToNot(BeZero())
				g.Expect(updated.Status.NextBilling.Time).To(BeTemporally(">", time.Now()))
			}, time.Second*10, time.Millisecond*500).Should(Succeed())
		})
	})

	Context("Finalizer on Deletion", func() {
		It("should run final billing logic before deletion", func() {
			By("Creating and activating a Subscription")
			sub = &billingv1alpha1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      subName,
					Namespace: namespace,
				},
				Spec: billingv1alpha1.SubscriptionSpec{
					UserID:  "user1",
					PlanRef: planName,
				},
			}
			Expect(k8sClient.Create(ctx, sub)).To(Succeed())

			By("Activating subscription")
			controllerReconciler := &SubscriptionReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(100),
			}
			_, _ = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: subName, Namespace: namespace},
			})

			By("Waiting for finalizer to be added")
			Eventually(func(g Gomega) {
				updated := &billingv1alpha1.Subscription{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: subName, Namespace: namespace}, updated)).To(Succeed())
				g.Expect(updated.Finalizers).To(ContainElement("billing.cloud-native.io/finalizer"))
			}, time.Second*10, time.Millisecond*500).Should(Succeed())

			By("Deleting the subscription")
			Expect(k8sClient.Delete(ctx, sub)).To(Succeed())

			By("Reconciling to process deletion")
			_, _ = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: subName, Namespace: namespace},
			})

			By("Verifying subscription is deleted")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: subName, Namespace: namespace}, &billingv1alpha1.Subscription{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}, time.Second*10, time.Millisecond*500).Should(Succeed())
		})
	})

	Context("BillingPlan Change Triggers Subscription Reconcile", func() {
		It("should reconcile subscriptions when BillingPlan is updated", func() {
			By("Creating a Subscription")
			subNamePlanChange := "test-sub-planchange-" + randomString()
			sub = &billingv1alpha1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      subNamePlanChange,
					Namespace: namespace,
				},
				Spec: billingv1alpha1.SubscriptionSpec{
					UserID:  "user1",
					PlanRef: planName,
				},
			}
			Expect(k8sClient.Create(ctx, sub)).To(Succeed())

			By("Activating subscription")
			controllerReconciler := &SubscriptionReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(100),
			}

			// First reconcile: add finalizer
			_, _ = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: subNamePlanChange, Namespace: namespace},
			})

			// Second reconcile: activation
			_, _ = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: subNamePlanChange, Namespace: namespace},
			})

			By("Updating the BillingPlan")
			updatedPlan := &billingv1alpha1.BillingPlan{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: namespace}, updatedPlan)).To(Succeed())
			updatedPlan.Spec.Price = "15.00"
			Expect(k8sClient.Update(ctx, updatedPlan)).To(Succeed())

			By("Verifying subscription remains active after plan change")
			Eventually(func(g Gomega) {
				updated := &billingv1alpha1.Subscription{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: subNamePlanChange, Namespace: namespace}, updated)).To(Succeed())
				g.Expect(updated.Status.State).To(Equal("Active"))
			}, time.Second*10, time.Millisecond*500).Should(Succeed())
		})
	})
})

// randomString generates a random string for unique resource names
func randomString() string {
	return randomStringWithLength(8)
}

func randomStringWithLength(n int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyz0123456789")
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[RandomInt(len(letters))]
	}
	return string(b)
}
