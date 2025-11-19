# Compute Service

## Overview

The compute service is essentially a cut down version of the [Kubernetes service](https://github.com/nscaledev/uni-kubernetes) that provisions its own compute servers using hardware abstraction provided by the [Region service](https://github.com/nscaledev/uni-region).

Where possible, as the Compute service is very similar to the Kubernetes service, we must maintain type and API parity to ease creation of UX tools and services.

## Installation

### Prerequisites

To use the Compute service you first need to install:

* [The identity service](https://github.com/nscaledev/uni-identity) to provide API authentication and authorization.
* [The region service](https://github.com/nscaledev/uni-region) to provide provider agnostic cloud services (e.g. images, flavors and identity management).

### Installing the Service

#### Installing Prerequisites

The compute server component has a couple prerequisites that are required for correct functionality.
If not installing the server component, skip to the next section.

You'll need to install:

* cert-manager (used to generate keying material for JWE/JWS and for ingress TLS)
* nginx-ingress (to perform routing, avoiding CORS, and TLS termination)

#### Installing the Compute Service

<details>
<summary>Helm</summary>

Create a `values.yaml` for the server component:
A typical `values.yaml` that uses cert-manager and ACME, and external DNS might look like:

```yaml
global:
  identity:
    host: https://identity.unikorn-cloud.org
  region:
    host: https://region.unikorn-cloud.org
  compute:
    host: https://compute.unikorn-cloud.org
```

```shell
helm install unikorn-compute charts/compute --namespace unikorn-compute --create-namespace --values values.yaml
```

</details>

<details>
<summary>ArgoCD</summary>

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: unikorn-compute
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://unikorn-cloud.github.io/compute
    chart: compute
    targetRevision: v0.1.0
  destination:
    namespace: unikorn
    server: https://kubernetes.default.svc
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
    - CreateNamespace=true
```

</details>

### Configuring Service Authentication and Authorization

The [Identity Service](https://github.com/nscaledev/uni-identity) describes how to configure a service organization, groups and role mappings for services that require them.

This service requires asynchronous access to the Region API in order to poll cloud identity and physical network status during cluster creation, and delete those resources on cluster deletion.

This service defines the `unikorn-compute` user that will need to be added to a group in the service organization.
It will need the built in role `infra-manager-service` that allows:

* Read access to the `region` endpoints to access external networks
* Read/delete access to the `identites` endpoints to poll and delete cloud identities
* Read/delete access to the `physicalnetworks` endpoints to poll and delete physical networks
* Create/Read/Delete access to the `servers` endpoints to manage compute instances

## Testing

### API Integration Tests

The compute service includes comprehensive API integration tests that validate cluster lifecycle management, machine operations, security, and metadata discovery endpoints.

#### Test Configuration

Tests are configured via environment variables. It's recommended you create a `.env` file in the `test/` directory; there is a template `.env.example` you can copy and adapt.

**Required Environment Variables:**

```bash
# API endpoints
API_BASE_URL=https://compute.your-domain.org
IDENTITY_BASE_URL=https://identity.your-domain.org

# Authentication
API_AUTH_TOKEN=your-auth-token-here

# Test resources
TEST_ORG_ID=your-organization-id
TEST_PROJECT_ID=your-project-id
TEST_SECONDARY_PROJECT_ID=secondary-project-id
TEST_REGION_ID=your-region-id
TEST_SECONDARY_REGION_ID=secondary-region-id
TEST_FLAVOR_ID=your-flavor-id
TEST_IMAGE_ID=your-image-id

# Optional configuration
REQUEST_TIMEOUT=30s           # Default: 30s
TEST_TIMEOUT=20m              # Default: 20m
DEBUG_LOGGING=false           # Default: false
LOG_REQUESTS=false            # Default: false
LOG_RESPONSES=false           # Default: false
```

#### Running Tests Locally (run from project root)

**Run all tests:**
```bash
make test-api
```

**Run all tests in parallel (not yet implemeted):**
```bash
make test-api-parallel
```

**Run specific test suite using focus:**
```bash
# Example Run only cluster management tests, which is the suite name
make test-api-focus FOCUS="Core Cluster Management"
```

**Run specific test spec using focus:**
```bash
# Example Run only the return all clusters test spec, which uses the test spec name.
make test-api-focus FOCUS="should return all clusters for the organization"
```

**Advanced Ginkgo options:**
```bash
# Run with different parallel workers
cd test/api/suites && ginkgo run --procs=8 --json-report=test-results.json

# Run with verbose output
cd test/api/suites && ginkgo run -v --show-node-events

# Skip specific tests
cd test/api/suites && ginkgo run --skip="Machine Operations"

# Randomize test order
cd test/api/suites && ginkgo run --randomize-all
```

#### GitHub Actions Workflow

The API tests can be triggered manually via GitHub Actions using `workflow_dispatch`:

**Workflow Inputs:**

| Input | Type | Description | Default |
|-------|------|-------------|---------|
| `focus` | choice | Test suite to run | `All` |
| `parallel` | boolean | Run tests in parallel | `false` |

**Available Test Suite Options:**
- `All` - Run all test suites
- `Core Cluster Management` - Cluster CRUD operations and lifecycle tests
- `Discovery and Metadata` - Region, flavor, and image discovery tests
- `Security and Authentication` - Authentication and input validation tests
- `Machine Operations` - Machine power operations and eviction tests

**Triggering Manually:**

1. Navigate to **Actions** tab in GitHub
2. Select **API Tests** workflow
3. Click **Run workflow**
4. Choose test suite
5. Click **Run workflow**

**Automatic Triggers:**

Tests automatically run on pushes to `main` branch

**Test Artifacts:**

After each run, test results are uploaded as artifacts:
- `api-test-results` - JSON format test results
- `api-test-junit` - JUnit XML format for CI integration

#### Cleaning Up Test Artifacts locally.

```bash
make test-api-clean
```
