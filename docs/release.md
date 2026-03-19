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

- git tag: `v<version>`
- controller image: `ghcr.io/xmbshwll/k8s-operator-hermes-agent:v<version>`
- Helm chart version: `<version>`
- Helm chart appVersion: `<version>`
- release bundle: `install.yaml` attached to the `v<version>` GitHub release

## Published install paths

### Helm chart from GHCR

```sh
helm install k8s-operator-hermes-agent \
  oci://ghcr.io/xmbshwll/charts/k8s-operator-hermes-agent \
  --version <version> \
  --namespace k8s-operator-hermes-agent-system \
  --create-namespace
```

The packaged release chart already points at the matching published controller image.
End users do not need to rebuild the operator image or override `image.repository` / `image.tag` for normal installs.

Published releases enable admission webhooks by default, so the target cluster must already have cert-manager installed.

### GitHub release bundle

```sh
kubectl apply -f \
  https://github.com/xmbshwll/k8s-operator-hermes-agent/releases/download/v<version>/install.yaml
```

The release bundle is generated with the same versioned controller image used by the chart.
It also expects cert-manager to be installed before applying the bundle because the deployment includes webhook certificate resources.

## How to cut a release

1. Make sure the default branch is green
   - `make test`
   - `make lint`
   - `make test-e2e`
2. Confirm the Helm chart and docs are ready for the release
3. Create and push a tag

```sh
git tag -a v<version> -m "Release v<version>"
git push origin v<version>
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
make build-installer IMG=ghcr.io/xmbshwll/k8s-operator-hermes-agent:v<version>
make package-chart \
  CHART_VERSION=<version> \
  CHART_APP_VERSION=<version> \
  CHART_IMAGE_REPOSITORY=ghcr.io/xmbshwll/k8s-operator-hermes-agent \
  CHART_IMAGE_TAG=v<version>
```

This is useful for checking the chart package and installer bundle before cutting the real release.
