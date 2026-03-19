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
| `spec.fileMounts` | array | no | empty | Mount ConfigMaps or Secrets as files at explicit paths |
| `spec.imagePullSecrets` | array | no | empty | Image pull secrets for the Hermes workload pod |
| `spec.serviceAccountName` | string | no | empty | ServiceAccount for the Hermes workload pod |
| `spec.nodeSelector` | object | no | empty | Node selector for the Hermes workload pod |
| `spec.tolerations` | array | no | empty | Tolerations for the Hermes workload pod |
| `spec.affinity` | object | no | empty | Affinity and anti-affinity for the Hermes workload pod |
| `spec.topologySpreadConstraints` | array | no | empty | Topology spread rules for the Hermes workload pod |
| `spec.storage` | object | no | persistence enabled | Hermes state storage settings |
| `spec.terminal` | object | no | `backend: local` | Fallback terminal backend for operator wiring |
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
| `secretRef.name` | string | Name of an existing Secret in the same namespace |
| `secretRef.key` | string | Key inside that Secret |
| `secretRef.optional` | bool | Optional for Kubernetes reference semantics, but missing required runtime inputs still prevent healthy workload progress |

Admission validation rejects mixing `raw`, `configMapRef`, and `secretRef` on the same config source.

Referenced `ConfigMap` and `Secret` content is hashed into the pod template.
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

Use this for the simple legacy secret-bundle path.
For new work, prefer `spec.fileMounts` when you want an explicit mount path or a `ConfigMap` source.
The operator only mounts the files; it does not make Hermes interpret or load them.
Referenced secret content is hashed into the pod template.
Changes trigger a rollout.

### `spec.fileMounts`
Mount a whole `ConfigMap` or `Secret` as a read-only directory at an explicit path.
Each entry must set:
- `mountPath`
- exactly one of `configMapRef` or `secretRef`

```yaml
spec:
  fileMounts:
    - mountPath: /var/run/hermes/plugins
      configMapRef:
        name: hermes-plugins
    - mountPath: /var/run/hermes/ssh
      secretRef:
        name: hermes-ssh-auth
```

Rules:
- `mountPath` must be absolute
- mount paths must be unique within `spec.fileMounts`
- exactly one source is allowed per entry

Use this for plugin bundles, SSH material, prompt packs, certificates, or other runtime assets that a custom Hermes runtime image consumes as files.
The operator only delivers those files; the runtime image still defines whether any plugin, API, or integration behavior exists.
Referenced `ConfigMap` and `Secret` content is hashed into the pod template.
Changes trigger a rollout.

## Workload pod placement and registry auth

These fields apply to the managed Hermes pod, not to the operator deployment itself.
Use them when the runtime image lives in a private registry, the Hermes workload must run on specific nodes, or Hermes itself needs a dedicated Kubernetes identity.

### `spec.imagePullSecrets`

```yaml
spec:
  imagePullSecrets:
    - name: ghcr-pull-secret
```

Standard Kubernetes `imagePullSecrets` for the Hermes workload pod.
Each entry must set `name`.

### `spec.serviceAccountName`

```yaml
spec:
  serviceAccountName: hermes-runtime
```

Standard Kubernetes `serviceAccountName` for the Hermes workload pod.
Use this when Hermes itself needs Kubernetes API access with its own identity.
This does not change the operator controller's ServiceAccount or RBAC.

### `spec.nodeSelector`

```yaml
spec:
  nodeSelector:
    kubernetes.io/os: linux
```

Standard Kubernetes node selector for the Hermes workload pod.

### `spec.tolerations`

```yaml
spec:
  tolerations:
    - key: dedicated
      operator: Equal
      value: hermes
      effect: NoSchedule
```

Standard Kubernetes tolerations for the Hermes workload pod.

### `spec.affinity`

```yaml
spec:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: node-pool
                operator: In
                values:
                  - gpu
```

Standard Kubernetes affinity and anti-affinity for the Hermes workload pod.

### `spec.topologySpreadConstraints`

```yaml
spec:
  topologySpreadConstraints:
    - maxSkew: 1
      topologyKey: topology.kubernetes.io/zone
      whenUnsatisfiable: ScheduleAnyway
      labelSelector:
        matchLabels:
          app.kubernetes.io/name: hermes
```

Standard Kubernetes topology spread constraints for the Hermes workload pod.

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

`config.yaml` is the source of truth for the effective terminal backend whenever it declares `terminal.backend`.
The operator derives Kubernetes-side behavior such as generated SSH egress rules from the resolved config content for inline, `configMapRef`, and `secretRef` inputs.
`spec.terminal.backend` is only a fallback for cases where `config.yaml` does not declare a backend.

When `backend: ssh` is used:
- set `terminal.backend: ssh` in Hermes `config.yaml`
- provide the SSH env and mounted secret material your runtime image expects
- optionally keep `spec.terminal.backend: ssh` as an explicit fallback/default, although the resolved config still wins

When inline `spec.config.raw` is used, the webhook rejects explicit terminal backend mismatches.
When `config.yaml` comes from `configMapRef`, the controller derives the effective backend during reconcile from the referenced ConfigMap content.

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

This is the supported Kubernetes exposure path for HTTP-oriented deployment stories such as a custom API-serving Hermes runtime or a Hermes backend consumed by a separate Open WebUI deployment. The operator still manages only the Hermes pod; your runtime image must already listen on the chosen service port and implement the HTTP contract you expect. A stock Hermes image should not be assumed to expose that interface by default.

## Optional NetworkPolicy

```yaml
spec:
  networkPolicy:
    enabled: true
    additionalTCPPorts:
      - 8443
    additionalUDPPorts:
      - 3478
```

When enabled, the operator creates an egress-focused NetworkPolicy for the Hermes pod.
The default policy allows:
- DNS
- HTTP
- HTTPS
- SSH when `terminal.backend: ssh`

You can widen the generated policy with:
- `additionalTCPPorts` for extra outbound TCP ports
- `additionalUDPPorts` for extra outbound UDP ports

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `enabled` | bool | `false` | Whether the operator creates the NetworkPolicy |
| `additionalTCPPorts` | array | empty | Extra outbound TCP ports added to the generated policy |
| `additionalUDPPorts` | array | empty | Extra outbound UDP ports added to the generated policy |

The generated policy still allows egress by port only, to any destination. If you need destination-specific rules or a substantially different policy shape, disable `spec.networkPolicy.enabled` and manage your own NetworkPolicy instead.

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
- mixed `raw`, `configMapRef`, and `secretRef` sources on the same config field
- incomplete config references
- incomplete `env`, `envFrom`, `secretRefs`, `fileMounts`, or `imagePullSecrets` references
- invalid file mount source combinations or duplicate file mount paths
- invalid storage sizes
- invalid enabled Service ports
- invalid additional NetworkPolicy ports

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
