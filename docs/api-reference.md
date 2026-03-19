# HermesAgent API reference

This document is the user-facing reference for the `HermesAgent` custom resource.
It complements the schema markers in `api/v1alpha1/hermesagent_types.go` with practical behavior notes.

## Resource

```yaml
apiVersion: hermes.nous.ai/v1alpha1
kind: HermesAgent
```

Scope: namespaced

## Top-level spec

| Field | Type | Required | Default | Notes |
| --- | --- | --- | --- | --- |
| `spec.image.repository` | string | yes | none | Hermes runtime image repository |
| `spec.image.tag` | string | no | `gateway-core` | Runtime image tag |
| `spec.image.pullPolicy` | string | no | `IfNotPresent` | Standard Kubernetes image pull policy |
| `spec.mode` | string | no | `gateway` | Only `gateway` is supported |
| `spec.config` | object | no | none | Source for `config.yaml` |
| `spec.gatewayConfig` | object | no | none | Source for `gateway.json` |
| `spec.env` | array | no | empty | Explicit environment variables |
| `spec.envFrom` | array | no | empty | Import env from `ConfigMap` or `Secret` |
| `spec.secretRefs` | array | no | empty | Mount named secrets under `/var/run/hermes/secrets/<name>` |
| `spec.storage` | object | no | persistence enabled | Hermes state storage settings |
| `spec.terminal` | object | no | `backend: local` | Terminal backend selection |
| `spec.resources` | object | no | empty | Standard Kubernetes resource requests and limits |
| `spec.probes` | object | no | profile-specific defaults | Startup, readiness, and liveness behavior |
| `spec.service` | object | no | disabled | Optional Service creation |
| `spec.networkPolicy` | object | no | disabled | Optional egress-focused NetworkPolicy |

## Image

```yaml
spec:
  image:
    repository: ghcr.io/example/hermes-agent
    tag: gateway-core
    pullPolicy: IfNotPresent
```

The runtime image must satisfy the contract documented in `README.md` and `docs/architecture.md`:
- contain `hermes` in `PATH`
- support `hermes gateway`
- tolerate `HERMES_HOME=/data/hermes`
- run as non-root
- support `bash -ec` probe commands

## Config files

### `spec.config`
Maps to `/data/hermes/config.yaml`

### `spec.gatewayConfig`
Maps to `/data/hermes/gateway.json`

Each file source supports exactly one of:

| Field | Type | Notes |
| --- | --- | --- |
| `raw` | string | Inline file content; the operator generates a ConfigMap |
| `configMapRef.name` | string | Name of an existing ConfigMap in the same namespace |
| `configMapRef.key` | string | Key inside that ConfigMap |
| `configMapRef.optional` | bool | Optional for Kubernetes reference semantics, but missing required runtime inputs still prevent healthy workload progress |

Admission validation rejects setting both `raw` and `configMapRef` on the same config source.

Referenced `ConfigMap` content is hashed into the pod template.
Updating the referenced object triggers a reconcile and rollout.

## Environment and secrets

### `spec.env`
Standard Kubernetes `EnvVar` entries.
`valueFrom.configMapKeyRef` and `valueFrom.secretKeyRef` are supported and hashed into the pod template.
Changes to those referenced objects trigger a rollout.

### `spec.envFrom`
Standard Kubernetes `EnvFromSource` entries.
Each entry must use exactly one of:
- `configMapRef`
- `secretRef`

The referenced object content is hashed into the pod template.
Changes trigger a rollout.

### `spec.secretRefs`
List of named `Secret` objects mounted as read-only directories:

```text
/var/run/hermes/secrets/<secret-name>
```

Use this for file bundles the runtime image consumes directly, such as SSH auth material or Hermes plugin bundles.
Referenced secret content is hashed into the pod template.
Changes trigger a rollout.

## Storage

```yaml
spec:
  storage:
    persistence:
      enabled: true
      size: 10Gi
      accessModes:
        - ReadWriteOnce
      storageClassName: fast-ssd
```

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `enabled` | bool | `true` | When false, Hermes uses `emptyDir` |
| `size` | string | `10Gi` | Must be a valid Kubernetes quantity greater than zero |
| `accessModes` | array | `ReadWriteOnce` | Standard PVC access modes |
| `storageClassName` | string | unset | Optional storage class override |

## Terminal

```yaml
spec:
  terminal:
    backend: local
```

Supported values:
- `local`
- `ssh`

`spec.terminal.backend` is operator-side wiring, not a second runtime entrypoint. The operator uses it for Kubernetes behavior such as generated egress rules.
Your Hermes runtime image still reads its terminal backend from `config.yaml`, so keep `spec.terminal.backend` and `spec.config.raw.terminal.backend` aligned.

When `backend: ssh` is used:
- set `spec.terminal.backend: ssh`
- set `terminal.backend: ssh` in Hermes `config.yaml`
- provide the SSH env and mounted secret material your runtime image expects

When inline `spec.config.raw` is used, the webhook rejects explicit terminal backend mismatches.
When `config.yaml` comes from `configMapRef`, the operator cannot inspect it at admission time, so you must keep the referenced config aligned yourself.

## Probes

```yaml
spec:
  probes:
    startup:
      enabled: true
      initialDelaySeconds: 0
      periodSeconds: 10
      timeoutSeconds: 5
      failureThreshold: 18
    readiness:
      enabled: true
      initialDelaySeconds: 5
      periodSeconds: 10
      timeoutSeconds: 5
      failureThreshold: 3
    liveness:
      enabled: true
      initialDelaySeconds: 15
      periodSeconds: 10
      timeoutSeconds: 5
      failureThreshold: 3
    requireConnectedPlatform: false
```

Defaults are admission-webhook driven and intentionally differ per probe type.
Do not assume the same defaults for startup, readiness, and liveness.

`requireConnectedPlatform: true` makes readiness stricter by requiring `gateway_state: "running"` and at least one `platforms.*.state: "connected"` entry in `gateway_state.json`.

## Optional Service

```yaml
spec:
  service:
    enabled: true
    type: ClusterIP
    port: 8080
```

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `enabled` | bool | `false` | Whether the operator creates a Service |
| `type` | string | `ClusterIP` | Standard Kubernetes Service type |
| `port` | int | `8080` | Must be greater than zero when enabled |

The operator manages a Service with the same name as the `HermesAgent`.
If another same-name Service already exists and is not owned by the `HermesAgent`, reconciliation fails.

This is the supported exposure path for HTTP-oriented deployment stories such as an API-serving Hermes runtime or a Hermes backend consumed by a separate Open WebUI deployment. The operator still manages only the Hermes pod; your runtime image must already listen on the chosen service port.

## Optional NetworkPolicy

```yaml
spec:
  networkPolicy:
    enabled: true
```

When enabled, the operator creates an egress-focused NetworkPolicy for the Hermes pod.
The default policy allows:
- DNS
- HTTP
- HTTPS
- SSH when `terminal.backend: ssh`

As with Service management, a same-name non-owned NetworkPolicy causes reconciliation to fail.

## Status

| Field | Type | Notes |
| --- | --- | --- |
| `status.phase` | string | Coarse-grained phase summary |
| `status.observedGeneration` | int64 | Last reconciled generation |
| `status.readyReplicas` | int32 | Ready Hermes pod count |
| `status.persistenceBound` | bool | Whether the PVC is bound |
| `status.lastReconcileTime` | timestamp | Last status update time |
| `status.conditions` | array | Condition set for config, persistence, workload, and overall readiness |

### Main condition types
- `ConfigReady`
- `PersistenceReady`
- `WorkloadReady`
- `Ready`

See `docs/troubleshooting.md` for common reasons and remediation steps.

## Admission behavior

The webhook currently rejects:
- mixed `raw` and `configMapRef` on the same config field
- incomplete config references
- incomplete `env`, `envFrom`, or `secretRefs` references
- invalid storage sizes
- invalid enabled Service ports

It also defaults:
- mode
- image tag and pull policy
- persistence settings
- service settings
- network policy enablement
- probe profiles

## Example

```yaml
apiVersion: hermes.nous.ai/v1alpha1
kind: HermesAgent
metadata:
  name: hermesagent-sample
spec:
  image:
    repository: ghcr.io/example/hermes-agent
  config:
    raw: |
      model: anthropic/claude-opus-4.1
      terminal:
        backend: local
  gatewayConfig:
    raw: |
      {
        "platforms": {}
      }
```

For complete sample manifests, see `config/samples/README.md`.
