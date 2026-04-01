# Hermes Kubernetes Operator

A Kubernetes operator for running [Hermes Agent](https://github.com/NousResearch/hermes-agent) as a stateful, long-lived gateway workload.

The operator manages a [Hermes Agent](https://github.com/NousResearch/hermes-agent) custom resource and reconciles the Kubernetes objects needed to run it safely in-cluster:
- a singleton `StatefulSet`
- persistent storage for `HERMES_HOME`
- generated or referenced configuration files
- optional `Service` and `NetworkPolicy` resources

This repository contains the **operator**. It does **not** build the Hermes runtime image for you. Each `HermesAgent` points at a separate Hermes container image through `spec.image`.

## Status

This operator is intentionally narrow in scope for its first production-ready release.
The supported product shape today is:
- one `HermesAgent` resource kind
- one Hermes pod per resource
- persistent local state via PVC by default
- egress-first deployments
- operator-managed config, storage, probe, `Service`, and egress `NetworkPolicy` resources for that single Hermes instance

The operator does **not** currently claim first-class support for:
- multi-replica Hermes workloads
- autoscaling
- default ingress generation
- a built-in HTTP API / Open WebUI product path

Those HTTP-facing examples remain custom-runtime examples, not supported product features of the operator itself.

See [`docs/supported-features.md`](docs/supported-features.md) for the support matrix and [`docs/architecture.md`](docs/architecture.md) for the design rationale and explicit v1 non-goals.

## How it works

A `HermesAgent` resource lets you declare:
- the Hermes runtime image to run
- Hermes `config.yaml` and `gateway.json`
- environment variables and file/secret references
- workload-level image pull secrets, pod labels/annotations, ServiceAccount identity, and pod placement controls
- persistent storage settings
- resource requests and limits
- startup, readiness, and liveness probes
- optional service exposure with distinct service and target ports, plus egress NetworkPolicy generation with extra TCP/UDP ports and optional destination allowlists for newer workflows

The controller then reconciles:
- `ConfigMap` resources for inline config
- a PVC for Hermes state when persistence is enabled
- a singleton `StatefulSet`
- an optional `Service`
- an optional egress-focused `NetworkPolicy`, with configurable extra TCP/UDP ports and optional destination allowlists when the defaults are too narrow

## Prerequisites

- Go 1.25.3+
- Docker
- kubectl
- Access to a Kubernetes cluster
- Helm 4 for chart installation and the bundled Makefile workflow
- cert-manager in the target cluster when admission webhooks are enabled

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

The repository now includes a lightweight published-runtime smoke check:

```sh
make test-runtime-contract
```

That check validates the published `ghcr.io/xmbshwll/hermes-agent-docker` image against the minimum contract above by running it as the operator would: non-root, with `HERMES_HOME=/data/hermes`, mounted config files, and runtime-state file emission. It is intentionally a small contract check, not a full product e2e workflow.

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
  --version <version> \
  --namespace k8s-operator-hermes-agent-system \
  --create-namespace
```

The published chart already points at the matching released controller image.
You do not need to build the operator image locally for a normal install.

Published installs enable admission webhooks and require cert-manager to already be installed in the cluster so webhook certificates can be issued and injected.

### 2. Install a published release with the YAML bundle

```sh
kubectl apply -f \
  https://github.com/xmbshwll/k8s-operator-hermes-agent/releases/download/v<version>/install.yaml
```

This installs:
- the `HermesAgent` CRD
- the operator deployment
- admission webhook configuration
- RBAC for the controller
- the metrics service

For chart configuration, install notes, and upgrade guidance, see [docs/helm-values.md](docs/helm-values.md).
For Helm upgrades, apply the release CRD bundle first and then run `helm upgrade`; the chart CRD in `crds/` is install-time only.

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
For webhook-enabled installs, make sure cert-manager is already running in the cluster.
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
- [`config/samples/hermes_v1alpha1_hermesagent_secret_config.yaml`](config/samples/hermes_v1alpha1_hermesagent_secret_config.yaml)
- [`config/samples/hermes_v1alpha1_hermesagent_ssh.yaml`](config/samples/hermes_v1alpha1_hermesagent_ssh.yaml)
- [`config/samples/hermes_v1alpha1_hermesagent_api_server.yaml`](config/samples/hermes_v1alpha1_hermesagent_api_server.yaml)
- [`config/samples/hermes_v1alpha1_hermesagent_openwebui.yaml`](config/samples/hermes_v1alpha1_hermesagent_openwebui.yaml)
- [`config/samples/hermes_v1alpha1_hermesagent_plugins.yaml`](config/samples/hermes_v1alpha1_hermesagent_plugins.yaml)

The API server and Open WebUI samples rely on the existing optional `Service`, but they are only valid when you provide a custom Hermes runtime image that already serves the expected HTTP interface on port `8080` while running under `hermes gateway`.

Those two samples are **example-only** today. They demonstrate how to point the operator at a custom HTTP-serving runtime image, but they are not a claim that the operator provides a supported built-in API-serving or Open WebUI integration path.

The minimal sample already points at the published Hermes runtime image from [`xmbshwll/hermes-agent-docker`](https://github.com/xmbshwll/hermes-agent-docker):

```yaml
spec:
  image:
    repository: ghcr.io/xmbshwll/hermes-agent-docker
    tag: v2026.3.30
```

If you need a different Hermes build, replace that image reference before applying the sample.

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

`kubectl get hermesagents` now surfaces the phase, ready replicas, persistence state, managed Service name, and managed PVC name directly from status so common rollout and storage issues are visible without drilling into owned resources first.

Watch the managed pod:

```sh
kubectl get pods -w
```

For day-2 debugging, start with:

```sh
kubectl describe hermesagent <name>
```

The operator emits focused events for invalid config, missing refs, PVC state changes, rollout progress, readiness, and same-name Service or NetworkPolicy conflicts.

## Configuration model

A `HermesAgent` supports two config files:
- `spec.config` → Hermes `config.yaml`
- `spec.gatewayConfig` → Hermes `gateway.json`

Each file can be supplied in one of three ways:

1. **Inline content** via `raw`
   - the controller creates a generated `ConfigMap`
   - changes trigger a `StatefulSet` rollout through a config hash annotation

2. **Existing ConfigMap reference** via `configMapRef`
   - the controller mounts the referenced key directly
   - referenced `ConfigMap` changes trigger a reconcile and `StatefulSet` rollout so the subPath-mounted file is refreshed safely

3. **Existing Secret reference** via `secretRef`
   - the controller mounts the referenced key directly from a `Secret`
   - referenced `Secret` changes trigger a reconcile and `StatefulSet` rollout the same way

Environment and mounted files are handled separately:
- `spec.env` adds explicit environment variables
- `spec.envFrom` imports `ConfigMap` and `Secret` env sources
- `spec.secretRefs` keeps the simple legacy secret-bundle path under `/var/run/hermes/secrets/<name>`
- `spec.fileMounts` mounts a projected `ConfigMap` or `Secret` at an explicit path, with optional key selection and file mode controls for cases like plugins, SSH material, prompts, or certificates; the operator only delivers those files, and the runtime image still decides what to do with them

Workload placement, registry auth, and workload identity are also configured per `HermesAgent`:
- `spec.imagePullSecrets` applies to the managed Hermes pod when the runtime image lives in a private registry
- `spec.serviceAccountName` lets the managed Hermes pod use its own Kubernetes identity without changing the operator controller's ServiceAccount
- `spec.automountServiceAccountToken` controls whether that workload identity token is mounted automatically; the operator defaults it to `false`
- `spec.nodeSelector`, `spec.tolerations`, `spec.affinity`, and `spec.topologySpreadConstraints` steer the managed Hermes pod onto the right nodes without affecting the operator deployment

`config.yaml` is the source of truth for the effective terminal backend whenever it declares `terminal.backend`. The controller derives operator-side wiring such as generated SSH egress rules from the resolved config content and only falls back to `spec.terminal.backend` when the config omits a backend. The operator only has SSH-specific behavior today; all other backend values are treated generically.

Referenced `ConfigMap` and `Secret` content is part of the pod template hash.
That means changes to `spec.config.configMapRef`, `spec.config.secretRef`, `spec.gatewayConfig.configMapRef`, `spec.gatewayConfig.secretRef`, `spec.env[].valueFrom`, `spec.envFrom`, `spec.secretRefs`, and `spec.fileMounts` roll the Hermes pod deterministically instead of relying on live volume refresh behavior.

## Admission and defaulting

`HermesAgent` now uses admission webhooks for both defaulting and validation.
That means invalid specs are rejected on create and update instead of being accepted and then failing later in reconcile status.

The webhook currently enforces and/or defaults:
- `raw` and `configMapRef` are mutually exclusive for `spec.config` and `spec.gatewayConfig`
- referenced config keys must include both `name` and `key`
- `spec.env[].valueFrom`, `spec.envFrom`, `spec.secretRefs`, `spec.fileMounts`, and `spec.imagePullSecrets` must use complete references
- file mounts must use exactly one source, an absolute `mountPath`, and unique mount paths; projected items must use unique keys, unique relative output paths, and valid file modes
- storage sizes must be valid Kubernetes quantities greater than zero
- enabled services must use a positive port
- runtime defaults for mode, image tag/pull policy, persistence, service settings, network policy, and probe profiles

For the full CRD field reference, see [docs/api-reference.md](docs/api-reference.md).

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

If you applied sample manifests directly with `kubectl apply -f config/samples/<file>.yaml`, remove those exact files directly as well. `kubectl delete -k config/samples/` only cleans up the minimal sample referenced by `config/samples/kustomization.yaml`.

## Development

Useful commands:

```sh
make test
make test-e2e
make lint-fix
make build-installer
make package-chart
```

`make test-e2e` creates a disposable Kind cluster, builds the operator image plus a lightweight Hermes-compatible runtime image, installs the operator, applies a sample `HermesAgent`, and validates readiness, PVC-backed persistence across restart, webhook rejection, referenced-config rollouts, secret-driven rollouts, and optional resource behavior.

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
  CHART_VERSION=<version> \
  CHART_APP_VERSION=<version> \
  CHART_IMAGE_REPOSITORY=ghcr.io/xmbshwll/k8s-operator-hermes-agent \
  CHART_IMAGE_TAG=v<version>
```

## Documentation

- [Architecture notes](docs/architecture.md)
- [API reference](docs/api-reference.md)
- [Helm values and upgrade notes](docs/helm-values.md)
- [Troubleshooting guide](docs/troubleshooting.md)
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
