# Release workflow

This repository ships operator releases from Git tags.
A release produces three end-user artifacts:

- a versioned controller image in GHCR
- a versioned Helm chart in GHCR as an OCI artifact
- a versioned `install.yaml` bundle attached to the GitHub release

## Versioning

Use semantic version tags with a leading `v`:

- `v0.1.0`
- `v0.2.0`
- `v1.0.0`

The release workflow maps the tag to published artifacts like this:

- git tag: `v0.2.0`
- controller image: `ghcr.io/xmbshwll/k8s-operator-hermes-agent:v0.2.0`
- Helm chart version: `0.2.0`
- Helm chart appVersion: `0.2.0`
- release bundle: `install.yaml` attached to the `v0.2.0` GitHub release

## Published install paths

### Helm chart from GHCR

```sh
helm install k8s-operator-hermes-agent \
  oci://ghcr.io/xmbshwll/charts/k8s-operator-hermes-agent \
  --version 0.2.0 \
  --namespace k8s-operator-hermes-agent-system \
  --create-namespace
```

The packaged release chart already points at the matching published controller image.
End users do not need to rebuild the operator image or override `image.repository` / `image.tag` for normal installs.

### GitHub release bundle

```sh
kubectl apply -f \
  https://github.com/xmbshwll/k8s-operator-hermes-agent/releases/download/v0.2.0/install.yaml
```

The release bundle is generated with the same versioned controller image used by the chart.

## How to cut a release

1. Make sure the default branch is green
   - `make test`
   - `make lint`
   - `make test-e2e`
2. Confirm the Helm chart and docs are ready for the release
3. Create and push a tag

```sh
git tag -a v0.2.0 -m "Release v0.2.0"
git push origin v0.2.0
```

Pushing the tag triggers `.github/workflows/release.yml`.

## What the release workflow does

On every `v*` tag, GitHub Actions will:

1. build and push the multi-arch controller image to GHCR
2. generate `dist/install.yaml` with the tagged image reference
3. package the Helm chart with matching chart version and image defaults
4. push the packaged Helm chart to GHCR as an OCI artifact
5. attach `install.yaml`, the chart `.tgz`, and `SHA256SUMS` to the GitHub release
6. generate release notes from GitHub metadata

## Release notes

GitHub generates release notes automatically when the release job publishes the tag.
Use clear PR titles and labels so the generated notes stay readable.

Labels excluded from generated notes are configured in `.github/release.yml`.

## Local dry run

You can build the release artifacts locally before tagging:

```sh
make build-installer IMG=ghcr.io/xmbshwll/k8s-operator-hermes-agent:v0.2.0
make package-chart \
  CHART_VERSION=0.2.0 \
  CHART_APP_VERSION=0.2.0 \
  CHART_IMAGE_REPOSITORY=ghcr.io/xmbshwll/k8s-operator-hermes-agent \
  CHART_IMAGE_TAG=v0.2.0
```

This is useful for checking the chart package and installer bundle before cutting the real release.
