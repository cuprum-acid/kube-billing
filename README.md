# kube-billing

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Report Card](https://goreportcard.com/badge/github.com/example/kube-billing)](https://goreportcard.com/report/github.com/example/kube-billing)

A Kubernetes operator for managing billing plans and subscriptions using Custom Resource Definitions (CRDs).

## Description

kube-billing provides native Kubernetes resources for managing tenant billing:

- **BillingPlan**: Define pricing tiers with configurable billing cycles
- **Subscription**: Link users to billing plans with automatic payment processing

Features:
- ✅ Automatic recurring billing with configurable intervals
- ✅ Pro-rated refunds on subscription cancellation
- ✅ Prometheus metrics for monitoring (active subscriptions, revenue, failures)
- ✅ OpenTelemetry tracing for debugging
- ✅ Kubernetes Events for audit trail
- ✅ Status Conditions for real-time state visibility
- ✅ Production-ready deployment (HPA, PDB, resource limits)

## API Reference

### BillingPlan

Defines a billing plan with pricing and billing cycle configuration.

```yaml
apiVersion: billing.cloud-native.io/v1alpha1
kind: BillingPlan
metadata:
  name: pro-plan
  namespace: default
spec:
  price: "19.99"              # Required: decimal with up to 2 places
  currency: USD               # Required: USD|EUR|RUB|KZT|GBP|JPY|CNY
  billingPeriod: monthly      # Required: hourly|daily|weekly|monthly|yearly
  requeueIntervalSeconds: 30  # Optional: billing cycle in seconds (default: 30)
  limits:                     # Optional: resource limits
    apiCalls: 10000
    storage: 10737418240
status:
  activeSubscriptions: 5      # Number of active subscriptions
  totalRevenue: "99.95"       # Total revenue generated
  conditions:
  - type: Available
    status: "True"
    reason: HasActiveSubscriptions
    message: "Plan has 5 active subscriptions"
    lastTransitionTime: "2026-05-23T10:00:00Z"
```

### Subscription

Links a user to a billing plan and tracks payment status.

```yaml
apiVersion: billing.cloud-native.io/v1alpha1
kind: Subscription
metadata:
  name: user-123-sub
  namespace: default
spec:
  userId: user-123            # Required: alphanumeric, _, - (max 256 chars)
  planRef: pro-plan           # Required: reference to BillingPlan name
status:
  state: Active               # Active|PaymentError|Error
  lastPayment: "2026-05-23T10:00:00Z"
  nextBilling: "2026-06-22T10:00:00Z"
  observedGeneration: 1
  conditions:
  - type: Active
    status: "True"
    reason: PaymentProcessed
    message: "Payment processed successfully"
    lastTransitionTime: "2026-05-23T10:00:00Z"
  - type: BillingPlanNotFound
    status: "False"
    reason: BillingPlanFound
    message: "Referenced BillingPlan exists"
    lastTransitionTime: "2026-05-23T10:00:00Z"
  - type: PaymentError
    status: "False"
    reason: PaymentSuccess
    message: "Last payment was successful"
    lastTransitionTime: "2026-05-23T10:00:00Z"
```

## Validation Rules

### BillingPlan
| Field | Validation |
|-------|------------|
| `price` | Pattern: `^\d+(\.\d{1,2})?$`, Length: 1-20 |
| `currency` | Enum: USD, EUR, RUB, KZT, GBP, JPY, CNY |
| `billingPeriod` | Enum: hourly, daily, weekly, monthly, yearly |
| `requeueIntervalSeconds` | Range: 1 - 31536000 (1 year) |

### Subscription
| Field | Validation |
|-------|------------|
| `userId` | Pattern: `^[a-zA-Z0-9_-]+$`, Length: 1-256 |
| `planRef` | Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`, Length: 1-253 |

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `billing_active_subscriptions` | Gauge | Current number of active subscriptions |
| `billing_revenue_total` | Counter | Total revenue processed |
| `billing_payment_failures` | Counter | Number of failed payment attempts |

Prometheus endpoint: `:8080/metrics`

## Events

The controller emits Kubernetes Events for auditing:

| Event Type | Reason | Description |
|------------|--------|-------------|
| Normal | PaymentProcessed | Successful payment processed |
| Warning | PaymentFailed | Payment processing failed |
| Normal | FinalBilling | Pro-rated refund calculated on deletion |

## Getting Started

### Prerequisites
- go version v1.24.6+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/kube-billing:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/kube-billing:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/kube-billing:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/kube-billing/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
kubebuilder edit --plugins=helm/v2-alpha
```

2. See that a chart was generated under 'dist/chart', and users
can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart
using the same command above to sync the latest changes. Furthermore,
if you create webhooks, you need to use the above command with
the '--force' flag and manually ensure that any custom configuration
previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
is manually re-applied afterwards.

## License

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
