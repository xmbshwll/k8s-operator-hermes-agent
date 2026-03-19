# Local demo guide: Kind + Argo CD + Hermes operator + first chat

This is the fastest clean local path to:
- spin up a disposable Kubernetes cluster
- get a useful web UI with Argo CD
- install this operator from your local checkout
- validate a Hermes runtime inside the cluster
- optionally talk to it through Open WebUI

This guide reflects the current operator behavior:
- the operator chart enables admission webhooks by default, so cert-manager should be installed first
- the local chart is the recommended install path while developing
- the operator now supports richer `fileMounts` projections for selected keys and file modes
- the general samples now use the published Hermes runtime image from `ghcr.io/xmbshwll/hermes-agent-docker:latest`
- the API server and Open WebUI paths still require a **custom** Hermes runtime image that actually serves the HTTP API you want on port `8080`

If your goal is only to validate the operator and a basic Hermes runtime, you can use the published GHCR image directly.
If your goal is the full Open WebUI first-chat flow, you still need a custom HTTP-capable Hermes runtime image.

---

## 1. Prerequisites

You need these on your machine:
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

## 2. Create a local Kubernetes cluster

```sh
kind create cluster --name hermes-local
kubectl cluster-info --context kind-hermes-local
```

Use that context for the rest of the guide:

```sh
kubectl config use-context kind-hermes-local
```

---

## 3. Install Argo CD for a nice UI

Argo CD is optional, but it gives you a clean dashboard for what is running in the cluster.

```sh
kubectl create namespace argocd
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
kubectl wait -n argocd --for=condition=Available deployment/argocd-server --timeout=5m
```

Open the UI in another terminal:

```sh
kubectl -n argocd port-forward svc/argocd-server 8081:443
```

Then open:
- https://localhost:8081

Get the admin password:

```sh
kubectl -n argocd get secret argocd-initial-admin-secret \
  -o jsonpath='{.data.password}' | base64 --decode && echo
```

Login with:
- username: `admin`
- password: output from the command above

---

## 4. Install cert-manager

The operator chart enables admission webhooks by default, so install cert-manager first.

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

## 5. Build and load the operator image

Build the operator image from this repo:

```sh
make docker-build IMG=k8s-operator-hermes-agent:local
```

Load it into Kind:

```sh
kind load docker-image k8s-operator-hermes-agent:local --name hermes-local
```

---

## 6. Install the operator from your local chart

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

You should see the controller-manager pod running.

### Local upgrade note

If you change the `HermesAgent` API or CRD schema while iterating locally, follow the same CRD-first flow the repo now documents for releases:

```sh
make build-crd-bundle
kubectl apply -f dist/hermesagents.hermes.nous.ai-crd.yaml
helm upgrade k8s-operator-hermes-agent ./charts/chart \
  --namespace k8s-operator-hermes-agent-system \
  --wait \
  --timeout 5m
```

---

## 7. Choose your Hermes runtime path

There are now two practical local-demo paths.

### Path A: use the published runtime image

For minimal operator validation, secret-backed config, Telegram, SSH, or plugin file-delivery demos, you can use:

```text
ghcr.io/xmbshwll/hermes-agent-docker:latest
```

That image is a concrete starting point for the non-HTTP samples in `config/samples/`.
It should not be treated as proof that an API-server or Open WebUI-compatible HTTP surface exists.

### Path B: use a custom HTTP-capable runtime image

For the Open WebUI path in this guide, you still need a custom Hermes runtime image that:
- contains `hermes` in `PATH`
- supports `hermes gateway`
- tolerates `HERMES_HOME=/data/hermes`
- supports `bash -ec` for probes
- serves the HTTP API you want on port `8080`

Example:

```sh
docker build -t hermes-agent-http:local /path/to/your/hermes-runtime
kind load docker-image hermes-agent-http:local --name hermes-local
```

---

## 8. Create a demo namespace

```sh
kubectl create namespace hermes-demo
```

---

## 9. Quick operator/runtime validation with the published image

If you want the quickest validation that the operator and runtime wiring work together, start with the minimal sample.
It now points at the published GHCR image already.

```sh
kubectl apply -n hermes-demo -f config/samples/hermes_v1alpha1_hermesagent.yaml
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

Useful checks:

```sh
kubectl get hermesagent -n hermes-demo
kubectl get pods,pvc -n hermes-demo
kubectl describe hermesagent hermesagent-sample -n hermes-demo
kubectl logs statefulset/hermesagent-sample -n hermes-demo
```

### Optional: validate newer file-delivery flows

The operator now supports richer file projections with selected keys and per-file modes.
You can exercise that locally with the updated samples.

#### SSH sample

```sh
kubectl apply -n hermes-demo -f config/samples/hermes_v1alpha1_hermesagent_ssh.yaml
```

What this demonstrates:
- `terminal.backend: ssh` in Hermes config
- fallback `spec.terminal.backend: ssh` only as an operator hint
- `fileMounts` projecting only `id_ed25519` and `known_hosts`
- SSH-safe file mode override for the private key
- generated SSH egress in the operator-managed `NetworkPolicy`

#### Plugin sample

```sh
kubectl apply -n hermes-demo -f config/samples/hermes_v1alpha1_hermesagent_plugins.yaml
```

What this demonstrates:
- `fileMounts` with explicit `items`
- selective projection of plugin files instead of mounting every key
- rollout hashing tied to the selected keys only

If you use either sample, edit the placeholder secrets first.
See `config/samples/README.md` for the expected inputs.

---

## 10. Deploy a Hermes runtime for Open WebUI

For the first-chat path, use a **custom HTTP-capable** Hermes runtime image.
The built-in API-server and Open WebUI samples intentionally still assume a custom image.

Apply a local-demo `HermesAgent`.
Replace `YOUR_OPENROUTER_KEY` with a real key before running it.
If you use another provider, adjust the env secret and `model:` accordingly.

```sh
cat <<'EOF' | kubectl apply -n hermes-demo -f -
apiVersion: v1
kind: Secret
metadata:
  name: hermesagent-openwebui-env
stringData:
  OPENROUTER_API_KEY: YOUR_OPENROUTER_KEY
type: Opaque
---
apiVersion: hermes.nous.ai/v1alpha1
kind: HermesAgent
metadata:
  name: hermesagent-openwebui
spec:
  image:
    repository: hermes-agent-http
    tag: local
    pullPolicy: IfNotPresent
  mode: gateway
  config:
    raw: |
      model: openrouter/anthropic/claude-opus-4.1
      terminal:
        backend: local
  gatewayConfig:
    raw: |
      {
        "platforms": {}
      }
  envFrom:
    - secretRef:
        name: hermesagent-openwebui-env
  storage:
    persistence:
      enabled: true
      size: 10Gi
      accessModes:
        - ReadWriteOnce
  terminal:
    backend: local
  resources:
    requests:
      cpu: 500m
      memory: 1Gi
    limits:
      cpu: "2"
      memory: 4Gi
  probes:
    startup:
      enabled: true
      periodSeconds: 10
    readiness:
      enabled: true
      periodSeconds: 10
    liveness:
      enabled: true
      periodSeconds: 10
  service:
    enabled: true
    type: ClusterIP
    port: 8080
  networkPolicy:
    enabled: false
EOF
```

Wait for it:

```sh
kubectl wait -n hermes-demo \
  --for=jsonpath='{.status.phase}'=Ready \
  hermesagent/hermesagent-openwebui \
  --timeout=10m

kubectl rollout status -n hermes-demo \
  statefulset/hermesagent-openwebui \
  --timeout=10m
```

Useful checks:

```sh
kubectl get hermesagent -n hermes-demo
kubectl get pods,svc,pvc -n hermes-demo
kubectl describe hermesagent hermesagent-openwebui -n hermes-demo
kubectl logs statefulset/hermesagent-openwebui -n hermes-demo
```

---

## 11. Quick direct API smoke test

Before adding a chat UI, confirm the Hermes service is reachable.

```sh
kubectl -n hermes-demo port-forward svc/hermesagent-openwebui 8080:8080
```

In another terminal, try:

```sh
curl http://localhost:8080/
```

If your Hermes runtime exposes an OpenAI-compatible API, also try its model or health endpoint, for example:

```sh
curl http://localhost:8080/v1/models
```

If this does not work, fix the Hermes runtime image or service configuration before adding Open WebUI.

---

## 12. Install Open WebUI for the first chat

This example keeps Open WebUI simple and in-cluster.
It points Open WebUI at the Hermes service DNS name.

```sh
kubectl create namespace openwebui

cat <<'EOF' | kubectl apply -n openwebui -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: open-webui
spec:
  replicas: 1
  selector:
    matchLabels:
      app: open-webui
  template:
    metadata:
      labels:
        app: open-webui
    spec:
      containers:
        - name: open-webui
          image: ghcr.io/open-webui/open-webui:main
          ports:
            - containerPort: 8080
          env:
            - name: OPENAI_API_BASE_URL
              value: http://hermesagent-openwebui.hermes-demo.svc.cluster.local:8080/v1
            - name: OPENAI_API_KEY
              value: local-demo
---
apiVersion: v1
kind: Service
metadata:
  name: open-webui
spec:
  selector:
    app: open-webui
  ports:
    - port: 8080
      targetPort: 8080
EOF
```

Wait for it:

```sh
kubectl wait -n openwebui --for=condition=Available deployment/open-webui --timeout=10m
```

Port-forward it:

```sh
kubectl -n openwebui port-forward svc/open-webui 3000:8080
```

Open:
- http://localhost:3000

Then:
1. finish the initial Open WebUI setup
2. pick the Hermes-backed model if Open WebUI lists it
3. send a first message

If your Hermes API expects a real API key on incoming requests, replace `OPENAI_API_KEY=local-demo` with the value your runtime expects.

---

## 13. Use Argo CD as the cluster dashboard

Argo CD will not magically manage your unpublished local checkout, but it is still useful as a visual dashboard while you are testing.

Good things to watch in Argo CD or with `kubectl`:
- `k8s-operator-hermes-agent-system` namespace
- `hermes-demo` namespace
- the `HermesAgent` custom resource
- the generated `StatefulSet`, `PVC`, `Service`, `NetworkPolicy`, and inline-generated `ConfigMap`

For raw Kubernetes views, these are still the fastest commands:

```sh
kubectl get all -n k8s-operator-hermes-agent-system
kubectl get all -n hermes-demo
kubectl get hermesagent -A
kubectl describe hermesagent hermesagent-openwebui -n hermes-demo
```

---

## 14. Troubleshooting shortcuts

Hermes not getting ready:

```sh
kubectl describe hermesagent hermesagent-openwebui -n hermes-demo
kubectl describe pod hermesagent-openwebui-0 -n hermes-demo
kubectl logs hermesagent-openwebui-0 -n hermes-demo
```

Operator issues:

```sh
kubectl logs deployment/k8s-operator-hermes-agent-controller-manager \
  -n k8s-operator-hermes-agent-system \
  -c manager
```

Open WebUI cannot talk to Hermes:
- verify `svc/hermesagent-openwebui` exists in `hermes-demo`
- verify Hermes really serves the API Open WebUI expects on `:8080`
- re-run the direct `curl http://localhost:8080/v1/models` smoke test through port-forward
- check the Open WebUI pod logs:

```sh
kubectl logs deployment/open-webui -n openwebui
```

Webhook or admission problems after local API changes:
- regenerate and apply the CRD bundle first
- then run `helm upgrade` again
- confirm cert-manager and webhook-serving resources are healthy

Referenced file or secret inputs not taking effect:
- inspect `kubectl describe hermesagent <name>` for `MissingReferencedInput` or `InvalidConfig`
- verify `fileMounts.items[].key` entries actually exist in the referenced `Secret` or `ConfigMap`
- verify projected item paths are relative and file modes are valid

---

## 15. Cleanup

Delete the demo apps:

```sh
kubectl delete namespace openwebui hermes-demo
helm uninstall k8s-operator-hermes-agent -n k8s-operator-hermes-agent-system
helm uninstall cert-manager -n cert-manager
kubectl delete namespace argocd cert-manager k8s-operator-hermes-agent-system
kind delete cluster --name hermes-local
```

---

## Minimal happy-path checklist

If you only want the shortest possible path, this is it:

1. `kind create cluster --name hermes-local`
2. install cert-manager
3. `make docker-build IMG=k8s-operator-hermes-agent:local`
4. `kind load docker-image k8s-operator-hermes-agent:local --name hermes-local`
5. `helm upgrade --install ... ./charts/chart ...`
6. either:
   - apply `config/samples/hermes_v1alpha1_hermesagent.yaml` to validate the published `ghcr.io/xmbshwll/hermes-agent-docker:latest` image, or
   - build and load your custom HTTP-capable Hermes runtime image for Open WebUI
7. wait for the `HermesAgent` to become ready
8. if using the HTTP-capable path, smoke-test `curl http://localhost:8080/v1/models`
9. deploy Open WebUI
10. open http://localhost:3000 and chat
