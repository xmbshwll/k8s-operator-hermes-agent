# HermesAgent samples

Use the sample that matches the deployment path you want to test.

## Minimal gateway

File: `hermes_v1alpha1_hermesagent.yaml`

- Smallest working `HermesAgent`
- Uses inline `config.yaml` and `gateway.json`
- Good for verifying the operator, PVC, and `StatefulSet`

Apply it with:

```sh
kubectl apply -k config/samples/
```

## Telegram gateway with secrets

File: `hermes_v1alpha1_hermesagent_telegram.yaml`

- Includes a placeholder `Secret` for Telegram credentials and model provider keys
- Imports secrets into the pod environment with `envFrom`
- Enables stricter readiness checks with `requireConnectedPlatform: true`
- Secret updates trigger a reconcile and pod rollout
- Good for a real messaging deployment path

Apply it with:

```sh
kubectl apply -f config/samples/hermes_v1alpha1_hermesagent_telegram.yaml
```

Before applying it:
- replace the placeholder secret values
- set `spec.image.repository` to your Hermes runtime image

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
