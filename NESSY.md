# kube-billing Project Context

Kubernetes Operator для управления биллинг-планами и подписками через Custom Resource Definitions (CRD). Часть бакалаврской диссертации **"Exploring Kubernetes as a Platform for Business Logic Using Custom Resource Definitions and the Operator Pattern"**.

---

## Технологии

| Компонент | Технология |
|-----------|------------|
| **Язык** | Go 1.25.3 |
| **Framework** | controller-runtime v0.23.1 (Kubebuilder v4.13.0) |
| **CRD API** | billing.cloud-native.io/v1alpha1 |
| **Метрики** | Prometheus (client_golang v1.23.2) |
| **Трассировка** | OpenTelemetry (OTLP over HTTP, localhost:4318) |
| **Линтер** | golangci-lint v2.8.0 (с кастомной конфигурацией) |
| **Тесты** | Ginkgo v2 + Gomega + envtest |
| **E2E** | Kind (изолированный кластер `kube-billing-test-e2e`) |

---

## CRD Ресурсы

### BillingPlan (`billingplan_types.go`)

```yaml
apiVersion: billing.cloud-native.io/v1alpha1
kind: BillingPlan
metadata:
  name: basic
spec:
  price: "10.00"           # string с валидацией Pattern=`^\d+(\.\d{1,2})?$`
  currency: USD
  billingPeriod: 30d
  limits:                  # optional
    apiCalls: 10000
```

**Статус:** Не используется (поле `status` определено, но не заполняется контроллером).

### Subscription (`subscription_types.go`)

```yaml
apiVersion: billing.cloud-native.io/v1alpha1
kind: Subscription
metadata:
  name: sub-user1
spec:
  userId: user1            # ID пользователя
  planRef: basic           # Ссылка на BillingPlan по имени
status:
  state: Active            # Active, Error
  lastPayment: "2026-05-23T10:00:00Z"
  nextBilling: "2026-06-22T10:00:00Z"
  observedGeneration: 1
  conditions: []           # metav1.Condition list
```

**Finalizer:** `billing.cloud-native.io/finalizer` — для финального биллинга перед удалением.

---

## Архитектура контроллера

### SubscriptionReconciler (`internal/controller/subscription_controller.go`)

**Reconcile логика:**

1. **Обработка удаления** — если `DeletionTimestamp.IsZero()`:
   - Запуск финального биллинга (TODO)
   - Удаление finalizer
2. **Добавление finalizer** — если отсутствует
3. **Проверка BillingPlan** — если план не найден → `State=Error`, requeue через 1 минуту
4. **Активация подписки** — если `State == ""`:
   - Инкремент метрики `ActiveSubscriptions`
   - Установка `LastPayment=now`, `NextBilling=now+30d`
5. **Периодический биллинг** — если `State == Active` и `now > NextBilling`:
   - Инкремент метрики `RevenueTotal`
   - Обновление `LastPayment`, `NextBilling`
   - **RequeueAfter: 30 секунд** (временно для тестирования)

**Watches:**
- `Subscription` (основной ресурс)
- `BillingPlan` → через `mapPlanToSubscriptions` (триггерит реконсиляцию всех подписок на этот план)

**Трассировка:**
- Span `ReconcileSubscription` — на всю реконсиляцию
- Span `ProcessPayment` — на обработку платежа
- Span `MapPlanToSubscriptions` — на маппинг плана на подписки

### Метрики (`internal/controller/metrics.go`)

| Метрика | Тип | Описание |
|---------|-----|----------|
| `billing_active_subscriptions` | Gauge | Количество активных подписок |
| `billing_revenue_total` | Counter | Общая сумма обработанных платежей |
| `billing_payment_failures` | Counter | Количество неудачных платежей (не используется) |

---

## Структура проекта

```
kube-billing/
├── cmd/
│   └── main.go                 # Entry point, инициализация менеджера, трассировки
├── api/v1alpha1/
│   ├── billingplan_types.go    # CRD BillingPlan spec/status
│   ├── subscription_types.go   # CRD Subscription spec/status
│   └── groupversion_info.go    # GroupVersionInfo для scheme
├── internal/controller/
│   ├── subscription_controller.go # Reconcile логика
│   ├── metrics.go              # Prometheus метрики
│   ├── tracing.go              # OpenTelemetry инициализация
│   └── suite_test.go           # Ginkgo test suite setup
├── config/
│   ├── crd/bases/              # Сгенерированные CRD (НЕ редактировать)
│   ├── default/                # Kustomize конфигурация для деплоя
│   ├── manager/                # Deployment манифест контроллера
│   ├── rbac/                   # ServiceAccount, Role, RoleBinding
│   └── samples/                # Примеры CR (billingplan.yaml, subscription.yaml)
├── test/e2e/                   # E2E тесты (требуют Kind)
├── hack/
│   └── boilerplate.go.txt      # Лицензионный заголовок для генерации кода
├── Makefile                    # Основные цели: build, run, test, deploy
├── Dockerfile                  # Multi-stage build (builder + distroless)
├── go.mod / go.sum             # Go модуль зависимости
├── .golangci.yml               # Конфигурация golangci-lint
└── PROJECT                     # Kubebuilder metadata (НЕ редактировать)
```

---

## Команды разработки

### Базовые

```bash
# Запуск локально (требует kubeconfig)
make run

# Сборка бинарника
make build

# Запуск тестов (unit + integration с envtest)
make test

# Линтинг
make lint
make lint-fix          # С авто-исправлениями
```

### Генерация кода

```bash
# После изменения *_types.go или маркеров
make manifests         # Генерация CRD, RBAC, WebhookConfiguration
make generate          # Генерация DeepCopy методов
```

### Деплой в кластер

```bash
# Сборка и пуш образа
export IMG=<registry>/kube-billing:tag
make docker-build docker-push IMG=$IMG

# Для Kind (локальный кластер)
kind load docker-image $IMG --name kube-billing-test-e2e

# Установка CRD
make install

# Деплой контроллера
make deploy IMG=$IMG

# Создание примеров
kubectl apply -k config/samples/

# Удаление
kubectl delete -k config/samples/
make uninstall
make undeploy
```

### E2E тесты

```bash
# Запуск (создаёт Kind кластер автоматически)
make test-e2e

# Ручная настройка/удаление кластера
make setup-test-e2e
make cleanup-test-e2e
```

---

## Конфигурация линтера

**Файл:** `.golangci.yml`

**Включённые линтеры:**
- `copyloopvar`, `dupl`, `errcheck`, `ginkgolinter`, `goconst`, `gocyclo`
- `govet`, `ineffassign`, `lll`, `modernize`, `misspell`, `nakedret`
- `prealloc`, `revive`, `staticcheck`, `unconvert`, `unparam`, `unused`
- `logcheck` (кастомный, для проверки логирования Kubernetes)

**Исключения:**
- `api/*` — исключено из `lll` (длинные строки в CRD)
- `internal/*` — исключено из `dupl`, `lll` (допустимы дубликаты в контроллере)

**Форматтеры:** `gofmt`, `goimports`

---

## Observability

### Трассировка

**Инициализация:** `controller.InitTracer()` в `main.go`

**Экспорт:** OTLP over HTTP на `localhost:4318` (небезопасный, для разработки)

**Требуемый сервис:** Jaeger или другой OTLP collector для сбора трейсов

**Span'ы:**
- `ReconcileSubscription` — корневой span реконсиляции
- `ProcessPayment` — обработка платежа
- `MapPlanToSubscriptions` — маппинг BillingPlan на Subscription

### Метрики

**Endpoint:** `:8080` (настраивается через `--metrics-bind-address`)

**Сборщик:** Prometheus (через `metrics-server` controller-runtime)

**RBAC:** Требуется доступ к `/metrics` с аутентификацией (настраивается в `config/rbac/`)

---

## Тестирование

### Unit/Integration тесты

**Фреймворк:** Ginkgo v2 + Gomega

**Среда:** envtest (запускает etcd + kube-apiserver локально)

**Файл:** `internal/controller/suite_test.go` (setup), `*_test.go` (тесты)

**Запуск:** `make test`

### E2E тесты

**Среда:** Kind кластер (`kube-billing-test-e2e`)

**Файл:** `test/e2e/e2e_suite_test.go`, `test/e2e/e2e_test.go`

**Запуск:** `make test-e2e`

**Требования:**
- Установлен `kind`
- Docker daemon запущен
- Образ контроллера собран и загружен в Kind

---

## Известные особенности реализации

1. **Billing цикл:** Временно установлен `RequeueAfter: 30 * time.Second` для демонстрации (вместо 30 дней).

2. **Финальный биллинг:** В обработке удаления есть комментарий `// здесь будет финальный биллинг` — функционал не реализован.

3. **Платежный gateway:** Обработка платежей симулируется (инкремент метрики `RevenueTotal`).

4. **Статус BillingPlan:** Не используется — контроллер не обновляет `status` у BillingPlan.

5. **Метрика PaymentFailures:** Определена, но не инкрементируется в коде.

6. **Tracer endpoint:** Жёстко задан `localhost:4318` — для работы требуется локальный OTLP collector.

---

## Ссылки

- **Kubebuilder Book:** https://book.kubebuilder.io
- **controller-runtime:** https://pkg.go.dev/sigs.k8s.io/controller-runtime
- **CRD спецификация:** `api/v1alpha1/*_types.go`
- **Примеры CR:** `config/samples/`, `plan.yaml`, `subscription.yaml`
