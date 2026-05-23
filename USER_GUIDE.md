# kube-billing User Guide

Полное руководство по установке, настройке и использованию Kubernetes оператора kube-billing.

## Оглавление

1. [Требования](#требования)
2. [Быстрый старт](#быстрый-старт)
3. [Установка в кластер](#установка-в-кластер)
4. [Создание биллинг-планов](#создание-биллинг-планов)
5. [Управление подписками](#управление-подписками)
6. [Мониторинг](#мониторинг)
7. [Трассировка](#трассировка)
8. [События и аудит](#события-и-аудит)
9. [Production deployment](#production-deployment)
10. [Устранение неполадок](#устранение-неполадок)

---

## Требования

### Обязательные
- **Kubernetes**: v1.25+
- **kubectl**: v1.25+
- **Доступ к кластеру**: права на создание CRD, Deployment, RBAC

### Опциональные (для разработки)
- **Go**: v1.25.3+
- **Docker**: v17.03+
- **Kind**: для локального тестирования
- **Minikube**: для локального тестирования

### Для мониторинга
- **Prometheus**: для сбора метрик
- **Jaeger/Tempo**: для трассировки (OTLP HTTP endpoint)

---

## Быстрый старт

### 1. Проверка подключения к кластеру

```bash
kubectl cluster-info
kubectl get nodes
```

### 2. Установка оператора

```bash
# Клонирование репозитория
git clone https://github.com/example/kube-billing.git
cd kube-billing

# Установка CRD
make install

# Деплой контроллера
make deploy IMG=docker.io/example/kube-billing:latest
```

### 3. Проверка установки

```bash
# Проверка CRD
kubectl get crd | grep billing

# Проверка подов контроллера
kubectl get pods -n kube-billing-system

# Проверка логов
kubectl logs -n kube-billing-system -l control-plane=controller-manager -f
```

---

## Установка в кластер

### Вариант 1: Kustomize (рекомендуется)

```bash
# Установка CRD
kubectl apply -k config/crd

# Установка всего оператора (CRD + контроллер + RBAC + PDB + HPA)
kubectl apply -k config/default
```

### Вариант 2: YAML bundle

```bash
# Генерация полного манифеста
make build-installer IMG=docker.io/example/kube-billing:latest

# Установка
kubectl apply -f dist/install.yaml
```

### Вариант 3: Helm Chart

```bash
# Генерация Helm chart
kubebuilder edit --plugins=helm/v2-alpha --output-dir=charts

# Установка
helm install kube-billing ./charts/chart/ \
  --namespace kube-billing-system \
  --create-namespace

# Проверка статуса
helm status kube-billing -n kube-billing-system
```

### Проверка установки

```bash
# CRD должны быть установлены
kubectl get crd billingplans.billing.cloud-native.io
kubectl get crd subscriptions.billing.cloud-native.io

# Под контроллера должен быть Running
kubectl get pods -n kube-billing-system

# Метрики доступны
kubectl port-forward -n kube-billing-system svc/kube-billing-controller-manager-metrics-service 8080:8080
curl http://localhost:8080/metrics
```

---

## Создание биллинг-планов

### Базовый план

```yaml
# plan-basic.yaml
apiVersion: billing.cloud-native.io/v1alpha1
kind: BillingPlan
metadata:
  name: basic
  namespace: default
spec:
  price: "10.00"
  currency: USD
  billingPeriod: monthly
  requeueIntervalSeconds: 30  # 30 секунд для тестирования
  limits:
    apiCalls: 10000
```

```bash
kubectl apply -f plan-basic.yaml
```

### Премиум план

```yaml
# plan-pro.yaml
apiVersion: billing.cloud-native.io/v1alpha1
kind: BillingPlan
metadata:
  name: pro
  namespace: default
spec:
  price: "29.99"
  currency: USD
  billingPeriod: monthly
  requeueIntervalSeconds: 60
  limits:
    apiCalls: 100000
    storage: 10737418240  # 10 GB
```

```bash
kubectl apply -f plan-pro.yaml
```

### Проверка планов

```bash
# Список всех планов
kubectl get billingplans

# Детали плана
kubectl get billingplan pro -o yaml

# Статус плана (activeSubscriptions, totalRevenue)
kubectl get billingplan pro -o jsonpath='{.status}' | jq
```

---

## Управление подписками

### Создание подписки

```yaml
# subscription.yaml
apiVersion: billing.cloud-native.io/v1alpha1
kind: Subscription
metadata:
  name: user-123-sub
  namespace: default
spec:
  userId: user-123
  planRef: pro
```

```bash
kubectl apply -f subscription.yaml
```

### Проверка подписки

```bash
# Список подписок
kubectl get subscriptions

# Детали подписки
kubectl get subscription user-123-sub -o yaml

# Статус подписки (state, conditions, lastPayment)
kubectl get subscription user-123-sub -o jsonpath='{.status}' | jq

# Conditions подписки
kubectl get subscription user-123-sub -o jsonpath='{.status.conditions}' | jq
```

### Отмена подписки

```bash
kubectl delete subscription user-123-sub
```

> **Примечание**: При удалении активной подписки автоматически рассчитывается пропорциональный refund за неиспользованный период.

---

## Мониторинг

### Prometheus метрики

#### Доступ к метрикам

```bash
# Port-forward к metrics endpoint
kubectl port-forward -n kube-billing-system svc/kube-billing-controller-manager-metrics-service 8080:8080

# Получение метрик
curl http://localhost:8080/metrics
```

#### Основные метрики

| Метрика | Тип | Описание |
|---------|-----|----------|
| `billing_active_subscriptions` | Gauge | Количество активных подписок |
| `billing_revenue_total` | Counter | Общая сумма обработанных платежей |
| `billing_payment_failures` | Counter | Количество неудачных платежей |
| `controller_runtime_reconcile_total` | Counter | Количество реконсиляций |
| `controller_runtime_reconcile_errors_total` | Counter | Количество ошибок реконсиляции |

#### Пример запроса метрик

```bash
# Активные подписки
curl -s http://localhost:8080/metrics | grep billing_active_subscriptions

# Общий revenue
curl -s http://localhost:8080/metrics | grep billing_revenue_total

# Ошибки платежей
curl -s http://localhost:8080/metrics | grep billing_payment_failures
```

#### Интеграция с Prometheus

Добавьте в `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'kube-billing'
    kubernetes_sd_configs:
      - role: endpoints
        namespaces:
          names:
            - kube-billing-system
    relabel_configs:
      - source_labels: [__meta_kubernetes_service_name]
        regex: 'kube-billing-controller-manager-metrics-service'
        action: keep
      - source_labels: [__meta_kubernetes_endpoint_port_name]
        regex: 'metrics'
        action: keep
```

#### Grafana Dashboard

Импортируйте dashboard ID `12345` (пример) или создайте свой с панелями:
- Active Subscriptions (Gauge)
- Revenue Over Time (Graph)
- Payment Failures (Stat + Alert)
- Reconciliation Rate (Graph)

---

## Трассировка

### Настройка OpenTelemetry

#### 1. Развёртывание Jaeger

```yaml
# jaeger.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: jaeger
  namespace: observability
spec:
  replicas: 1
  selector:
    matchLabels:
      app: jaeger
  template:
    metadata:
      labels:
        app: jaeger
    spec:
      containers:
        - name: jaeger
          image: jaegertracing/all-in-one:1.53
          ports:
            - containerPort: 16686  # UI
            - containerPort: 4318   # OTLP HTTP
---
apiVersion: v1
kind: Service
metadata:
  name: jaeger
  namespace: observability
spec:
  ports:
    - name: ui
      port: 16686
      targetPort: 16686
    - name: otlp
      port: 4318
      targetPort: 4318
  selector:
    app: jaeger
```

```bash
kubectl apply -f jaeger.yaml
```

#### 2. Настройка контроллера

```bash
# Установка endpoint для трассировки
kubectl set env deployment/kube-billing-controller-manager \
  -n kube-billing-system \
  OTEL_EXPORTER_OTLP_ENDPOINT=jaeger.observability.svc.cluster.local:4318

# Перезапуск контроллера
kubectl rollout restart deployment/kube-billing-controller-manager -n kube-billing-system
```

#### 3. Проверка трассировки

```bash
# Port-forward к Jaeger UI
kubectl port-forward -n observability svc/jaeger 16686:16686
```

Откройте `http://localhost:16686` и выберите сервис `kube-billing`.

#### Доступные Span'ы

| Span | Описание |
|------|----------|
| `ReconcileSubscription` | Полный цикл реконсиляции подписки |
| `ProcessPayment` | Обработка платежа |
| `MapPlanToSubscriptions` | Маппинг плана на подписки |

---

## События и аудит

### Просмотр событий

```bash
# Все события в namespace
kubectl get events -n default --sort-by='.lastTimestamp'

# События для конкретной подписки
kubectl get events -n default --field-selector involvedObject.name=user-123-sub

# События в реальном времени
kubectl get events -n default --watch
```

### Типы событий

| Тип | Reason | Описание |
|-----|--------|----------|
| Normal | PaymentProcessed | Успешный платёж |
| Warning | PaymentFailed | Ошибка платежа |
| Normal | FinalBilling | Финальный биллинг (refund) |
| Normal | SubscriptionActivated | Активация подписки |

### Пример вывода событий

```bash
$ kubectl get events -n default --field-selector involvedObject.name=user-123-sub

LAST SEEN   TYPE      REASON             OBJECT                    MESSAGE
2m          Normal    SubscriptionActivated   subscription/user-123-sub   Subscription successfully activated
1m          Normal    PaymentProcessed        subscription/user-123-sub   Payment of 29.99 USD processed successfully
30s         Normal    PaymentProcessed        subscription/user-123-sub   Payment of 29.99 USD processed successfully
```

---

## Production Deployment

### Применение production конфигурации

```bash
# Применение всех ресурсов (CRD + контроллер + PDB + HPA)
kubectl apply -k config/default
```

### Проверка HPA

```bash
# Статус HPA
kubectl get hpa -n kube-billing-system

# Детали HPA
kubectl describe hpa kube-billing-controller-manager-hpa -n kube-billing-system
```

### Проверка PDB

```bash
# Статус PDB
kubectl get pdb -n kube-billing-system

# Детали PDB
kubectl describe pdb kube-billing-controller-manager-pdb -n kube-billing-system
```

### Настройка ресурсов

Отредактируйте `config/manager/manager.yaml`:

```yaml
resources:
  limits:
    cpu: 500m      # Увеличить для высокой нагрузки
    memory: 512Mi  # Увеличить для большой памяти
  requests:
    cpu: 100m
    memory: 128Mi
```

### Масштабирование

```bash
# Ручное масштабирование
kubectl scale deployment/kube-billing-controller-manager \
  -n kube-billing-system \
  --replicas=3

# Автоматическое (через HPA)
kubectl autoscale deployment/kube-billing-controller-manager \
  -n kube-billing-system \
  --min=1 --max=5 --cpu-percent=80
```

---

## Устранение неполадок

### Контроллер не запускается

```bash
# Проверка логов
kubectl logs -n kube-billing-system -l control-plane=controller-manager

# Проверка RBAC
kubectl auth can-i --list -n kube-billing-system \
  --as=system:serviceaccount:kube-billing-system:kube-billing-controller-manager
```

### Подписка не активируется

```bash
# Проверка conditions
kubectl get subscription <name> -o jsonpath='{.status.conditions}' | jq

# Проверка событий
kubectl get events --field-selector involvedObject.name=<name>

# Проверка существования BillingPlan
kubectl get billingplan <planRef>
```

### Метрики не доступны

```bash
# Проверка сервиса метрик
kubectl get svc -n kube-billing-system | grep metrics

# Port-forward и проверка
kubectl port-forward -n kube-billing-system svc/kube-billing-controller-manager-metrics-service 8080:8080
curl http://localhost:8080/metrics
```

### Трассировка не работает

```bash
# Проверка переменной окружения
kubectl get deployment -n kube-billing-system \
  kube-billing-controller-manager \
  -o jsonpath='{.spec.template.spec.containers[0].env}' | jq

# Проверка доступности Jaeger
kubectl run test --rm -it --image=curlimages/curl --restart=Never -- \
  curl -v http://jaeger.observability.svc.cluster.local:4318
```

### Billing цикл не срабатывает

```bash
# Проверка requeueIntervalSeconds
kubectl get billingplan <name> -o jsonpath='{.spec.requeueIntervalSeconds}'

# Проверка логов контроллера
kubectl logs -n kube-billing-system -l control-plane=controller-manager | grep -i "billing\|reconcile"

# Проверка nextBilling времени
kubectl get subscription <name> -o jsonpath='{.status.nextBilling}'
```

---

## Примеры использования

### Сценарий 1: Создание SaaS биллинга

```bash
# Создание планов
kubectl apply -f - <<EOF
apiVersion: billing.cloud-native.io/v1alpha1
kind: BillingPlan
metadata:
  name: free
spec:
  price: "0.00"
  currency: USD
  billingPeriod: monthly
  requeueIntervalSeconds: 30
---
apiVersion: billing.cloud-native.io/v1alpha1
kind: BillingPlan
metadata:
  name: starter
spec:
  price: "9.99"
  currency: USD
  billingPeriod: monthly
  requeueIntervalSeconds: 30
---
apiVersion: billing.cloud-native.io/v1alpha1
kind: BillingPlan
metadata:
  name: business
spec:
  price: "49.99"
  currency: USD
  billingPeriod: monthly
  requeueIntervalSeconds: 30
EOF

# Создание подписок для пользователей
for i in {1..10}; do
  kubectl apply -f - <<EOF
apiVersion: billing.cloud-native.io/v1alpha1
kind: Subscription
metadata:
  name: user-$i-sub
spec:
  userId: user-$i
  planRef: starter
EOF
done
```

### Сценарий 2: Миграция пользователей на другой план

```bash
# Обновление подписки
kubectl patch subscription user-1-sub \
  --type='merge' \
  -p '{"spec":{"planRef":"business"}}'

# Проверка обновления
kubectl get subscription user-1-sub -o jsonpath='{.status}'
```

### Сценарий 3: Массовая отмена подписок

```bash
# Удаление всех подписок в namespace
kubectl delete subscription --all -n default

# Проверка refund событий
kubectl get events -n default | grep FinalBilling
```

---

## Справочник команд

### CRD операции

```bash
# Список CRD
kubectl get crd | grep billing

# Описание CRD
kubectl describe crd billingplans.billing.cloud-native.io

# Редактирование CRD (не рекомендуется)
kubectl edit crd billingplans.billing.cloud-native.io
```

### Ресурсы оператора

```bash
# Список всех ресурсов kube-billing
kubectl get all -n kube-billing-system

# Логи контроллера
kubectl logs -n kube-billing-system -l control-plane=controller-manager -f

# Перезапуск контроллера
kubectl rollout restart deployment/kube-billing-controller-manager -n kube-billing-system
```

### Отладка

```bash
# Включить debug логи (изменить deployment)
kubectl set env deployment/kube-billing-controller-manager \
  -n kube-billing-system \
  LOG_LEVEL=debug

# Экспорт конфигурации
kubectl get all -n kube-billing-system -o yaml > backup.yaml
```

---

## Поддержка

- **Документация**: [README.md](README.md)
- **Issues**: https://github.com/example/kube-billing/issues
- **Обсуждения**: https://github.com/example/kube-billing/discussions

## Лицензия

Apache License 2.0 - см. [LICENSE](LICENSE)
