# Hermes Operator Architecture

## Overview

The Hermes operator manages Hermes Agent as a Kubernetes-native, stateful gateway workload.
Its job is deliberately narrow: reconcile the resources needed to run one Hermes instance reliably per `HermesAgent` custom resource.

For each `HermesAgent`, the controller may create or manage:
- generated `ConfigMap` resources for inline file content
- a `PersistentVolumeClaim` for Hermes state when persistence is enabled
- a managed `StatefulSet`
- an optional `Service`
- an optional egress-focused `NetworkPolicy`
- an automatic `PodDisruptionBudget` when replicas are greater than `1`

## Why the workload still uses StatefulSet

Hermes is still treated as a workload with meaningful local runtime identity.

The operator keeps `StatefulSet` as the workload primitive because it preserves:
- stable pod identity
- explicit rollout controls such as `OnDelete` and RollingUpdate partitioning
- a clean distinction between singleton persistent workloads and stateless multi-replica workloads

Single replica remains the default because Hermes keeps important local runtime state such as:
- configuration files
- session and gateway state
- pid and status files used by probes
- other on-disk Hermes home data

## Multi-replica boundary

The operator now supports multi-replica HermesAgent workloads, but only for the stateless path.
That means:
- set `spec.replicas` greater than `1`
- set `spec.storage.persistence.enabled=false`
- use `spec.updateStrategy` when you want explicit StatefulSet rollout behavior

The operator intentionally rejects multi-replica specs that also ask for managed persistence.
It does not try to coordinate or bless shared Hermes state across replicas.

## Why persistent storage is the default

The controller sets `HERMES_HOME=/data/hermes` and mounts the Hermes state volume at `/data/hermes`.
By default that storage comes from a PVC.

Persistence is the default because Hermes writes state that should survive:
- pod restarts
- node drains
- controller rollouts
- config-triggered restarts

Without persistence, Hermes falls back to `emptyDir`, which is acceptable for disposable, HTTP-serving, or stateless multi-replica deployments.
For real singleton gateway workloads that need durable local state, losing that state on restart is the wrong default.

## Workload and filesystem contract

The managed Hermes container is expected to:
- have a working `hermes` CLI available in `PATH`
- support `hermes gateway`
- write runtime state beneath `HERMES_HOME`
- run as a non-root user
- tolerate a writable `/tmp`
- support exec probes that run via `bash -ec`

The repo includes a lightweight published-runtime contract check at `make test-runtime-contract`.
That smoke test is meant to fail fast when the published `ghcr.io/xmbshwll/hermes-agent-docker` image drifts away from the operator's minimum runtime assumptions, without trying to replace the fuller fake-runtime e2e coverage.

The operator provides these paths:
- `/data/hermes` — persistent or ephemeral state volume and `HERMES_HOME`
- `/data/hermes/config.yaml` — mounted Hermes config when provided
- `/data/hermes/gateway.json` — mounted gateway config when provided
- `/var/run/hermes/secrets/<name>` — mounted secret references
- `/tmp` — writable scratch space

## Configuration model

The operator keeps configuration intentionally simple.

### Main Hermes config

`spec.config` maps to `config.yaml`.

### Gateway config

`spec.gatewayConfig` maps to `gateway.json`.

### Allowed sources

Each config file can come from exactly one source:
- `raw` inline content
- `configMapRef` reference to an existing `ConfigMap` key
- `secretRef` reference to an existing `Secret` key

If inline content is used, the controller generates a dedicated `ConfigMap` and mounts it into the pod.
If a reference is used, the controller mounts the referenced key directly.

Referenced `ConfigMap` and `Secret` objects are watched by the controller.
Their current content is folded into the pod template hash so external updates trigger a deterministic reconcile and rollout.
This is especially important for `configMapRef` file mounts because they use `subPath`, which does not live-refresh in a running container.

The controller computes a config hash from:
- resolved file inputs
- referenced `ConfigMap` file content for `spec.config.configMapRef` and `spec.gatewayConfig.configMapRef`
- `spec.env`, including current data for `configMapKeyRef` and `secretKeyRef`
- `spec.envFrom` plus current data from referenced `ConfigMap` and `Secret` objects
- `spec.secretRefs` plus current data from referenced `Secret` objects
- `spec.fileMounts` plus current data from the selected referenced `ConfigMap` or `Secret` keys

That hash is added to the pod template so config changes roll the `StatefulSet` predictably.

## Secrets and environment variables

The operator separates file-like config from environment-driven config.

- `spec.env` adds explicit environment variables
- `spec.envFrom` imports environment values from `ConfigMap` or `Secret` sources
- `spec.secretRefs` mounts named secrets as files
- `spec.fileMounts` mounts projected ConfigMap or Secret files with optional key selection and file mode controls

This lets users keep credentials in Kubernetes `Secret` resources instead of embedding them into inline config blobs.

## Admission and defaulting model

`HermesAgent` uses admission webhooks for both mutating defaults and validating cross-field rules.
This keeps invalid specs out of the cluster instead of relying on the reconciler to notice bad input later.

The webhook is responsible for defaults that are awkward or impossible to express cleanly with OpenAPI markers alone, especially probe-profile defaults that differ between startup, readiness, and liveness.
It also enforces cross-field rules such as mutually exclusive config sources and complete object references.

## Probes and health model

The operator uses exec probes rather than HTTP probes for Hermes itself.

That is because Hermes does not expose a native Kubernetes-style readiness endpoint. Instead, probe logic checks:
- the Hermes pid file metadata and extracts the numeric `pid`
- the gateway state file reports `gateway_state: "running"`
- optionally, whether any `platforms.*.state` entry is actually `"connected"`

This keeps health checking close to Hermes' real runtime state instead of inventing a sidecar or synthetic HTTP server just for Kubernetes.

## Optional service exposure

Service creation is disabled by default.

That is intentional. Hermes gateway deployments are primarily egress-first, and many deployments do not need an inbound Kubernetes `Service` at all.
When users do need a service, they can opt in through `spec.service`.

That `Service` support is intentionally narrow in v1. It is meant to expose the managed Hermes pod when the runtime image already serves the interface you need. It is **not** a first-class ingress or API product layer, and the operator does not add an ingress, proxy, sidecar, or HTTP compatibility shim on top.

## Optional network policy

The operator can create an egress-focused `NetworkPolicy` when `spec.networkPolicy.enabled` is true.
The default policy shape allows:
- DNS
- HTTP
- HTTPS
- SSH when the resolved Hermes config declares an `ssh` terminal backend

The controller derives that effective backend from resolved `config.yaml` content, including referenced ConfigMaps when available. `spec.terminal.backend` only acts as a fallback when the config does not declare a backend.

Users can widen the generated policy with additional TCP and UDP egress ports, and they can optionally restrict non-DNS egress to explicit destination peers such as CIDR blocks or selector-based in-cluster targets. DNS stays destination-agnostic so name resolution keeps working without forcing users to model cluster DNS endpoints first.

If they need a substantially different policy shape than that generated allowlist model, the intended path is still to disable operator-managed NetworkPolicy generation and supply their own manifest.

## Security defaults

The managed Hermes pod is hardened with restricted-style defaults:
- non-root execution
- explicit UID/GID
- `seccompProfile: RuntimeDefault`
- dropped Linux capabilities
- `allowPrivilegeEscalation: false`
- `readOnlyRootFilesystem: true`
- `automountServiceAccountToken: false` by default
- separate writable `/tmp`
- a dedicated writable Hermes state volume at `/data/hermes`

The operator image itself also follows a locked-down container model.

## Day-2 feedback model

The operator is expected to be diagnosable with standard Kubernetes workflows.
That means status conditions are only part of the UX; the reconciler also emits focused Kubernetes events for important transitions and failures so `kubectl describe hermesagent <name>` is useful during incidents.

High-signal events are emitted for:
- invalid or unreadable configuration inputs
- PVC pending, lost, and bound transitions
- StatefulSet progress and readiness
- Service and NetworkPolicy conflicts or reconcile failures

## Install model

Operator installation is packaged as a Helm chart under `charts/chart/` and as a generated `install.yaml` bundle for release consumers.

### Helm chart behavior

The chart installs:
- the `HermesAgent` CRD on first install from `crds/`
- the controller deployment
- controller RBAC
- the metrics service
- mutating and validating admission webhooks when the webhook path is enabled
- cert-manager issuer and certificate resources when chart-managed TLS is enabled
- optional `ServiceMonitor` and ingress `NetworkPolicy` resources for the controller-manager endpoints

The published chart is the primary installation path, but Helm CRDs still follow normal Helm constraints: the CRD in `crds/` is install-time only.
For upgrades, the intended operational path is explicit and two-step:
1. apply the release CRD bundle first
2. then run `helm upgrade`

That flow keeps the controller and CRD schema aligned instead of assuming Helm will patch CRDs in place.

### Webhook and cert-manager model

The supported production path is still webhook-enabled and cert-manager-backed.
When `webhook.enabled=true` and `certManager.enabled=true`, the chart renders the webhook service, admission configurations, and serving certificate resources, and the manager process also keeps webhook runtime behavior enabled.

When the chart disables that path — either with `webhook.enabled=false` or `certManager.enabled=false` — the rendered webhook resources disappear and the manager container sets `ENABLE_WEBHOOKS=false` so the process does not keep serving or registering admission webhooks behind the scenes.

### Metrics and observability model

Metrics are served over HTTPS when enabled.
The chart always supports the controller-runtime metrics service and can optionally add:
- a `ServiceMonitor` when the Prometheus Operator API exists at render time
- ingress `NetworkPolicy` resources for metrics and webhook traffic
- cert-manager-backed metrics serving certificates when `metrics.certManager.enabled=true` together with `certManager.enabled=true`

That metrics TLS path mirrors the kustomize-based production guidance more closely: the chart can issue a dedicated metrics certificate, mount it into the controller-manager pod, and configure the generated `ServiceMonitor` to use the matching CA, cert, and key secret references.

### Release scoping

Chart-managed controller-manager selectors are release-scoped with `app.kubernetes.io/instance`.
That is an architectural choice, not just a cosmetic label convention: metrics discovery, webhook service targeting, and optional controller-manager `NetworkPolicy` resources should only ever target the pods from their own Helm release, even if multiple releases exist in the same namespace.

### Install-time values

The chart intentionally exposes install-time controls for operator concerns only, including:
- operator image repository, tag, and pull policy
- controller replica count and leader election
- service account creation or reuse
- pod metadata, scheduling, and security settings
- metrics enablement, scraping integration, ingress protection, and optional cert-manager-backed metrics TLS
- webhook enablement, ingress protection, and cert-manager-backed webhook certificates

The Hermes runtime image is not configured through the chart because it belongs to each `HermesAgent`, not to the operator installation.

For concrete install commands, supported values, and upgrade notes, see `README.md`, `docs/helm-values.md`, and `docs/release.md`.

## Supported scope vs example-only paths

The supported v1 product scope is:
- one or more Hermes pods per `HermesAgent`
- persistent-state singleton gateway management
- stateless multi-replica HermesAgent workloads with rollout controls and automatic `PodDisruptionBudget` generation
- operator-managed config, storage, probes, optional `Service`, and optional egress `NetworkPolicy`
- Service-based HTTP exposure for custom Hermes runtime images that serve HTTP under `hermes gateway`
- webhook-validated and defaulted CR instances

The following user-facing paths still exist only as examples, not as first-class supported product features:
- custom runtime images used behind Open WebUI
- plugin-delivery examples where the runtime image is responsible for plugin discovery and execution

Those examples are still useful, but they should be read as "the operator can deliver this workload shape" rather than "the operator guarantees this full end-to-end product behavior."

## Main design decisions

### Narrow API surface

The CRD focuses on the deployment concerns needed for a usable MVP:
- image selection
- config file sources
- environment injection
- persistence
- probes
- optional service and network policy

It does not attempt to model every Hermes capability as a first-class Kubernetes API field.

### No backward-looking compatibility layers

This project is product code, not a public SDK.
The design prefers the cleanest current model over preserving hypothetical legacy behavior.

The one deliberate exception is CRD version serving needed for safe API upgrades: `hermes.nous.ai/v1` is the storage and preferred version, while deprecated `hermes.nous.ai/v1alpha1` remains served so clusters with historical `storedVersions` can move forward cleanly.
That compatibility bridge exists to unblock upgrades, not to preserve parallel long-term product behavior.

### Egress-first assumptions

The operator assumes Hermes usually connects outward to external systems rather than serving cluster ingress traffic by default.
That keeps the default install smaller and easier to reason about.

## v1 non-goals

The following are explicitly out of scope for v1:
- shared persistent Hermes state across replicas
- autoscaling
- browser sidecars
- Docker-in-Docker terminal backends
- generic multi-tenant platform abstractions
- multiple CRDs for higher-level orchestration
- default ingress resources
- preserving compatibility with older deployment models

These are intentionally excluded so the first release stays focused on one clean path: install the operator, create one `HermesAgent`, and run Hermes reliably with persistent state.

For HTTP-serving workloads, the clean supported boundary is now: the operator manages the Hermes pods plus their Service, while ingress and higher-level HTTP platform concerns stay outside the operator.

## Relationship between operator and Hermes runtime

A common source of confusion is image ownership:

- this repository builds the **operator image**
- each `HermesAgent` references a separate **Hermes runtime image** in `spec.image`

That separation is deliberate. The operator manages Kubernetes resources; it does not embed the Hermes runtime into the controller process.

## Files to know

- `api/v1/hermesagent_types.go` — CRD schema
- `internal/controller/hermesagent_resources.go` — reconciled resource shapes
- `config/samples/hermes_v1_hermesagent.yaml` — sample custom resource
- `charts/chart/` — operator installation chart
- `README.md` — user-facing install and usage guide
