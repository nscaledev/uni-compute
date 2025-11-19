/*
Copyright 2024-2025 the Unikorn Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package api provides integration test utilities for the Compute API.
//
// # Separate Client Implementation
//
// This package intentionally maintains a separate HTTP client implementation
// (APIClient) instead of using the auto-generated OpenAPI client. This design
// choice provides several benefits:
//
// 1. **API Contract Validation**: Having an independent client implementation
// serves as a form of triangulation on API correctness. Any legitimate change
// to the OpenAPI specification must have a compensating change in this client,
// making API evolution more explicit and reviewable. Conversely, if a change
// to the API doesn't require updates here, it may indicate a problem with the
// change.
//
// 2. **Test-Specific Features**: The custom client includes features tailored
// for integration testing:
//   - W3C trace context propagation for request correlation
//   - Detailed error logging with trace IDs for debugging
//   - Flexible authentication token management
//   - Custom timeout and retry logic
//   - Direct access to HTTP status codes and response bodies
//
// # Future Improvements
//
// * The test scaffolding in this package could be factored out into a central
// location and reused across multiple integration and sub-integration test
// suites. This would reduce the cost of maintaining this code.
package api
