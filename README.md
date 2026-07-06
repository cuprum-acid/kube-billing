# kube-billing

A Kubernetes operator for managing billing plans and subscriptions using Custom Resource Definitions (CRDs).

## Features

- **Billing Plans**: Define subscription plans with pricing, currency, and billing periods
- **Subscription Management**: Link users to billing plans with automatic payment processing
- **Recurring Billing**: Configurable billing cycles (hourly, daily, weekly, monthly, yearly)
- **Metrics**: Prometheus metrics for active subscriptions, revenue, and payment failures
- **Tracing**: OpenTelemetry distributed tracing support
- **Audit**: Kubernetes Events for billing operations

## Quick Start

### Prerequisites

- Go 1.25.3+
- Docker 17.03+
- Kubernetes 1.25+ cluster
- kubectl 1.25+

### Deploy to Cluster

```bash
# Build and push image
export IMG=<registry>/kube-billing:tag
make docker-build docker-push IMG=$IMG

# Install CRDs
make install

# Deploy controller
make deploy IMG=$IMG

# Apply sample resources
kubectl apply -k config/samples/
```

### Run Locally

```bash
make run
```

## Usage

### Create a Billing Plan

```yaml
apiVersion: billing.cloud-native.io/v1alpha1
kind: BillingPlan
metadata:
  name: pro-plan
spec:
  price: "19.99"
  currency: USD
  billingPeriod: monthly
  limits:
    apiCalls: 10000
```

```bash
kubectl apply -f plan.yaml
```

### Create a Subscription

```yaml
apiVersion: billing.cloud-native.io/v1alpha1
kind: Subscription
metadata:
  name: user-123-sub
spec:
  userId: user-123
  planRef: pro-plan
```

```bash
kubectl apply -f subscription.yaml
```

### View Resources

```bash
# List all subscriptions
kubectl get subscriptions

# Get subscription details
kubectl get subscription user-123-sub -o yaml

# View billing plan status
kubectl get billingplan pro-plan -o yaml
```

### Cancel a Subscription

```bash
kubectl delete subscription user-123-sub
```

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `billing_active_subscriptions` | Gauge | Current number of active subscriptions |
| `billing_revenue_total` | Counter | Total revenue processed |
| `billing_payment_failures` | Counter | Number of failed payment attempts |

Endpoint: `:8080/metrics`

## Development

```bash
# Build
make build

# Run tests
make test

# Lint
make lint
make lint-fix

# Generate CRDs and code
make manifests generate
```

## License

MIT License
