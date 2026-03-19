# Helm values reference

The operator chart lives in `charts/chart/` and is the recommended installation path for published releases.

## Prerequisites

- Helm 4
- cert-manager installed in the target cluster for the default webhook-enabled install path

## Install

```sh
helm install k8s-operator-hermes-agent \
  oci://ghcr.io/xmbshwll/charts/k8s-operator-hermes-agent \
  --version <version> \
  --namespace k8s-operator-hermes-agent-system \
  --create-namespace
```

## Upgrade

Helm installs the chart CRD on first install from `crds/`, but Helm does not perform normal CRD upgrades from that directory on later `helm upgrade` runs.
The supported upgrade path is therefore explicit and two-step:

1. apply the matching release CRD bundle
2. run `helm upgrade`

```sh
kubectl apply -f \
  https://github.com/xmbshwll/k8s-operator-hermes-agent/releases/download/v<version>/hermesagents.hermes.nous.ai-crd.yaml

helm upgrade k8s-operator-hermes-agent \
  oci://ghcr.io/xmbshwll/charts/k8s-operator-hermes-agent \
  --version <version> \
  --namespace k8s-operator-hermes-agent-system
```

Before upgrading:
- check the release notes
- confirm cert-manager is healthy if webhooks are enabled
- verify existing `HermesAgent` resources are healthy before changing the operator
- always apply the CRD bundle first, even when the schema change seems minor

Do not rely on plain `helm upgrade` by itself for CRD changes, or you can end up with a new controller running against an old CRD schema.

Selector upgrade note:
- the chart now scopes controller-manager selectors with `app.kubernetes.io/instance` so multiple releases in the same namespace do not cross-select each other's pods
- if you are upgrading from an older chart revision that used broader selectors, the Deployment selector change is immutable in Kubernetes
- for that one-time transition, use `helm upgrade --force` after applying the CRD bundle first so Helm can replace the Deployment cleanly

## Values

The chart now includes `values.schema.json`, so Helm validates the supported values surface before rendering or installing.

### `image`

```yaml
image:
  repository: ghcr.io/xmbshwll/k8s-operator-hermes-agent
  tag: v<version>
  pullPolicy: IfNotPresent
```

Controls the operator controller-manager image, not the Hermes runtime image used by `HermesAgent.spec.image`.

### `replicaCount`

```yaml
replicaCount: 1
```

Controls the controller-manager Deployment replica count.
If you increase this above `1`, leave `leaderElection.enabled=true`.

### `imagePullSecrets`

```yaml
imagePullSecrets:
  - name: ghcr-pull-secret
```

Optional pull secrets for the controller-manager pod.

### `podDisruptionBudget`

```yaml
podDisruptionBudget:
  enabled: false
  maxUnavailable: 1
```

Optional `PodDisruptionBudget` for the controller-manager Deployment.
The chart only renders it when both of these are true:
- `podDisruptionBudget.enabled=true`
- `replicaCount > 1`

That keeps the default single-replica install safe and avoids creating a misleading PDB that would only block voluntary disruption without improving availability.
For HA installs, keep leader election enabled and raise `replicaCount` above `1` before turning the PDB on.
The chart currently uses `maxUnavailable` because it fits the controller-manager leader-election model and scales cleanly across small multi-replica installs.

### `leaderElection`

```yaml
leaderElection:
  enabled: true
```

Leave enabled for normal clustered installs, especially when `replicaCount > 1`.

### `serviceAccount`

```yaml
serviceAccount:
  create: true
  name: ""
```

If `create: false`, set `name` to an existing ServiceAccount.

### `podAnnotations` and `podLabels`

```yaml
podAnnotations:
  cluster-autoscaler.kubernetes.io/safe-to-evict: "true"

podLabels:
  environment: production
```

Adds extra metadata to the controller-manager pod without affecting the built-in selectors.

### `podSecurityContext` and `containerSecurityContext`

```yaml
podSecurityContext:
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault

containerSecurityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - ALL
  readOnlyRootFilesystem: true
```

Lets you override the default hardened security settings when the target cluster needs a small adjustment.

### Scheduling knobs

```yaml
nodeSelector:
  kubernetes.io/os: linux

tolerations: []

affinity: {}

topologySpreadConstraints: []
```

Use these to steer the controller-manager pod onto the right nodes for production installs.

### `resources`

```yaml
resources:
  limits:
    cpu: 500m
    memory: 128Mi
  requests:
    cpu: 10m
    memory: 64Mi
```

Container resource requests and limits for the operator itself.

### `metrics`

```yaml
metrics:
  enabled: true
  service:
    annotations: {}
    labels: {}
  serviceMonitor:
    enabled: false
    namespace: ""
    additionalLabels: {}
    interval: ""
    scrapeTimeout: ""
    tlsConfig:
      insecureSkipVerify: false
      serverName: ""
  certManager:
    enabled: false
  networkPolicy:
    enabled: false
    namespaceSelector:
      matchLabels:
        metrics: enabled
```

`metrics.enabled` exposes the controller-runtime metrics Service and RBAC protection.
Additional chart-native options now cover:
- `metrics.service.annotations` and `metrics.service.labels` for service discovery metadata
- `metrics.serviceMonitor.*` for Prometheus Operator integration
- `metrics.certManager.enabled` for cert-manager-backed metrics serving certificates mounted into the manager pod
- `metrics.networkPolicy.*` for optional ingress protection on the metrics endpoint

`metrics.serviceMonitor.enabled=true` only renders a `ServiceMonitor` when the `monitoring.coreos.com/v1` API exists in the cluster at render time.

#### TLS expectations for metrics scraping

The metrics endpoint is served over HTTPS.

For simple setups, `metrics.serviceMonitor.tlsConfig.*` lets you point Prometheus at an already-trusted serving certificate.
The secure default is `metrics.serviceMonitor.tlsConfig.insecureSkipVerify=false`, which means your Prometheus stack must trust the serving certificate presented by the controller.

For chart-managed cert-manager parity with the kustomize flow, enable both:
- `certManager.enabled=true`
- `metrics.certManager.enabled=true`

That path makes the chart:
- issue a metrics serving certificate with cert-manager
- mount that certificate into the controller-manager container via `--metrics-cert-path=/tmp/k8s-metrics-server/metrics-certs`
- configure the generated `ServiceMonitor` to use the matching CA, cert, and key secret references when `metrics.serviceMonitor.enabled=true`

If you use the cert-manager-backed metrics path and also set `metrics.serviceMonitor.namespace` to a namespace different from the chart release namespace, the generated TLS secret references will not automatically move with it. Keep the `ServiceMonitor` in the release namespace for the chart-managed TLS path, or manage the TLS references yourself.

If your environment does not trust the serving certificate yet, either:
- configure Prometheus trust correctly, optionally setting `metrics.serviceMonitor.tlsConfig.serverName` when you need explicit hostname verification, or
- set `metrics.serviceMonitor.tlsConfig.insecureSkipVerify=true` only as a deliberate local-development shortcut

### `webhook`

```yaml
webhook:
  enabled: true
  port: 9443
  networkPolicy:
    enabled: false
```

Controls admission webhook serving.
The chart is designed for webhook-enabled installs.
`webhook.networkPolicy.enabled=true` adds an ingress NetworkPolicy for webhook traffic.
When `webhook.enabled=false`, the chart also sets `ENABLE_WEBHOOKS=false` in the controller-manager container so the process does not register or serve admission webhooks at runtime.

### `certManager`

```yaml
certManager:
  enabled: true
```

Controls chart-managed webhook certificate resources.

Important behavior:
- the supported production path is `webhook.enabled=true` and `certManager.enabled=true`
- if `certManager.enabled=false`, the chart will not render webhook resources that depend on webhook TLS and will also set `ENABLE_WEBHOOKS=false` so the manager does not keep webhook runtime behavior enabled by itself
- in practice, disabling cert-manager means disabling the default admission-webhook install path too

## Secure metrics and Prometheus

The repository includes secure metrics wiring and optional Prometheus artifacts in the kustomize config.
For operator users, the main points are:
- metrics are served over HTTPS when enabled
- the chart installs the metrics Service and RBAC support
- if you want Prometheus scraping in production, use the repository’s Prometheus manifests and TLS guidance rather than `insecureSkipVerify` shortcuts

See:
- `config/prometheus/monitor.yaml`
- `config/prometheus/monitor_tls_patch.yaml`

## Install notes

### Namespace choice

Default namespace:

```text
k8s-operator-hermes-agent-system
```

You can install into another namespace, but keep cert-manager and webhook certificate wiring in mind.
The chart now release-scopes controller-manager selectors, so multiple releases in the same namespace do not share metrics, webhook, or NetworkPolicy targeting by accident.

### After install

Recommended checks:

```sh
kubectl get pods -n k8s-operator-hermes-agent-system
kubectl get crd hermesagents.hermes.nous.ai
helm status k8s-operator-hermes-agent -n k8s-operator-hermes-agent-system
```

Then create a `HermesAgent` from one of the sample manifests in `config/samples/`.

## Uninstall

```sh
helm uninstall k8s-operator-hermes-agent -n k8s-operator-hermes-agent-system
```

The Helm uninstall path keeps the CRD by design.
Delete `HermesAgent` resources first if you want a clean operator removal.
