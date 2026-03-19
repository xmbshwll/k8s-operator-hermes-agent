# Troubleshooting HermesAgent

Start with the object itself:

```sh
kubectl describe hermesagent <name>
kubectl get hermesagent <name> -o yaml
kubectl get statefulset,pod,pvc,configmap,secret -l app.kubernetes.io/instance=<name>
```

`kubectl describe hermesagent <name>` now shows high-signal events from the operator.
Use those events together with the status conditions to narrow the issue quickly.

## Common conditions and fixes

| Condition reason | What it means | What to check next |
| --- | --- | --- |
| `InvalidConfig` | The spec is internally inconsistent or references were defined incorrectly | Check `spec.config`, `spec.gatewayConfig`, `spec.env`, `spec.envFrom`, `spec.secretRefs`, and `spec.fileMounts` for incomplete or conflicting fields |
| `ReferencedInputsReadFailed` | The controller could not read referenced ConfigMaps or Secrets because of an API or RBAC problem | Check operator logs and controller permissions |
| `MissingReferencedInput` event | A referenced ConfigMap or Secret, or one of its keys, does not exist yet | Verify the referenced objects exist in the same namespace and that the names/keys are correct |
| `ConfigMapReconcileFailed` | The operator could not create or update an inline generated ConfigMap | Check operator logs and look for API permission or conflict errors |
| `PersistentVolumeClaimMissing` | The Hermes PVC has not been created yet | Look for earlier reconcile failures or invalid storage configuration |
| `PersistentVolumeClaimPending` | The PVC exists but has not bound to storage yet | Check storage class, capacity, access modes, and cluster storage health |
| `PersistentVolumeClaimSpecDrift` | The requested PVC spec changed in a way Kubernetes cannot apply in place | Recreate the claim if you need new `accessModes` or `storageClassName`; size changes still reconcile normally |
| `PersistentVolumeClaimLost` | The PVC was lost after creation | Inspect the storage backend and recover or recreate the claim |
| `StatefulSetMissing` | The Hermes StatefulSet has not been created yet | Look for earlier reconcile failures on config, PVC, Service, or NetworkPolicy |
| `StatefulSetProgressing` | The StatefulSet rollout is still in progress | Inspect pod scheduling, image pulls, startup/readiness/liveness probes, and container logs |
| `StatefulSetReconcileFailed` | The operator could not create or update the StatefulSet | Check operator logs for API errors or immutable field conflicts |
| `ServiceReconcileFailed` | The optional Service could not be reconciled | Check for a same-name Service that is not owned by the HermesAgent |
| `NetworkPolicyReconcileFailed` | The optional NetworkPolicy could not be reconciled | Check for a same-name NetworkPolicy that is not owned by the HermesAgent |
| `Ready` | Persistence and workload are ready | No action needed |

## Common failure patterns

### Missing ConfigMap or Secret references

Symptoms:
- warning events with `MissingReferencedInput`
- the managed pod may fail to start because referenced files or env sources are unavailable
- `Ready=False` once the workload cannot progress

Check:

```sh
kubectl get configmap -n <namespace>
kubectl get secret -n <namespace>
```

Fix:
- create the missing object
- correct the referenced `name` or `key`
- re-apply the `HermesAgent`

### PVC stays pending

Symptoms:
- `PersistenceReady=False`
- event with `PersistentVolumeClaimPending`
- pod never becomes ready

Check:

```sh
kubectl describe pvc <name>-data -n <namespace>
kubectl get storageclass
```

Fix:
- choose an available storage class
- adjust requested size or access modes
- confirm the cluster has working dynamic provisioning

### Requested PVC settings do not match the existing claim

Symptoms:
- `PersistenceReady=False`
- event or condition reason `PersistentVolumeClaimSpecDrift`
- the existing Hermes pod may still be running, but the requested storage contract is not what Kubernetes applied

Check:

```sh
kubectl describe hermesagent <name> -n <namespace>
kubectl get pvc <name>-data -n <namespace> -o yaml
```

Fix:
- `spec.storage.persistence.size` can be increased in place when the storage class supports expansion
- changes to `spec.storage.persistence.accessModes` or `storageClassName` require recreating the claim
- back up any needed Hermes state, delete the workload/PVC deliberately, and then re-apply the `HermesAgent`

### Pods fail probes or never become ready

Symptoms:
- `WorkloadReady=False`
- `StatefulSetProgressing`
- pod restarts or rollout stalls

Check:

```sh
kubectl describe pod <name>-0 -n <namespace>
kubectl logs <name>-0 -n <namespace>
```

Focus on:
- startup probe failures
- readiness probe failures
- missing `gateway.pid` or `gateway_state.json`
- `gateway_state.json` never reaching `gateway_state: "running"`
- strict readiness waiting for any `platforms.*.state: "connected"`
- image pull errors

Fix:
- make sure the Hermes runtime image satisfies the runtime contract
- verify the container runs `hermes gateway`
- confirm the image has `bash` available for exec probes
- tune probe settings only if the runtime is healthy but startup is genuinely slower

### Runtime image does not match the operator contract

Symptoms:
- container starts but fails quickly
- probe failures despite valid Kubernetes resources
- logs show missing binaries or unsupported command lines

The runtime image must:
- include `hermes` in `PATH`
- support `hermes gateway`
- tolerate `HERMES_HOME=/data/hermes`
- write JSON metadata to `gateway.pid` with a numeric `pid` field
- write `gateway_state.json` with `gateway_state` and, when strict readiness is used, `platforms.*.state`
- run as non-root
- support `bash -ec` probe commands

### Same-name Service or NetworkPolicy already exists

Symptoms:
- `ServiceReconcileFailed` or `NetworkPolicyReconcileFailed`
- warning events mentioning the object is not owned by the `HermesAgent`

Check:

```sh
kubectl get service <name> -n <namespace> -o yaml
kubectl get networkpolicy <name> -n <namespace> -o yaml
```

Fix:
- delete or rename the conflicting object
- or disable the optional `spec.service.enabled` / `spec.networkPolicy.enabled`

### Generated NetworkPolicy is too narrow

Symptoms:
- Hermes starts but outbound calls to a non-default port fail
- the generated `NetworkPolicy` exists and egress works only for DNS, HTTP, HTTPS, and optional SSH

Check:

```sh
kubectl get networkpolicy <name> -n <namespace> -o yaml
```

Fix:
- add `spec.networkPolicy.additionalTCPPorts` or `spec.networkPolicy.additionalUDPPorts` for the missing ports
- or disable `spec.networkPolicy.enabled` and manage your own NetworkPolicy when you need destination-specific rules or a substantially different policy shape

## Useful follow-up commands

```sh
kubectl get hermesagent <name> -n <namespace> -o jsonpath='{.status.conditions}'
kubectl rollout status statefulset/<name> -n <namespace>
kubectl logs deployment/k8s-operator-hermes-agent-controller-manager -n k8s-operator-hermes-agent-system -c manager
```
