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
   - `IDENTITY_BASE_URL` - Identity API server URL
   - `API_AUTH_TOKEN` - Service token from console
   - `TEST_ORG_ID`, `TEST_PROJECT_ID`, `TEST_SECONDARY_PROJECT_ID` - Test organization and project IDs
   - `TEST_REGION_ID`, `TEST_SECONDARY_REGION_ID` - Test region IDs
   - `TEST_NETWORK_ID` - Test network ID
   - `TEST_FLAVOR_ID`, `TEST_IMAGE_ID` - Test flavor and image IDs

**Note:** All `test/.env` and `test/.env.*` files are gitignored and contain sensitive credentials. They should never be committed to the repository. You can use either `test/.env` directly or create environment-specific files like `test/.env.dev`, `test/.env.uat`, etc.

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
| `run_dev` | boolean | Run Dev environment tests | `true` |
| `run_uat` | boolean | Run UAT environment tests | `false` |
| `focus` | choice | Test suite to run | `All` |

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

**Setup webhook for provider verification (one-time setup per environment):**

This webhook triggers uni-region's CI to automatically verify contracts when they change:

```bash
# Create a GitHub Personal Access Token with 'repo' scope
# Then run:
GITHUB_TOKEN="ghp_your_token_here" make setup-webhook

# Verify webhook was created
make list-webhooks
```

**Available make targets:**
- `test-contracts-consumer` - Run consumer contract tests
- `publish-pacts` - Publish generated pacts to Pact Broker
- `can-i-deploy` - Check if service version is safe to deploy
- `record-deployment` - Record deployment to an environment
- `setup-webhook` - Configure webhook to trigger provider verification (one-time setup)
- `update-webhook` - Update existing webhook (use with WEBHOOK_UUID variable)
- `list-webhooks` - List all webhooks configured in the Pact Broker
- `show-webhook` - Show detailed webhook info (use with WEBHOOK_UUID variable)
- `test-webhook` - Manually trigger webhook to test it (use with WEBHOOK_UUID variable)
- `open-webhook` - Open webhook in browser for editing (use with WEBHOOK_UUID variable)
- `delete-webhook` - Delete a webhook by UUID (use with WEBHOOK_UUID variable)
- `clean-contracts` - Clean generated pact files

**Note:** To enable/disable webhooks, use `make open-webhook` to open the Pact Broker UI in your browser, or delete and recreate the webhook as needed.

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

For complete examples, see `test/contracts/consumer/region/regions_test.go`.
