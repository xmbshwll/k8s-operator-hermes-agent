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

- Shows how to switch the CR to `terminal.backend: ssh`
- Supplies SSH host and user via a secret-backed environment source
- Includes a mounted secret bundle for SSH auth material
- Secret updates trigger a reconcile and pod rollout

Apply it with:

```sh
kubectl apply -f config/samples/hermes_v1alpha1_hermesagent_ssh.yaml
```

Before applying it:
- replace the placeholder SSH host, user, and model provider key
- add your SSH private key and `known_hosts` content to the auth secret
- make sure your Hermes runtime image knows how to consume the mounted SSH auth bundle
- leave `networkPolicy.enabled: true` unless you are intentionally supplying your own policy

Remove it with:

```sh
kubectl delete -f config/samples/hermes_v1alpha1_hermesagent_ssh.yaml
```
