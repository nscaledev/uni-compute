# Compute Service

## Overview

The compute service exposes a higher-level `Instance` abstraction backed by
hidden compute-server lifecycle provided by the
[Region service](https://github.com/nscaledev/uni-region).

Users interact with `ComputeInstance` resources and the `/api/v2/instances`
API. The compute controller then realizes that desired state as hidden
`region.Server` lifecycle, projects server status back onto the instance, and
coordinates quota/accounting at the instance abstraction boundary.

Historically this service is close to the
[Kubernetes service](https://github.com/nscaledev/uni-kubernetes), and where
possible type and API parity are still useful for UX tooling and shared service
integration. That historical similarity is not the main architectural fact
about this repository though: the important distinction is the visible
instance-versus-hidden-server split.

## Architecture Documentation

Package-level architecture and lifecycle documentation lives under
[`pkg/README.md`](./pkg/README.md).

Recommended entry points:

- [`pkg/README.md`](./pkg/README.md) for the service-level package graph and
  lifecycle summary
- [`pkg/apis/unikorn/v1alpha1/README.md`](./pkg/apis/unikorn/v1alpha1/README.md)
  for the persisted `ComputeInstance` resource model
- [`pkg/server/handler/instance/README.md`](./pkg/server/handler/instance/README.md)
  for the public `Instance` API behaviour
- [`pkg/provisioners/managers/instance/README.md`](./pkg/provisioners/managers/instance/README.md)
  for the controller-side realization of hidden backing `region.Server`
  lifecycle

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

This service requires access to both identity and region APIs in order to:

- authorize instance operations
- charge and release project-scoped resource allocations
- validate region-owned resources such as networks, flavors, images, security
  groups, and SSH certificate authorities
- create, update, delete, and operate the hidden backing `region.Server`
  associated with each instance

This service defines the `unikorn-compute` user that will need to be added to a group in the service organization.
It will need the built in role `infra-manager-service` that allows:

* Access to allocation endpoints in `identity` to create, update, and delete
  compute-related resource allocations
* Read access to `region` resources used to validate and scope instances, such
  as networks, flavors, images, security groups, and SSH certificate
  authorities
* Create/Read/Update/Delete and operational access to `region` `servers`
  endpoints in order to realize and operate hidden backing server lifecycle for
  compute instances

For the code-level architecture behind that split, start with
[`pkg/README.md`](./pkg/README.md).

## Testing

### API Integration Tests

The compute service includes comprehensive API integration tests that validate instance operations, security, and authentication.

#### Test Configuration

Tests are configured via environment variables using a `.env` file in the `test/` directory.

**Setup:**

1. **Set up your environment configuration:**

   Copy the example config and update with your values:
   ```bash
   cp test/.env.example test/.env
   ```

   Or create environment-specific files (not tracked in git):
   ```bash
   # Create .env.dev with your dev credentials
   cp test/.env.example test/.env.dev
   # Edit test/.env.dev with dev values

   # Create .env.uat with your UAT credentials
   cp test/.env.example test/.env.uat
   # Edit test/.env.uat with UAT values

   # Use the appropriate environment
   cp test/.env.dev test/.env    # For dev environment
   cp test/.env.uat test/.env    # For UAT environment
   ```

2. **Configure the required values in `test/.env`:**
   - `API_BASE_URL` - Compute API server URL
   - `API_AUTH_TOKEN` - Service token from console
   - `TEST_ORG_ID`, `TEST_PROJECT_ID` - Test organization and project IDs
   - `TEST_REGION_ID` - Test region ID
   - `TEST_NETWORK_ID` - Test network ID
   - `TEST_FLAVOR_ID`, `TEST_IMAGE_ID` - Test flavor and image IDs
**Note:** All `test/.env` and `test/.env.*` files are gitignored and contain sensitive credentials. They should never be committed to the repository. You can use either `test/.env` directly or create environment-specific files like `test/.env.dev`, `test/.env.uat`, etc.

#### Running Tests Locally (run from project root)

**Run all tests:**
```bash
make test-api
```

**Run all tests in parallel:**
```bash
make test-api-parallel
```

**Run specific test suite using focus:**
```bash
# Example: run only instance operations tests
make test-api-focus FOCUS="Instance Operations"
```

**Run specific test spec using focus:**
```bash
# Example: run only a specific test spec by name
make test-api-focus FOCUS="should successfully stop a running instance"
```

The instance operations suite checks for leftover API test instances in the
configured project before running its specs. CI-created instances use the
`e2e-uni-compute-ci-` prefix, local instances use `e2e-uni-compute-local-`, and
cleanup only requests deletion for instances older than one hour whose names
match the current environment prefix. It prints GitHub Actions `::error::`
annotations and continues without deleting instances from other active builds.

**Advanced Ginkgo options:**
```bash
# Run with different parallel workers
cd test/api/suites && ginkgo run --procs=8 --json-report=test-results.json

# Run with verbose output
cd test/api/suites && ginkgo run -v --show-node-events

# Skip specific tests
cd test/api/suites && ginkgo run --skip="Security and Authentication"

# Randomize test order
cd test/api/suites && ginkgo run --randomize-all
```

#### GitHub Actions Workflow

The API tests can be triggered manually via GitHub Actions using `workflow_dispatch`:

**Workflow Inputs:**

| Input | Type | Description | Default |
|-------|------|-------------|---------|
| `run_dev` | boolean | Run Dev environment tests | `true` |
| `run_uat` | boolean | Run UAT environment tests | `false` |
| `focus` | choice | Test suite to run | `All` |
| `region_id_override` | string | Override region ID for manual runs | unset |
| `flavor_id_override` | string | Override flavor ID when overriding region | unset |
| `image_id_override` | string | Override image ID when overriding region | unset |
| `network_id_override` | string | Override network ID when overriding region | unset |

**Available Test Suite Options:**
- `All` - Run all test suites
- `Instance Operations` - Instance lifecycle and power operation tests
- `Security and Authentication` - Authentication and input validation tests

If `region_id_override` is set, `flavor_id_override`, `image_id_override`, and
`network_id_override` must also be provided.

**Triggering Manually:**

1. Navigate to **Actions** tab in GitHub
2. Select **API Tests** workflow
3. Click **Run workflow**
4. Select which environments to test:
   - **Run Dev tests** (checked by default)
   - **Run UAT tests** (unchecked by default)
5. Choose test suite from the **focus** dropdown
6. Click **Run workflow**

**Test Artifacts:**

After each run, test results are uploaded as artifacts per environment:
- `api-test-results-dev` / `api-test-results-uat` - JSON format test results
- `api-test-junit-dev` / `api-test-junit-uat` - JUnit XML format for CI integration

#### Cleaning Up Test Artifacts locally.

```bash
make test-api-clean
```

### Contract Testing

The compute service uses consumer-driven contract testing to validate interactions with dependent services (e.g., uni-region, uni-identity) without requiring full service deployments.

#### Prerequisites

**Install Pact FFI Library:**

Consumer contract tests require the Pact FFI library to be installed locally.

**macOS:**
```bash
brew tap pact-foundation/pact-ruby-standalone
brew install pact-ruby-standalone
mkdir -p $HOME/Library/pact
cp /usr/local/opt/pact-ruby-standalone/libexec/lib/*.dylib $HOME/Library/pact/
```

**Start Pact Broker:**

The Pact Broker is required for publishing and managing contracts. Reference the uni-core repository's make target for starting a local broker instance, or run:

```bash
docker run -d --name pact-broker \
  -p 9292:9292 \
  -e PACT_BROKER_DATABASE_URL=sqlite:///pact_broker.sqlite \
  pactfoundation/pact-broker:latest
```

#### Running Consumer Contract Tests

**Run all consumer tests:**
```bash
make test-contracts-consumer
```

**Publish pacts to broker:**
```bash
make publish-pacts
```

**Available make targets:**
- `test-contracts-consumer` - Run consumer contract tests
- `publish-pacts` - Publish generated pacts to Pact Broker
- `can-i-deploy` - Check if service version is safe to deploy
- `record-deployment` - Record deployment to an environment
- `clean-contracts` - Clean generated pact files

**Configuration:**

Broker settings can be configured via environment variables or Makefile defaults:
- `PACT_BROKER_URL` - Broker base URL (default: `http://localhost:9292`)
- `PACT_BROKER_USERNAME` - Broker username (default: `pact`)
- `PACT_BROKER_PASSWORD` - Broker password (default: `pact`)

#### Writing Consumer Tests

Consumer tests follow a standard pattern:

1. Create a Pact mock provider using `contract.NewV4Pact()`
2. Define interactions with `Given()`, `UponReceiving()`, `WithRequest()`, `WillRespondWith()`
3. Execute the test using your actual client code against the mock server
4. Verify expectations using Gomega matchers
(you can and should use the OpenAPI spec as a guide here for building tests)

**Example structure:**
```go
var _ = Describe("Provider Service Contract", func() {
    var pact *consumer.V4HTTPMockProvider

    BeforeEach(func() {
        pact, _ = contract.NewV4Pact(contract.PactConfig{
            Consumer: "uni-compute",
            Provider: "uni-region",
            PactDir:  "../pacts",
        })
    })

    It("returns expected response", func() {
        pact.AddInteraction().
            GivenWithParameter(...).
            UponReceiving("a request").
            WithRequest("GET", "/api/v1/endpoint").
            WillRespondWith(200, func(b *consumer.V4ResponseBuilder) {
                b.JSONBody(...)
            })

        test := func(config consumer.MockServerConfig) error {
            // Use actual client code here
            return nil
        }

        Expect(pact.ExecuteTest(testingT, test)).To(Succeed())
    })
})
```

For complete examples, see:
- `test/contracts/consumer/region/regions_test.go` - Region service consumer tests
- `test/contracts/consumer/identity/identity_test.go` - Identity service consumer tests

#### Consumer Contracts with uni-identity

The compute service defines consumer contracts for interactions with the identity service resource allocation API:

- Create allocation - Track resource usage when instances are created
- Update allocation - Update resource counts when scaling operations occur
- Delete allocation - Release resource allocations during cleanup

#### Emergency Escape Hatch

In exceptional circumstances (e.g. a hotfix that can't wait for contract tests to be updated), contract testing can be bypassed by adding the `skip-contract-tests` label to a PR.

When this label is present, both the `ConsumerContractTests` and `CanIDeploy` CI jobs are skipped. Skipped jobs show as neutral (green) in GitHub and satisfy required status checks, so the PR can still be merged.

**This label should only be used as a last resort.** Its use is visible in the PR timeline and auditable. After merging, the contract tests must be updated and the label removed before the next PR.
