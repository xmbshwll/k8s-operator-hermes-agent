# Install and upgrade guide

This guide keeps the supported operator install paths in one place.
Use it when you want copy-pasteable commands for either a quick evaluation install or a production Helm install.

## Choose the right path

### Evaluation install

Use the published `install.yaml` bundle when you want the fastest way to try the operator on a cluster.
This path is good for:
- local evaluation
- short-lived test clusters
- quick validation that the controller starts and the CRD is present

### Production install

Use the published Helm chart when you want repeatable upgrades and chart-level configuration.
This is the primary supported release path for long-lived clusters.

## Common prerequisite

Both supported install paths enable admission webhooks by default.
That means the target cluster must already have cert-manager installed before you install or upgrade the operator.

The supported default is therefore:
- `webhook.enabled=true`
- `certManager.enabled=true`

You can disable webhooks for special cases, but that is not the primary documented production path.

The current primary API version is `hermes.nous.ai/v1`.
Releases still serve deprecated `v1alpha1` in the CRD for upgrade safety, but all new manifests and samples should use `v1`.

## Path A: quick evaluation install with the published bundle

```sh
kubectl apply -f \
  https://github.com/xmbshwll/k8s-operator-hermes-agent/releases/download/v<version>/install.yaml
```

This installs:
- the `HermesAgent` CRD
- the controller manager deployment
- admission webhook configuration
- RBAC for the controller
- the metrics service

Verify the install:

```sh
kubectl get pods -n k8s-operator-hermes-agent-system
kubectl get crd hermesagents.hermes.nous.ai
```

## Path B: production install with Helm

```sh
helm install k8s-operator-hermes-agent \
  oci://ghcr.io/xmbshwll/charts/k8s-operator-hermes-agent \
  --version <version> \
  --namespace k8s-operator-hermes-agent-system \
  --create-namespace
```

Verify the install:

```sh
kubectl get pods -n k8s-operator-hermes-agent-system
kubectl get crd hermesagents.hermes.nous.ai
helm status k8s-operator-hermes-agent -n k8s-operator-hermes-agent-system
```

## Helm upgrade sequence

Helm installs the chart CRD from `crds/` only on the first install.
It does **not** perform normal CRD upgrades from that directory on later `helm upgrade` runs.

The supported upgrade sequence is therefore always:

1. apply the matching CRD bundle
2. run `helm upgrade`

### Published release upgrade

```sh
kubectl apply -f \
  https://github.com/xmbshwll/k8s-operator-hermes-agent/releases/download/v<version>/hermesagents.hermes.nous.ai-crd.yaml

helm upgrade k8s-operator-hermes-agent \
  oci://ghcr.io/xmbshwll/charts/k8s-operator-hermes-agent \
  --version <version> \
  --namespace k8s-operator-hermes-agent-system
```

### Local chart upgrade during development

```sh
make helm-upgrade-crd-first \
  HELM_RELEASE=k8s-operator-hermes-agent \
  HELM_NAMESPACE=k8s-operator-hermes-agent-system \
  HELM_CHART_DIR=./charts/chart
```

That helper target:
- regenerates the local CRD bundle
- applies the CRD bundle first
- runs `helm upgrade --install` with your configured chart reference

If you want to use a published CRD bundle instead of the local one, override `CRD_BUNDLE`.
For example:

```sh
make helm-upgrade-crd-first \
  CRD_BUNDLE=https://github.com/xmbshwll/k8s-operator-hermes-agent/releases/download/v<version>/hermesagents.hermes.nous.ai-crd.yaml \
  HELM_CHART_DIR=oci://ghcr.io/xmbshwll/charts/k8s-operator-hermes-agent \
  HELM_EXTRA_ARGS="--version <version>"
```

## Before upgrading

- read the release notes
- confirm cert-manager is healthy
- confirm existing `HermesAgent` resources are healthy
- apply the CRD bundle first even when the schema change looks small

Do not rely on plain `helm upgrade` alone for CRD changes.
That can leave a newer controller running against an older CRD schema.

## Validation status

The repository CI already validates the explicit CRD-first Helm upgrade flow and a previous-release upgrade path.
That upgrade ordering is not just documentation; it is part of the tested release workflow.
