# HTTP exposure for HermesAgent

This document defines the supported HTTP-serving path for the operator.

## Supported path

The supported HTTP exposure model is:

1. run a Hermes runtime image that still starts with `hermes gateway`
2. make that runtime image serve its HTTP interface inside the pod on a known container port
3. enable `spec.service.enabled`
4. set `spec.service.targetPort` to the runtime's listen port
5. optionally set `spec.service.port` to the in-cluster port you want clients to use

Example:

```yaml
spec:
  service:
    enabled: true
    type: ClusterIP
    port: 80
    targetPort: 8080
```

That is the supported Kubernetes exposure path for HTTP-serving Hermes runtimes.
When `spec.replicas` is greater than `1`, the Service load-balances across the ready Hermes pods.

## What the operator guarantees

When you use the supported HTTP path, the operator guarantees:
- the managed `Service` targets the Hermes pods
- the Service port and target port are reconciled from the CRD
- the managed pod declares the matching container port when service exposure is enabled
- the normal Hermes config, storage, probe, and rollout behavior still applies
- multi-replica HTTP workloads get an operator-managed `PodDisruptionBudget` with `maxUnavailable: 1`
- the HTTP path is covered by end-to-end test coverage in this repository

## What your runtime image must provide

The operator still does **not** create an HTTP server for you.
Your Hermes runtime image must:
- include `hermes` in `PATH`
- support `hermes gateway`
- keep the normal Hermes runtime contract used by the operator
- listen on the configured `spec.service.targetPort`
- serve the HTTP API contract your clients expect

A stock Hermes image should not be assumed to provide that HTTP interface by default.
If you need HTTP, choose or build a Hermes runtime image that already provides it.

## What is not in scope

The operator does **not** currently:
- generate ingress resources for Hermes workloads
- provision API gateways, proxies, or auth layers in front of Hermes
- deploy Open WebUI
- guarantee that arbitrary custom runtime images expose the same HTTP API shape

Ingress remains user-managed.
If you need ingress, point your own ingress controller or gateway at the operator-managed Service.

## Recommended usage model

### In-cluster consumers

For in-cluster clients, use the managed `ClusterIP` Service directly.
That is the simplest supported path.
For stateless HTTP-serving runtimes, you can also combine that with `spec.replicas > 1` and `spec.storage.persistence.enabled=false`.

### External or routed consumers

For external HTTP entry, keep the operator-managed Service as the backend and layer your own ingress or gateway on top.
The operator boundary stays clean:
- Hermes workload and Service: managed by the operator
- ingress, DNS, auth, TLS edge policy: managed by your platform

## Samples

The repository includes two HTTP-oriented samples:
- `config/samples/hermes_v1_hermesagent_api_server.yaml` — supported Service-based HTTP exposure for a custom Hermes runtime image
- `config/samples/hermes_v1_hermesagent_openwebui.yaml` — example-only Open WebUI backend shape built on the supported Service path

Use the API server sample when you want the supported operator-owned HTTP exposure model.
Use the Open WebUI sample only when you also manage the external Open WebUI side yourself.
