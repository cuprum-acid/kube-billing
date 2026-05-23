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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// BillingPlanSpec defines the desired state of BillingPlan
type BillingPlanSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// Price is the price amount (e.g., "19.99")
	// +kubebuilder:validation:Pattern=`^\d+(\.\d{1,2})?$`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=20
	// +required
	Price string `json:"price"`

	// Currency is the currency code (e.g., USD, EUR, RUB)
	// +kubebuilder:validation:Enum=USD;EUR;RUB;KZT;GBP;JPY;CNY
	// +required
	Currency string `json:"currency"`

	// BillingPeriod is the billing period (e.g., hourly, daily, weekly, monthly, yearly)
	// +kubebuilder:validation:Enum=hourly;daily;weekly;monthly;yearly
	// +required
	BillingPeriod string `json:"billingPeriod"`

	// Limits define resource limits for this billing plan
	// +optional
	Limits map[string]int `json:"limits,omitempty"`

	// RequeueIntervalSeconds is the interval between billing cycles in seconds.
	// Default is 30 seconds for testing. Set to 2592000 for monthly billing (30 days).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=31536000
	// +kubebuilder:default=30
	// +optional
	RequeueIntervalSeconds int `json:"requeueIntervalSeconds,omitempty"`
}

// BillingPlanStatus defines the observed state of BillingPlan.
type BillingPlanStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the BillingPlan resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// activeSubscriptions is the number of active subscriptions using this plan
	// +optional
	ActiveSubscriptions int32 `json:"activeSubscriptions,omitempty"`

	// totalRevenue is the total revenue generated from this plan (in cents/smallest currency unit)
	// +optional
	TotalRevenue string `json:"totalRevenue,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// BillingPlan is the Schema for the billingplans API
type BillingPlan struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of BillingPlan
	// +required
	Spec BillingPlanSpec `json:"spec"`

	// status defines the observed state of BillingPlan
	// +optional
	Status BillingPlanStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// BillingPlanList contains a list of BillingPlan
type BillingPlanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []BillingPlan `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BillingPlan{}, &BillingPlanList{})
}
