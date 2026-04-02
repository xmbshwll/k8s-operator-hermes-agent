# Local Kind demo guide: plain kubectl path and plain Helm path

This guide is intentionally narrow.
It focuses on two local workflows only:

1. install and test the operator with plain `kubectl` / local manifests
2. install and test the operator with plain Helm

It does **not** cover any extra UI tooling.
The goal is just to prove that:
- the operator installs cleanly on Kind
- a `HermesAgent` becomes functional
- you can inspect everything directly with `kubectl`

This guide reflects the current project behavior:
- cert-manager should be installed first because the default install path enables admission webhooks
- local chart installs support an optional controller-manager `PodDisruptionBudget`, but it only renders for HA installs with `replicaCount > 1`
- `HermesAgent` now supports richer `fileMounts` projections with selected keys and file modes
- the general samples now use the published runtime image `ghcr.io/xmbshwll/hermes-agent-docker:v2026.3.30`
- new manifests use `apiVersion: hermes.nous.ai/v1`; deprecated `v1alpha1` remains served only for upgrade compatibility
- HTTP UI and API-demo flows are out of scope here

---

## 1. Prerequisites

You need these locally:
- Docker
- `kubectl`
- `kind`
- Helm

Quick check:

```sh
docker version
kubectl version --client
kind version
helm version
```

---

## 2. Create a local cluster

```sh
kind create cluster --name hermes-local
kubectl config use-context kind-hermes-local
kubectl cluster-info --context kind-hermes-local
```

---

## 3. Install cert-manager

The operator install paths in this repo assume webhook TLS is available, so install cert-manager first.

```sh
helm repo add jetstack https://charts.jetstack.io
helm repo update
helm upgrade --install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --set crds.enabled=true \
  --wait \
  --timeout 5m
```

Check it:

```sh
kubectl get pods -n cert-manager
```

---

## 4. Build and load the local operator image

Build the operator image from this repo:

```sh
make docker-build IMG=k8s-operator-hermes-agent:local
```

Load it into Kind:

```sh
kind load docker-image k8s-operator-hermes-agent:local --name hermes-local
```

---

## 5. Runtime image used for testing

For the functional `HermesAgent` checks in this guide, use the published runtime image:

```text
ghcr.io/xmbshwll/hermes-agent-docker:v2026.3.30
```

That image is already used by the general samples such as:
- `config/samples/hermes_v1_hermesagent.yaml`
- `config/samples/hermes_v1_hermesagent_secret_config.yaml`
- `config/samples/hermes_v1_hermesagent_ssh.yaml`
- `config/samples/hermes_v1_hermesagent_plugins.yaml`
- `config/samples/hermes_v1_hermesagent_telegram.yaml`

This guide stays on those non-HTTP functionality paths.

Create a namespace for the demo objects:

```sh
kubectl create namespace hermes-demo
```

---

## 6. Path A: install and test with plain kubectl

This path uses the repo’s local kustomize/manifests flow.

### 6.1 Install the operator

```sh
make deploy IMG=k8s-operator-hermes-agent:local
```

Verify:

```sh
kubectl get pods -n k8s-operator-hermes-agent-system
kubectl get crd hermesagents.hermes.nous.ai
```

### 6.2 Apply the minimal HermesAgent sample

```sh
kubectl apply -n hermes-demo -f config/samples/hermes_v1_hermesagent.yaml
```

Wait for it:

```sh
kubectl wait -n hermes-demo \
  --for=jsonpath='{.status.phase}'=Ready \
  hermesagent/hermesagent-sample \
  --timeout=10m

kubectl rollout status -n hermes-demo \
  statefulset/hermesagent-sample \
  --timeout=10m
```

### 6.3 Verify functionality with kubectl

```sh
kubectl get hermesagent -n hermes-demo
kubectl describe hermesagent hermesagent-sample -n hermes-demo
kubectl get statefulset,pod,pvc,configmap,secret -n hermes-demo
kubectl logs statefulset/hermesagent-sample -n hermes-demo
```

Things to confirm:
- the `HermesAgent` reaches `Ready`
- the managed `StatefulSet` exists and rolls out successfully
- a PVC is created and bound
- generated or referenced config files are mounted as expected
- operator events are clean and do not show `InvalidConfig` or `MissingReferencedInput`

### 6.4 Optional: test newer runtime-alignment samples

#### Secret-backed config sample

```sh
kubectl apply -n hermes-demo -f config/samples/hermes_v1_hermesagent_secret_config.yaml
kubectl describe hermesagent hermesagent-secret-config -n hermes-demo
```

This validates:
- `config.yaml` from `secretRef`
- `gateway.json` from `secretRef`
- deterministic rollout behavior for referenced Secret changes

#### SSH sample

Edit the placeholder secret values first, then:

```sh
kubectl apply -n hermes-demo -f config/samples/hermes_v1_hermesagent_ssh.yaml
kubectl describe hermesagent hermesagent-ssh -n hermes-demo
kubectl get networkpolicy -n hermes-demo
```

This validates:
- `terminal.backend: ssh` in resolved config
- fallback `spec.terminal.backend` semantics
- `fileMounts.items[]` projection
- SSH-safe file mode overrides
- generated SSH egress in the operator-managed `NetworkPolicy`

#### Plugin file-delivery sample

```sh
kubectl apply -n hermes-demo -f config/samples/hermes_v1_hermesagent_plugins.yaml
kubectl describe hermesagent hermesagent-plugins -n hermes-demo
```

This validates:
- explicit `fileMounts` key selection
- projected file delivery without widening the CRD into generic pod-volume plumbing

### 6.5 Clean up the kubectl-installed operator path

Delete sample workloads first:

```sh
kubectl delete namespace hermes-demo
```

Then remove the operator:

```sh
make undeploy
```

If you also want to remove the CRD entirely:

```sh
make uninstall
```

---

## 7. Path B: install and test with Helm

This path uses the local chart under `charts/chart/`.

### 7.1 Install the operator with Helm

```sh
helm upgrade --install k8s-operator-hermes-agent ./charts/chart \
  --namespace k8s-operator-hermes-agent-system \
  --create-namespace \
  --set image.repository=k8s-operator-hermes-agent \
  --set image.tag=local \
  --wait \
  --timeout 5m
```

Verify:

```sh
kubectl get pods -n k8s-operator-hermes-agent-system
kubectl get crd hermesagents.hermes.nous.ai
```

### 7.2 Apply the same minimal HermesAgent sample

```sh
kubectl create namespace hermes-demo
kubectl apply -n hermes-demo -f config/samples/hermes_v1_hermesagent.yaml
```

Wait for it:

```sh
kubectl wait -n hermes-demo \
  --for=jsonpath='{.status.phase}'=Ready \
  hermesagent/hermesagent-sample \
  --timeout=10m

kubectl rollout status -n hermes-demo \
  statefulset/hermesagent-sample \
  --timeout=10m
```

### 7.3 Verify functionality with kubectl

```sh
kubectl get hermesagent -n hermes-demo
kubectl describe hermesagent hermesagent-sample -n hermes-demo
kubectl get statefulset,pod,pvc,configmap,secret -n hermes-demo
kubectl logs statefulset/hermesagent-sample -n hermes-demo
```

This should give you the same functional result as the kubectl install path.
The difference is only how the operator itself was installed.

### 7.4 Optional: test Helm-specific chart behavior

#### CRD-first local upgrade flow

If you changed the API or CRD locally, use the same upgrade flow documented for releases:

```sh
make build-crd-bundle
kubectl apply -f dist/hermesagents.hermes.nous.ai-crd.yaml
helm upgrade k8s-operator-hermes-agent ./charts/chart \
  --namespace k8s-operator-hermes-agent-system \
  --wait \
  --timeout 5m
```

#### Optional HA / PodDisruptionBudget path

The chart now supports an optional controller-manager PDB, but only for HA installs.
It renders only when `replicaCount > 1`.

Example:

```sh
helm upgrade --install k8s-operator-hermes-agent ./charts/chart \
  --namespace k8s-operator-hermes-agent-system \
  --create-namespace \
  --set image.repository=k8s-operator-hermes-agent \
  --set image.tag=local \
  --set replicaCount=2 \
  --set podDisruptionBudget.enabled=true \
  --wait \
  --timeout 5m
```

Check it:

```sh
kubectl get pdb -n k8s-operator-hermes-agent-system
kubectl describe pdb k8s-operator-hermes-agent-controller-manager-pdb -n k8s-operator-hermes-agent-system
```

### 7.5 Clean up the Helm-installed operator path

Delete sample workloads first:

```sh
kubectl delete namespace hermes-demo
```

Then uninstall the chart:

```sh
helm uninstall k8s-operator-hermes-agent -n k8s-operator-hermes-agent-system
```

Helm uninstall keeps the CRD by default.
If you also want to remove the API entirely after deleting all `HermesAgent` resources:

```sh
make uninstall
```

---

## 8. Useful kubectl checks for both paths

Operator health:

```sh
kubectl get all -n k8s-operator-hermes-agent-system
kubectl logs deployment/k8s-operator-hermes-agent-controller-manager \
  -n k8s-operator-hermes-agent-system \
  -c manager
```

HermesAgent health:

```sh
kubectl get hermesagent -A
kubectl describe hermesagent <name> -n hermes-demo
kubectl describe pod <pod-name> -n hermes-demo
kubectl logs <pod-name> -n hermes-demo
```

Related resources:

```sh
kubectl get statefulset,pod,pvc,configmap,secret,networkpolicy -n hermes-demo
```

---

## 9. Troubleshooting shortcuts

### The HermesAgent never becomes ready

```sh
kubectl describe hermesagent <name> -n hermes-demo
kubectl describe pod <pod-name> -n hermes-demo
kubectl logs <pod-name> -n hermes-demo
```

Look for:
- probe failures
- PVC binding problems
- image pull failures
- missing referenced ConfigMaps or Secrets

### The operator rejects the resource on create

Check the admission error and then review:
- `spec.config`
- `spec.gatewayConfig`
- `spec.env`
- `spec.envFrom`
- `spec.secretRefs`
- `spec.fileMounts`

Common current validation rules:
- config sources must not mix `raw`, `configMapRef`, and `secretRef`
- file mounts must use exactly one source and an absolute `mountPath`
- projected file items must use unique keys and unique relative output paths
- projected file modes must be valid

### A referenced file or secret change does not seem to take effect

The controller now hashes referenced inputs into the pod template.
Check:

```sh
kubectl describe hermesagent <name> -n hermes-demo
kubectl get statefulset <name> -n hermes-demo -o yaml | rg config-hash
```

If you are using `fileMounts.items[]`, make sure the keys you changed are actually selected by the mount.
Unselected keys do not participate in the rollout hash.

### Helm upgrade after API changes behaves strangely

Use the CRD-first flow:

```sh
make build-crd-bundle
kubectl apply -f dist/hermesagents.hermes.nous.ai-crd.yaml
helm upgrade k8s-operator-hermes-agent ./charts/chart \
  --namespace k8s-operator-hermes-agent-system
```

---

## 10. Full cleanup

```sh
kubectl delete namespace hermes-demo --ignore-not-found=true
helm uninstall cert-manager -n cert-manager || true
kubectl delete namespace cert-manager k8s-operator-hermes-agent-system --ignore-not-found=true
kind delete cluster --name hermes-local
```

---

## Minimal checklist

If you want the shortest possible proof that things work:

1. create the Kind cluster
2. install cert-manager
3. build and load the operator image
4. choose one install path:
   - `make deploy IMG=k8s-operator-hermes-agent:local`
   - or `helm upgrade --install ... ./charts/chart ...`
5. create `hermes-demo`
6. apply `config/samples/hermes_v1_hermesagent.yaml`
7. wait for `hermesagent/hermesagent-sample` to become ready
8. inspect it with `kubectl describe hermesagent hermesagent-sample -n hermes-demo`
9. inspect logs and generated resources with `kubectl`
