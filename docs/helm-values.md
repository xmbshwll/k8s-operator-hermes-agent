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

## Values

### `image`

```yaml
image:
  repository: ghcr.io/xmbshwll/k8s-operator-hermes-agent
  tag: v<version>
  pullPolicy: IfNotPresent
```

Controls the operator controller-manager image, not the Hermes runtime image used by `HermesAgent.spec.image`.

### `leaderElection`

```yaml
leaderElection:
  enabled: true
```

Leave enabled for normal clustered installs.

### `serviceAccount`

```yaml
serviceAccount:
  create: true
  name: ""
```

If `create: false`, set `name` to an existing ServiceAccount.

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
```

Enables the controller-runtime metrics Service and RBAC protection.
If you plan to scrape metrics in production, pair this with the secure metrics and Prometheus guidance below.

### `webhook`

```yaml
webhook:
  enabled: true
  port: 9443
```

Controls admission webhook serving.
The chart is designed for webhook-enabled installs.

### `certManager`

```yaml
certManager:
  enabled: true
```

Controls chart-managed webhook certificate resources.

Important behavior:
- the supported production path is `webhook.enabled=true` and `certManager.enabled=true`
- if `certManager.enabled=false`, the chart will not render webhook resources that depend on webhook TLS
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
