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
| `spec.image.repository` | string | yes | `ghcr.io/xmbshwll/hermes-agent-docker` | Hermes runtime image repository |
| `spec.image.tag` | string | no | `v2026.3.30` | Runtime image tag |
| `spec.image.pullPolicy` | string | no | `IfNotPresent` | Standard Kubernetes image pull policy |
| `spec.mode` | string | no | `gateway` | Only `gateway` is supported |
| `spec.config` | object | no | none | Source for `config.yaml` |
| `spec.gatewayConfig` | object | no | none | Source for `gateway.json` |
| `spec.env` | array | no | empty | Explicit environment variables |
| `spec.envFrom` | array | no | empty | Import env from `ConfigMap` or `Secret` |
| `spec.secretRefs` | array | no | empty | Mount named secrets under `/var/run/hermes/secrets/<name>` |
| `spec.fileMounts` | array | no | empty | Mount projected ConfigMaps or Secrets as files at explicit paths |
| `spec.imagePullSecrets` | array | no | empty | Image pull secrets for the Hermes workload pod |
| `spec.podLabels` | object | no | empty | Additional labels for the Hermes pod template |
| `spec.podAnnotations` | object | no | empty | Additional annotations for the Hermes pod template |
| `spec.serviceAccountName` | string | no | empty | ServiceAccount for the Hermes workload pod |
| `spec.automountServiceAccountToken` | bool | no | `false` | Controls automatic ServiceAccount token mounting for the Hermes workload pod |
| `spec.nodeSelector` | object | no | empty | Node selector for the Hermes workload pod |
| `spec.tolerations` | array | no | empty | Tolerations for the Hermes workload pod |
| `spec.affinity` | object | no | empty | Affinity and anti-affinity for the Hermes workload pod |
| `spec.topologySpreadConstraints` | array | no | empty | Topology spread rules for the Hermes workload pod |
| `spec.terminationGracePeriodSeconds` | int64 | no | Kubernetes default | Pod termination grace period for the Hermes workload pod |
| `spec.storage` | object | no | persistence enabled | Hermes state storage settings |
| `spec.terminal` | object | no | empty | Optional fallback terminal hint for operator wiring |
| `spec.resources` | object | no | empty | Standard Kubernetes resource requests and limits |
| `spec.probes` | object | no | profile-specific defaults | Startup, readiness, and liveness behavior |
| `spec.service` | object | no | disabled | Optional Service creation |
| `spec.networkPolicy` | object | no | disabled | Optional egress-focused NetworkPolicy |

## Image

```yaml
spec:
  image:
    repository: ghcr.io/xmbshwll/hermes-agent-docker
    tag: v2026.3.30
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
Mount a projected `ConfigMap` or `Secret` as a read-only directory at an explicit path.
Each entry must set:
- `mountPath`
- exactly one of `configMapRef` or `secretRef`

Optional projection controls:
- `items[]` to select specific keys and output paths
- `defaultMode` to set the default file mode for projected files
- `items[].mode` to override the mode for a specific file

```yaml
spec:
  fileMounts:
    - mountPath: /var/run/hermes/plugins
      configMapRef:
        name: hermes-plugins
      items:
        - key: plugin.py
          path: plugin.py
    - mountPath: /var/run/hermes/ssh
      secretRef:
        name: hermes-ssh-auth
      defaultMode: 0444
      items:
        - key: id_ed25519
          path: id_ed25519
          mode: 0600
        - key: known_hosts
          path: known_hosts
```

Rules:
- `mountPath` must be absolute
- mount paths must be unique within `spec.fileMounts`
- exactly one source is allowed per entry
- when `items` is omitted, all keys from the referenced object are projected
- `items[].key` and `items[].path` are required when `items` is used
- `items[].path` must be relative and must not contain `.` or `..` path segments
- item keys and item paths must be unique within a single file mount
- `defaultMode` and `items[].mode` must be between `0000` and `0777`

Use this for plugin bundles, SSH material, prompt packs, certificates, or other runtime assets that a custom Hermes runtime image consumes as files.
The operator only delivers those files; the runtime image still defines whether any plugin, API, or integration behavior exists.
Referenced `ConfigMap` and `Secret` content is hashed into the pod template.
When `items` is used, the rollout hash only tracks the selected keys instead of unrelated keys in the same object.
Changes trigger a rollout.

## Workload pod placement and registry auth

These fields apply to the managed Hermes pod, not to the operator deployment itself.
Use them when the runtime image lives in a private registry, the Hermes workload must run on specific nodes, Hermes itself needs a dedicated Kubernetes identity, or the pod must integrate with tools that depend on labels or annotations such as Prometheus, service meshes, or policy engines.

### `spec.imagePullSecrets`

```yaml
spec:
  imagePullSecrets:
    - name: ghcr-pull-secret
```

Standard Kubernetes `imagePullSecrets` for the Hermes workload pod.
Each entry must set `name`.

### `spec.podLabels`

```yaml
spec:
  podLabels:
    sidecar.istio.io/inject: "false"
```

Additional labels applied to the managed Hermes pod template.
The operator's own identity labels still win on conflicts so selectors and ownership wiring stay stable.
Use this for integrations such as service-mesh policy, cost allocation, or workload classification.

### `spec.podAnnotations`

```yaml
spec:
  podAnnotations:
    prometheus.io/scrape: "true"
```

Additional annotations applied to the managed Hermes pod template.
Use this for integrations such as Prometheus scraping hints, sidecar settings, or admission-controller metadata.

### `spec.serviceAccountName`

```yaml
spec:
  serviceAccountName: hermes-runtime
```

Standard Kubernetes `serviceAccountName` for the Hermes workload pod.
Use this when Hermes itself needs Kubernetes API access with its own identity.
This does not change the operator controller's ServiceAccount or RBAC.

### `spec.automountServiceAccountToken`

```yaml
spec:
  automountServiceAccountToken: false
```

Controls whether Kubernetes automatically mounts the ServiceAccount token into the managed Hermes pod.
The operator defaults this to `false` so Hermes does not receive cluster credentials unless you opt in deliberately.
If your Hermes runtime actually needs Kubernetes API access, set both `serviceAccountName` and `automountServiceAccountToken: true` explicitly.

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

### `spec.terminationGracePeriodSeconds`

```yaml
spec:
  terminationGracePeriodSeconds: 120
```

Standard Kubernetes pod termination grace period for the managed Hermes workload.
Use this when Hermes needs longer shutdown time to flush session state, emit final runtime files, or disconnect from external systems cleanly before Kubernetes sends `SIGKILL`.
When omitted, the operator leaves the field unset and Kubernetes applies its normal default.

### `spec.replicas`

```yaml
spec:
  replicas: 2
```

Controls how many Hermes pods the operator runs in the managed StatefulSet.
The default is `1`.
When you set `replicas` greater than `1`, you must also set `spec.storage.persistence.enabled: false`.
The operator does not manage shared Hermes state across replicas.
When replicas are greater than `1`, the operator also creates a `PodDisruptionBudget` with `maxUnavailable: 1`.

### `spec.updateStrategy`

```yaml
spec:
  updateStrategy:
    type: RollingUpdate
    rollingUpdate:
      partition: 1
```

Controls StatefulSet rollout behavior for Hermes pods.

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `type` | string | `RollingUpdate` | Supported values are `RollingUpdate` and `OnDelete` |
| `rollingUpdate.partition` | int32 | unset | Only valid when `type` is `RollingUpdate`; pods with ordinal lower than the partition wait for an explicit strategy change before updating |

Use `OnDelete` when you want manual, pod-by-pod restart control.
Use `RollingUpdate` with `partition` when you want staged updates across a multi-replica StatefulSet.

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
| `size` | string | `10Gi` | Must be a valid Kubernetes quantity greater than zero; storage request updates are reconciled onto the existing PVC |
| `accessModes` | array | `ReadWriteOnce` | Standard PVC access modes; changing this after the PVC exists requires claim recreation |
| `storageClassName` | string | unset | Optional storage class override; changing this after the PVC exists requires claim recreation |

## Terminal

```yaml
spec:
  terminal:
    backend: local
```

`config.yaml` is the source of truth for the effective terminal backend whenever it declares `terminal.backend`.
The operator derives Kubernetes-side behavior such as generated SSH egress rules from the resolved config content for inline, `configMapRef`, and `secretRef` inputs.
`spec.terminal.backend` is only an optional fallback hint for cases where `config.yaml` does not declare a backend.

The operator only has SSH-specific behavior today.
That means:
- when the effective backend is `ssh`, the generated NetworkPolicy adds SSH egress
- any other backend value is treated generically by the operator
- the operator does not try to model the full Hermes terminal backend surface in the CRD

When SSH behavior matters:
- prefer setting `terminal.backend: ssh` in Hermes `config.yaml`
- provide the SSH env and mounted secret material your runtime image expects
- optionally set `spec.terminal.backend: ssh` only as a fallback hint when the config does not declare a backend

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
    port: 80
    targetPort: 8080
```

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `enabled` | bool | `false` | Whether the operator creates a Service |
| `annotations` | object | empty | Additional annotations applied to the managed Service |
| `type` | string | `ClusterIP` | Standard Kubernetes Service type |
| `port` | int | `8080` | Published Service port; must be greater than zero when enabled |
| `targetPort` | int | same as `port` | Container port targeted by the Service |

The operator manages a Service with the same name as the `HermesAgent`.
If another same-name Service already exists and is not owned by the `HermesAgent`, reconciliation fails.

Use `service.annotations` for integrations such as Prometheus scraping metadata, cloud load-balancer controller hints, or cluster policy annotations.

This is the supported Kubernetes exposure path for HTTP-oriented deployment stories such as a custom API-serving Hermes runtime or a Hermes backend consumed by a separate Open WebUI deployment. The operator still manages only the Hermes pod; your runtime image must already listen on the chosen target port and implement the HTTP contract you expect. A stock Hermes image should not be assumed to expose that interface by default.

Use `targetPort` when you want the in-cluster Service port to differ from the port your runtime image actually listens on. When service exposure is enabled, the operator also declares the corresponding container port on the managed Hermes pod.

## Optional NetworkPolicy

```yaml
spec:
  networkPolicy:
    enabled: true
    destinations:
      - cidr: 203.0.113.0/24
        except:
          - 203.0.113.128/25
      - namespaceSelector:
          matchLabels:
            kubernetes.io/metadata.name: shared-services
        podSelector:
          matchLabels:
            app: egress-proxy
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

You can widen or restrict the generated policy with:
- `destinations` to restrict non-DNS egress to explicit CIDR blocks and/or selector-based peers
- `additionalTCPPorts` for extra outbound TCP ports
- `additionalUDPPorts` for extra outbound UDP ports

| Field | Type | Default | Notes |
| --- | --- | --- | --- |
| `enabled` | bool | `false` | Whether the operator creates the NetworkPolicy |
| `destinations` | array | empty | Optional destination allowlist applied to HTTP, HTTPS, SSH, and additional configured ports |
| `additionalTCPPorts` | array | empty | Extra outbound TCP ports added to the generated policy |
| `additionalUDPPorts` | array | empty | Extra outbound UDP ports added to the generated policy |

When `destinations` is omitted, the generated policy keeps the original port-only behavior for non-DNS egress.
When `destinations` is set, the operator still leaves DNS destination-agnostic so name resolution continues to work, but it restricts HTTP, HTTPS, SSH, and additional configured ports to the listed peers.

Each destination peer must set at least one of:
- `cidr`
- `namespaceSelector`
- `podSelector`

`except` is only valid together with `cidr`.
If you need a substantially different policy shape than this generated allowlist model, disable `spec.networkPolicy.enabled` and manage your own NetworkPolicy instead.

As with Service management, a same-name non-owned NetworkPolicy causes reconciliation to fail.

## Status

| Field | Type | Notes |
| --- | --- | --- |
| `status.phase` | string | Coarse-grained phase summary |
| `status.observedGeneration` | int64 | Last reconciled generation |
| `status.image` | string | Effective Hermes runtime image reference |
| `status.configHash` | string | Observed config revision hash for the current Hermes pod template |
| `status.readyReplicas` | int32 | Ready Hermes pod count |
| `status.persistenceBound` | bool | Whether the PVC is bound |
| `status.persistentVolumeClaimName` | string | Managed PVC name when persistence is enabled |
| `status.persistentVolumeClaimDriftedFields` | array | Immutable PVC fields that no longer match the requested spec |
| `status.persistentVolumeClaimRemediation` | string | Guided next step when immutable PVC drift blocks reconciliation |
| `status.serviceName` | string | Managed Service name when service exposure is enabled |
| `status.lastReconcileTime` | timestamp | Last status update time |
| `status.conditions` | array | Condition set for config, persistence, workload, and overall readiness |

### Main condition types
- `ConfigReady`
- `PersistenceReady`
- `WorkloadReady`
- `Ready`

Common workload-progress reasons include:
- `StatefulSetRolloutPending` when the StatefulSet controller has not yet observed the desired generation
- `StatefulSetWaitingForReadyReplicas` when the rollout generation is current but the pod is not yet ready
- `WaitingForPersistence` when workload progress is blocked on PVC creation or binding

When immutable PVC drift is detected, the operator also fills `status.persistentVolumeClaimDriftedFields` with the exact blocked fields and `status.persistentVolumeClaimRemediation` with the supported recovery path.

See `docs/troubleshooting.md` for common reasons and remediation steps.

## Admission behavior

The webhook currently rejects:
- mixed `raw`, `configMapRef`, and `secretRef` sources on the same config field
- incomplete config references
- incomplete `env`, `envFrom`, `secretRefs`, `fileMounts`, or `imagePullSecrets` references
- invalid file mount projection items such as duplicate keys, duplicate output paths, invalid relative paths, or invalid file modes
- invalid file mount source combinations or duplicate file mount paths
- invalid storage sizes
- invalid replica counts or multi-replica specs that also enable persistence
- invalid StatefulSet update strategy combinations
- invalid enabled Service ports
- invalid NetworkPolicy destination peers or additional ports

It also defaults:
- mode
- image tag and pull policy
- persistence settings
- replica count
- rollout strategy type
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
    repository: ghcr.io/xmbshwll/hermes-agent-docker
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
