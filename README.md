# Hermes Kubernetes Operator

A Kubernetes operator for running Hermes Agent as a stateful, long-lived gateway workload.

The operator manages a `HermesAgent` custom resource and reconciles the Kubernetes objects needed to run it safely in-cluster:
- a singleton `StatefulSet`
- persistent storage for `HERMES_HOME`
- generated or referenced configuration files
- optional `Service` and `NetworkPolicy` resources

This repository contains the **operator**. It does **not** build the Hermes runtime image for you. Each `HermesAgent` points at a separate Hermes container image through `spec.image`.

## Status

This is an MVP-focused operator. The first release is intentionally narrow:
- one `HermesAgent` resource kind
- one Hermes pod per resource
- persistent local state via PVC
- egress-first deployments
- minimal installation surface via Helm

See [`docs/architecture.md`](docs/architecture.md) for the design rationale and explicit v1 non-goals.

## How it works

A `HermesAgent` resource lets you declare:
- the Hermes runtime image to run
- Hermes `config.yaml` and `gateway.json`
- environment variables and secret references
- persistent storage settings
- resource requests and limits
- startup, readiness, and liveness probes
- optional service exposure and network policy

The controller then reconciles:
- `ConfigMap` resources for inline config
- a PVC for Hermes state when persistence is enabled
- a singleton `StatefulSet`
- an optional `Service`
- an optional egress-focused `NetworkPolicy`

## Prerequisites

- Go 1.24.6+
- Docker
- kubectl
- Access to a Kubernetes cluster
- Helm 4 for chart installation

## Key concepts

### Operator image vs Hermes runtime image

There are two different images involved:

1. **Operator image**
   - Built from this repository's `Dockerfile`
   - Runs the Kubernetes controller manager
   - Installed with Helm via `make helm-deploy`

2. **Hermes runtime image**
   - Referenced by each `HermesAgent` in `spec.image`
   - Runs the actual Hermes process inside the managed `StatefulSet`
   - Must contain a working `hermes` CLI

Do not point `spec.image` at the operator image. They serve different roles.

### Runtime contract for Hermes images

The operator expects the Hermes runtime image to:
- contain the `hermes` executable in `PATH`
- support running `hermes gateway`
- tolerate `HERMES_HOME=/data/hermes`
- write runtime state under `HERMES_HOME`
- run under a non-root security context
- support exec probes that use `bash -ec`

The controller mounts:
- `/data/hermes` for Hermes state
- `/tmp` as writable scratch space
- `config.yaml` at `/data/hermes/config.yaml` when provided
- `gateway.json` at `/data/hermes/gateway.json` when provided
- referenced secrets under `/var/run/hermes/secrets/<secret-name>`

## Install the operator

### 1. Install a published release with Helm

```sh
helm install k8s-operator-hermes-agent \
  oci://ghcr.io/xmbshwll/charts/k8s-operator-hermes-agent \
  --version 0.1.0 \
  --namespace k8s-operator-hermes-agent-system \
  --create-namespace
```

The published chart already points at the matching released controller image.
You do not need to build the operator image locally for a normal install.

### 2. Install a published release with the YAML bundle

```sh
kubectl apply -f \
  https://github.com/xmbshwll/k8s-operator-hermes-agent/releases/download/v0.1.0/install.yaml
```

This installs:
- the `HermesAgent` CRD
- the operator deployment
- RBAC for the controller
- the metrics service

### 3. Verify the operator

```sh
kubectl get pods -n k8s-operator-hermes-agent-system
kubectl get crd hermesagents.hermes.nous.ai
```

### 4. Build and install manually during development

```sh
make docker-build docker-push IMG=<registry>/k8s-operator-hermes-agent:<tag>
make helm-deploy IMG=<registry>/k8s-operator-hermes-agent:<tag>
```

By default this installs into `k8s-operator-hermes-agent-system`.
Override the namespace when needed:

```sh
make helm-deploy \
  IMG=<registry>/k8s-operator-hermes-agent:<tag> \
  HELM_NAMESPACE=<namespace>
```

## Deploy a sample HermesAgent

Sample manifests live under [`config/samples/`](config/samples/).

Start with the minimal sample at [`config/samples/hermes_v1alpha1_hermesagent.yaml`](config/samples/hermes_v1alpha1_hermesagent.yaml).
For other real deployment paths, see:
- [`config/samples/README.md`](config/samples/README.md)
- [`config/samples/hermes_v1alpha1_hermesagent_telegram.yaml`](config/samples/hermes_v1alpha1_hermesagent_telegram.yaml)
- [`config/samples/hermes_v1alpha1_hermesagent_ssh.yaml`](config/samples/hermes_v1alpha1_hermesagent_ssh.yaml)

Before applying the minimal sample, update the Hermes runtime image:

```yaml
spec:
  image:
    repository: ghcr.io/example/hermes-agent
    tag: gateway-core
```

Replace that placeholder with your real Hermes image.

Then apply the sample:

```sh
kubectl apply -k config/samples/
```

Check the resulting resources:

```sh
kubectl get hermesagents
kubectl get statefulsets,pvc,configmaps
kubectl describe hermesagent hermesagent-sample
```

Watch the managed pod:

```sh
kubectl get pods -w
```

## Configuration model

A `HermesAgent` supports two config files:
- `spec.config` → Hermes `config.yaml`
- `spec.gatewayConfig` → Hermes `gateway.json`

Each file can be supplied in one of two ways:

1. **Inline content** via `raw`
   - the controller creates a generated `ConfigMap`
   - changes trigger a `StatefulSet` rollout through a config hash annotation

2. **Existing ConfigMap reference** via `configMapRef`
   - the controller mounts the referenced key directly

Environment and secrets are handled separately:
- `spec.env` adds explicit environment variables
- `spec.envFrom` imports `ConfigMap` and `Secret` env sources
- `spec.secretRefs` mounts named secrets under `/var/run/hermes/secrets/`

## Persistence model

Hermes is treated as stateful.

By default the operator provisions a PVC and mounts it at `/data/hermes`, with `HERMES_HOME` set to `/data/hermes`.
This preserves Hermes state across pod restarts and reschedules.

You can disable persistence, but that switches Hermes state to `emptyDir`, which is appropriate only for disposable environments.

## Uninstall

Delete any `HermesAgent` resources you created:

```sh
kubectl delete -k config/samples/
```

Then uninstall the operator release.

If you installed with Helm:

```sh
make helm-uninstall
```

Helm uninstall keeps the CRD so existing custom resources are not removed unexpectedly.

If you installed from the published bundle, `kubectl delete -f .../install.yaml` also deletes the CRD.
Use that only if you want to remove the API entirely after deleting all `HermesAgent` resources.

## Development

Useful commands:

```sh
make test
make test-e2e
make lint-fix
make build-installer
make package-chart
```

`make test-e2e` creates a disposable Kind cluster, builds the operator image plus a lightweight Hermes-compatible runtime image, installs the operator, applies a sample `HermesAgent`, and validates readiness, PVC-backed persistence across restart, and config rollout behavior.

Helm chart location:
- `charts/chart/`

Install directly from the chart during development:

```sh
helm upgrade --install k8s-operator-hermes-agent ./charts/chart \
  --namespace k8s-operator-hermes-agent-system \
  --create-namespace \
  --set image.repository=<registry>/k8s-operator-hermes-agent \
  --set image.tag=<tag>
```

Package a release-style chart locally:

```sh
make package-chart \
  CHART_VERSION=0.1.0 \
  CHART_APP_VERSION=0.1.0 \
  CHART_IMAGE_REPOSITORY=ghcr.io/xmbshwll/k8s-operator-hermes-agent \
  CHART_IMAGE_TAG=v0.1.0
```

## Documentation

- [Architecture notes](docs/architecture.md)
- [Release workflow](docs/release.md)
- [Sample catalog](config/samples/README.md)
- [Minimal HermesAgent](config/samples/hermes_v1alpha1_hermesagent.yaml)

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
