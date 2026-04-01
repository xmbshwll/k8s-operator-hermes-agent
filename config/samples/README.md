# HermesAgent samples

Use the sample that matches the deployment path you want to test.
Each sample is applied directly with `kubectl apply -f`, so remove it with the matching `kubectl delete -f` command when you are done.

These samples assume the operator is already installed.
For operator-level install knobs such as secure metrics, cert-manager-backed webhooks, and Prometheus integration, see `docs/helm-values.md` and the manifests under `config/prometheus/`.

Unless a sample says otherwise, the `spec.image` examples now use the published Hermes runtime image from [`ghcr.io/xmbshwll/hermes-agent-docker:v2026.3.30`](https://github.com/xmbshwll/hermes-agent-docker).

Product scope note:
- the minimal, telegram, secret-config, and ssh samples are within the supported v1 operator scope
- those supported samples now show the recommended hardened baseline with `automountServiceAccountToken: false`
- the API server and Open WebUI samples are **example-only** because they depend on a custom Hermes runtime image that serves an HTTP interface the operator does not provide
- the plugin sample is supported for file delivery only; plugin discovery and execution remain runtime-image behavior

See `docs/supported-features.md` for the canonical support matrix.

## Minimal gateway

File: `hermes_v1alpha1_hermesagent.yaml`

- Smallest working `HermesAgent`
- Uses inline `config.yaml` and `gateway.json`
- Good for verifying the operator, PVC, and `StatefulSet`

Apply it with:

```sh
kubectl apply -k config/samples/
```

Remove it with:

```sh
kubectl delete -k config/samples/
```

## Telegram gateway with secrets

File: `hermes_v1alpha1_hermesagent_telegram.yaml`

- Includes a placeholder `Secret` for Telegram credentials and model provider keys
- Imports secrets into the pod environment with `envFrom`
- Enables stricter readiness checks with `requireConnectedPlatform: true`
- Waits for `gateway_state.json` to report `gateway_state: "running"` and at least one `platforms.*.state: "connected"`
- Secret updates trigger a reconcile and pod rollout
- Good for a real messaging deployment path
- Useful for validating webhook, secret, and connected-platform readiness behavior

Apply it with:

```sh
kubectl apply -f config/samples/hermes_v1alpha1_hermesagent_telegram.yaml
```

Before applying it:
- replace the placeholder secret values
- keep `spec.image` as the published `ghcr.io/xmbshwll/hermes-agent-docker:v2026.3.30` reference, or swap in your own Hermes runtime image
- keep `networkPolicy.enabled: true` unless you have a deliberate reason to widen egress manually
- if you know the exact external or in-cluster destinations Hermes should reach, add `networkPolicy.destinations` to restrict non-DNS egress instead of relying on the broader port-only default

Remove it with:

```sh
kubectl delete -f config/samples/hermes_v1alpha1_hermesagent_telegram.yaml
```

## Secret-backed config files

File: `hermes_v1alpha1_hermesagent_secret_config.yaml`

- Stores both `config.yaml` and `gateway.json` in a Kubernetes `Secret`
- Good when the Hermes config itself is sensitive and should not live in a `ConfigMap`
- Referenced Secret changes trigger a reconcile and pod rollout

Apply it with:

```sh
kubectl apply -f config/samples/hermes_v1alpha1_hermesagent_secret_config.yaml
```

Before applying it:
- replace the placeholder config values with your real Hermes settings
- keep `spec.image` as the published `ghcr.io/xmbshwll/hermes-agent-docker:v2026.3.30` reference, or swap in your own Hermes runtime image
- remember that the operator only mounts the Secret-backed files; the runtime image still defines Hermes behavior

Remove it with:

```sh
kubectl delete -f config/samples/hermes_v1alpha1_hermesagent_secret_config.yaml
```

## SSH terminal backend

File: `hermes_v1alpha1_hermesagent_ssh.yaml`

- Shows how to switch Hermes `config.yaml` to `ssh` and optionally keep the CR fallback hint explicit
- Supplies SSH host and user via a secret-backed environment source
- Includes an explicit file mount for SSH auth material with projected keys and SSH-safe file modes
- Secret updates trigger a reconcile and pod rollout

Apply it with:

```sh
kubectl apply -f config/samples/hermes_v1alpha1_hermesagent_ssh.yaml
```

Before applying it:
- replace the placeholder SSH host, user, and model provider key
- add your SSH private key and `known_hosts` content to the auth secret; the sample projects only those keys and sets a tighter mode on the private key file
- set `config.raw.terminal.backend: ssh`; `spec.terminal.backend` is kept here only as an explicit fallback hint
- the sample already uses `ghcr.io/xmbshwll/hermes-agent-docker:v2026.3.30`; swap it only if you need a different Hermes runtime image
- leave `networkPolicy.enabled: true` unless you are intentionally supplying your own policy
- when your SSH target and any required egress proxies have stable network identities, prefer `networkPolicy.destinations` over the broader port-only default

Remove it with:

```sh
kubectl delete -f config/samples/hermes_v1alpha1_hermesagent_ssh.yaml
```

## API server exposure with a custom runtime image (example-only)

File: `hermes_v1alpha1_hermesagent_api_server.yaml`

- Exposes the Hermes pod through the built-in optional `Service`
- Demonstrates a distinct `Service` port (`80`) and runtime `targetPort` (`8080`)
- Only works when you provide a custom Hermes runtime image that already serves the HTTP API you want while still running under `hermes gateway`
- Does not imply that a stock Hermes image exposes an operator-ready HTTP API on `:8080`
- Keeps the operator focused on the Hermes workload only; it does not add an ingress, proxy, sidecar, or API shim

Apply it with:

```sh
kubectl apply -f config/samples/hermes_v1alpha1_hermesagent_api_server.yaml
```

Before applying it:
- replace the placeholder API key secret values
- set `spec.image.repository` to your custom Hermes runtime image
- make sure that image actually listens on port `8080` inside the container and exposes the API contract you expect
- adjust `service.port` and `service.targetPort` together if you want a different in-cluster port mapping
- do not assume a stock Hermes image provides this path by default
- remember that the operator still starts `hermes gateway`; if your runtime needs a different entrypoint or port, adjust the image rather than the CR

Remove it with:

```sh
kubectl delete -f config/samples/hermes_v1alpha1_hermesagent_api_server.yaml
```

## Open WebUI backend with a custom runtime image (example-only)

File: `hermes_v1alpha1_hermesagent_openwebui.yaml`

- Exposes Hermes through a `ClusterIP` `Service` intended to be consumed by a separate Open WebUI deployment
- Demonstrates a distinct `Service` port (`80`) and runtime `targetPort` (`8080`)
- Only works when you provide a custom Hermes runtime image that already serves the OpenAI-compatible or Open WebUI-compatible HTTP interface you expect
- Does not imply that a stock Hermes image is a drop-in Open WebUI backend
- Keeps Open WebUI out of the operator scope; this sample only manages the Hermes backend side

Apply it with:

```sh
kubectl apply -f config/samples/hermes_v1alpha1_hermesagent_openwebui.yaml
```

Before applying it:
- replace the placeholder API key secret values
- set `spec.image.repository` to your custom Hermes runtime image
- make sure that image serves the exact HTTP interface Open WebUI expects on port `8080`
- adjust `service.port` and `service.targetPort` together if you want a different in-cluster port mapping
- do not assume a stock Hermes image will satisfy that contract without extra runtime work
- point Open WebUI at the resulting service DNS name, for example `http://hermesagent-openwebui:80` from the same namespace
- remember that this operator does not deploy or configure Open WebUI itself

Remove it with:

```sh
kubectl delete -f config/samples/hermes_v1alpha1_hermesagent_openwebui.yaml
```

## Plugin bundle file delivery for a custom runtime image

File: `hermes_v1alpha1_hermesagent_plugins.yaml`

- Uses `spec.fileMounts` as the preferred file-delivery mechanism, including explicit key selection
- This sample is supported for operator-side file delivery only; it is not a promise that the runtime image will auto-load or execute the mounted plugin bundle
- Mounts a projected plugin bundle at `/var/run/hermes/plugins`
- Secret updates trigger a reconcile and pod rollout
- Only handles file delivery; it does not make Hermes discover or load plugins by itself
- Keeps plugin delivery on the existing operator API instead of introducing plugin-specific CRD fields

Apply it with:

```sh
kubectl apply -f config/samples/hermes_v1alpha1_hermesagent_plugins.yaml
```

Before applying it:
- replace the placeholder plugin file contents with your real plugin bundle
- the sample already uses `ghcr.io/xmbshwll/hermes-agent-docker:v2026.3.30`; swap it if your plugin workflow needs a different Hermes runtime image
- do not assume stock Hermes loads arbitrary mounted plugin files automatically
- keep plugin filenames stable if your runtime expects specific entrypoints

Remove it with:

```sh
kubectl delete -f config/samples/hermes_v1alpha1_hermesagent_plugins.yaml
```
