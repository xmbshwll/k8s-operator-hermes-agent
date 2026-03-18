# Hermes Operator Architecture

## Overview

The Hermes operator manages Hermes Agent as a Kubernetes-native, stateful gateway workload.
Its job is deliberately narrow: reconcile the resources needed to run one Hermes instance reliably per `HermesAgent` custom resource.

For each `HermesAgent`, the controller may create or manage:
- generated `ConfigMap` resources for inline file content
- a `PersistentVolumeClaim` for Hermes state
- a singleton `StatefulSet`
- an optional `Service`
- an optional egress-focused `NetworkPolicy`

## Why a singleton StatefulSet

Hermes is treated as a stateful process, not a horizontally scalable stateless service.

The operator uses a `StatefulSet` with `replicas: 1` because Hermes keeps important local runtime state such as:
- configuration files
- session and gateway state
- pid and status files used by probes
- other on-disk Hermes home data

A singleton `Deployment` would be possible, but `StatefulSet` makes the workload shape explicit:
- stable pod identity
- stable storage attachment behavior
- a clear model that Hermes is not intended for multi-replica fan-out in v1

This choice keeps the design honest. The operator does not pretend Hermes is safely scalable across multiple identical pods when the runtime state says otherwise.

## Why persistent storage is the default

The controller sets `HERMES_HOME=/data/hermes` and mounts the Hermes state volume at `/data/hermes`.
By default that storage comes from a PVC.

Persistence is the default because Hermes writes state that should survive:
- pod restarts
- node drains
- controller rollouts
- config-triggered restarts

Without persistence, Hermes falls back to `emptyDir`, which is acceptable only for disposable or test deployments.
For real gateway workloads, losing local state on restart is the wrong default.

## Workload and filesystem contract

The managed Hermes container is expected to:
- have a working `hermes` CLI available in `PATH`
- support `hermes gateway`
- write runtime state beneath `HERMES_HOME`
- run as a non-root user
- tolerate a writable `/tmp`
- support exec probes that run via `bash -ec`

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

That hash is added to the pod template so config changes roll the `StatefulSet` predictably.

## Secrets and environment variables

The operator separates file-like config from environment-driven config.

- `spec.env` adds explicit environment variables
- `spec.envFrom` imports environment values from `ConfigMap` or `Secret` sources
- `spec.secretRefs` mounts named secrets as files

This lets users keep credentials in Kubernetes `Secret` resources instead of embedding them into inline config blobs.

## Probes and health model

The operator uses exec probes rather than HTTP probes for Hermes itself.

That is because Hermes does not expose a native Kubernetes-style readiness endpoint. Instead, probe logic checks:
- the Hermes pid file
- the gateway state file
- optionally, whether a platform is actually connected

This keeps health checking close to Hermes' real runtime state instead of inventing a sidecar or synthetic HTTP server just for Kubernetes.

## Optional service exposure

Service creation is disabled by default.

That is intentional. Hermes gateway deployments are primarily egress-first, and many deployments do not need an inbound Kubernetes `Service` at all.
When users do need a service, they can opt in through `spec.service`.

## Optional network policy

The operator can create an egress-focused `NetworkPolicy` when `spec.networkPolicy.enabled` is true.
The default policy shape allows:
- DNS
- HTTP
- HTTPS
- SSH when the terminal backend is `ssh`

The policy is deliberately narrow and aligned with the MVP workload shape.

## Security defaults

The managed Hermes pod is hardened with restricted-style defaults:
- non-root execution
- explicit UID/GID
- `seccompProfile: RuntimeDefault`
- dropped Linux capabilities
- `allowPrivilegeEscalation: false`
- separate writable `/tmp`

The operator image itself also follows a locked-down container model.

## Install model

Operator installation is packaged as a Helm chart under `charts/chart/`.

The chart installs:
- the `HermesAgent` CRD
- the controller deployment
- controller RBAC
- the metrics service

Install-time values are intentionally minimal:
- operator image repository, tag, and pull policy
- controller resource requests and limits
- leader election toggle
- service account creation or reuse
- metrics enablement

The Hermes runtime image is not configured through the chart because it belongs to each `HermesAgent`, not to the operator installation.

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

### Egress-first assumptions

The operator assumes Hermes usually connects outward to external systems rather than serving cluster ingress traffic by default.
That keeps the default install smaller and easier to reason about.

## v1 non-goals

The following are explicitly out of scope for v1:
- multi-replica Hermes deployments
- autoscaling
- browser sidecars
- Docker-in-Docker terminal backends
- generic multi-tenant platform abstractions
- multiple CRDs for higher-level orchestration
- default ingress resources
- preserving compatibility with older deployment models

These are intentionally excluded so the first release stays focused on one clean path: install the operator, create one `HermesAgent`, and run Hermes reliably with persistent state.

## Relationship between operator and Hermes runtime

A common source of confusion is image ownership:

- this repository builds the **operator image**
- each `HermesAgent` references a separate **Hermes runtime image** in `spec.image`

That separation is deliberate. The operator manages Kubernetes resources; it does not embed the Hermes runtime into the controller process.

## Files to know

- `api/v1alpha1/hermesagent_types.go` — CRD schema
- `internal/controller/hermesagent_resources.go` — reconciled resource shapes
- `config/samples/hermes_v1alpha1_hermesagent.yaml` — sample custom resource
- `charts/chart/` — operator installation chart
- `README.md` — user-facing install and usage guide
