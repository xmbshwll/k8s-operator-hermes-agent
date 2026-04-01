# Supported features

This document defines the current product boundary for the Hermes Kubernetes operator.
It is the canonical answer to "is this supported?"

## Supported in v1

| Area | Status | Notes |
| --- | --- | --- |
| One `HermesAgent` CRD | Supported | The operator manages a single resource kind for Hermes workloads. |
| One Hermes pod per `HermesAgent` | Supported | The managed workload is a singleton `StatefulSet`. |
| Persistent Hermes state | Supported | PVC-backed `HERMES_HOME` is the default. |
| Inline or referenced config files | Supported | `spec.config` and `spec.gatewayConfig` support `raw`, `configMapRef`, and `secretRef`. |
| Env and mounted secret/config inputs | Supported | `env`, `envFrom`, `secretRefs`, and `fileMounts` are part of the supported API. File delivery is supported; what the runtime image does with those files is runtime-specific. |
| Probe management | Supported | Startup, readiness, and liveness probes are managed by the operator. |
| Optional `Service` | Supported | This is the supported HTTP exposure path for a single Hermes pod when the runtime image already serves the required interface. |
| Optional egress `NetworkPolicy` | Supported | The generated policy is intentionally simple and egress-focused. |
| Admission defaulting and validation | Supported | Invalid specs should be rejected before reconcile. |
| Helm chart and generated install bundle | Supported | These are the supported operator install paths. |

## Example-only paths

These flows are present in docs or samples, but they are not first-class product guarantees of the operator itself.

| Area | Status | Why |
| --- | --- | --- |
| API server sample | Supported | Demonstrates the supported Service-based HTTP exposure path for a custom Hermes runtime image that serves HTTP under `hermes gateway`. |
| Open WebUI backend sample | Example-only | Uses the supported Service-based backend exposure path, but the external Open WebUI integration remains outside the operator scope. |
| Plugin execution or auto-discovery from mounted plugin bundles | Example-only | The operator can deliver files, but plugin discovery and execution remain runtime-image behavior. |

## Explicitly out of scope for v1

| Area | Status | Notes |
| --- | --- | --- |
| Multi-replica Hermes workloads | Out of scope | The operator manages a singleton `StatefulSet` today. |
| Autoscaling | Out of scope | No HPA or operator-driven scaling model is provided. |
| Default ingress resources | Out of scope | The supported HTTP path is Service-based; ingress remains user-managed. |
| Browser sidecars | Out of scope | Not part of the current product shape. |
| Docker-in-Docker terminal backends | Out of scope | Not supported by the operator. |
| Generic multi-tenant platform abstractions | Out of scope | The operator focuses on one Hermes workload per CR. |
| Higher-level orchestration CRDs | Out of scope | No fleet, workspace, or multi-resource orchestration API is provided. |

## How to read samples

- Samples under `config/samples/` show how to shape a `HermesAgent` resource.
- A sample is **supported** only when it stays inside the v1 product boundary above.
- A sample is **example-only** when it depends on external systems or runtime-specific behavior that the operator does not manage as part of the supported path.

For the supported HTTP-serving model, see `docs/http-exposure.md`.

If a future release expands scope, update this file first and then align the README, architecture doc, samples, and tests.
