# k8s-operator-hermes-agent
// TODO(user): Add simple overview of use/purpose

## Description
// TODO(user): An in-depth paragraph about your project and overview of use

## Getting Started

### Prerequisites
- go version v1.24.6+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/k8s-operator-hermes-agent:tag
```

**Install the operator with Helm:**

```sh
make helm-deploy IMG=<some-registry>/k8s-operator-hermes-agent:tag
```

This installs the CRD, controller deployment, RBAC, and metrics service into the
`k8s-operator-hermes-agent-system` namespace by default. Override the namespace
with `HELM_NAMESPACE=<namespace>` when needed.

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from `config/samples/`:

```sh
kubectl apply -k config/samples/
```

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Uninstall the Helm release:**

```sh
make helm-uninstall
```

CRDs are kept on uninstall so existing custom resources are not removed unexpectedly.
Delete `charts/chart/crds/hermesagents.hermes.nous.ai.yaml` manually only when you
intend to remove the API from the cluster.

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/k8s-operator-hermes-agent:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/k8s-operator-hermes-agent/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

The repository includes a Helm chart under `charts/chart/`.

**Install directly from the chart:**

```sh
helm upgrade --install k8s-operator-hermes-agent ./charts/chart \
  --namespace k8s-operator-hermes-agent-system \
  --create-namespace \
  --set image.repository=<some-registry>/k8s-operator-hermes-agent \
  --set image.tag=<tag>
```

**Supported values:**
- `image.repository`
- `image.tag`
- `image.pullPolicy`
- `resources`
- `leaderElection.enabled`
- `serviceAccount.create`
- `serviceAccount.name`
- `metrics.enabled`

If you regenerate the chart with `kubebuilder edit --plugins=helm/v2-alpha --output-dir=charts`,
re-apply any manual chart customizations afterwards.

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

