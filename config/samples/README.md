# HermesAgent samples

Use the sample that matches the deployment path you want to test.
Each sample is applied directly with `kubectl apply -f`, so remove it with the matching `kubectl delete -f` command when you are done.

These samples assume the operator is already installed.
For operator-level install knobs such as secure metrics, cert-manager-backed webhooks, and Prometheus integration, see `docs/helm-values.md` and the manifests under `config/prometheus/`.

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
- set `spec.image.repository` to your Hermes runtime image
- keep `networkPolicy.enabled: true` unless you have a deliberate reason to widen egress manually

Remove it with:

```sh
kubectl delete -f config/samples/hermes_v1alpha1_hermesagent_telegram.yaml
```

## SSH terminal backend

File: `hermes_v1alpha1_hermesagent_ssh.yaml`

- Shows how to switch Hermes `config.yaml` to `ssh` and keep the fallback CR field explicit
- Supplies SSH host and user via a secret-backed environment source
- Includes an explicit file mount for SSH auth material
- Secret updates trigger a reconcile and pod rollout

Apply it with:

```sh
kubectl apply -f config/samples/hermes_v1alpha1_hermesagent_ssh.yaml
```

Before applying it:
- replace the placeholder SSH host, user, and model provider key
- add your SSH private key and `known_hosts` content to the auth secret
- set `config.raw.terminal.backend: ssh`; `spec.terminal.backend` is kept here as an explicit fallback/default
- make sure your Hermes runtime image knows how to consume the mounted SSH auth files at `/var/run/hermes/ssh`
- leave `networkPolicy.enabled: true` unless you are intentionally supplying your own policy

Remove it with:

```sh
kubectl delete -f config/samples/hermes_v1alpha1_hermesagent_ssh.yaml
```

## API server exposure with a custom runtime image

File: `hermes_v1alpha1_hermesagent_api_server.yaml`

- Exposes the Hermes pod through the built-in optional `Service`
- Uses `ClusterIP` on port `8080`
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
- do not assume a stock Hermes image provides this path by default
- remember that the operator still starts `hermes gateway`; if your runtime needs a different entrypoint or port, adjust the image rather than the CR

Remove it with:

```sh
kubectl delete -f config/samples/hermes_v1alpha1_hermesagent_api_server.yaml
```

## Open WebUI backend with a custom runtime image

File: `hermes_v1alpha1_hermesagent_openwebui.yaml`

- Exposes Hermes through a `ClusterIP` `Service` intended to be consumed by a separate Open WebUI deployment
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
- do not assume a stock Hermes image will satisfy that contract without extra runtime work
- point Open WebUI at the resulting service DNS name, for example `http://hermesagent-openwebui:8080` from the same namespace
- remember that this operator does not deploy or configure Open WebUI itself

Remove it with:

```sh
kubectl delete -f config/samples/hermes_v1alpha1_hermesagent_openwebui.yaml
```

## Plugin bundle file delivery for a custom runtime image

File: `hermes_v1alpha1_hermesagent_plugins.yaml`

- Uses `spec.fileMounts` as the preferred file-delivery mechanism
- Mounts the plugin bundle at `/var/run/hermes/plugins`
- Secret updates trigger a reconcile and pod rollout
- Only handles file delivery; it does not make Hermes discover or load plugins by itself
- Keeps plugin delivery on the existing operator API instead of introducing plugin-specific CRD fields

Apply it with:

```sh
kubectl apply -f config/samples/hermes_v1alpha1_hermesagent_plugins.yaml
```

Before applying it:
- replace the placeholder plugin file contents with your real plugin bundle
- make sure your custom Hermes runtime image knows how to discover or load plugins from `/var/run/hermes/plugins`
- do not assume stock Hermes loads arbitrary mounted plugin files automatically
- keep plugin filenames stable if your runtime expects specific entrypoints

Remove it with:

```sh
kubectl delete -f config/samples/hermes_v1alpha1_hermesagent_plugins.yaml
```
