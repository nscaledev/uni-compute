/*
Copyright 2024-2025 the Unikorn Authors.
Copyright 2026 Nscale.

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

package api

import (
	"fmt"
	"net/url"
)

// Endpoints contains all API endpoint patterns.
type Endpoints struct{}

// NewEndpoints creates a new Endpoints instance.
func NewEndpoints() *Endpoints {
	return &Endpoints{}
}

// Discovery endpoints.
func (e *Endpoints) ListRegions(orgID string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/regions",
		url.PathEscape(orgID))
}

func (e *Endpoints) ListFlavors(orgID, regionID string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/regions/%s/flavors",
		url.PathEscape(orgID), url.PathEscape(regionID))
}

func (e *Endpoints) ListImages(orgID, regionID string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/regions/%s/images",
		url.PathEscape(orgID), url.PathEscape(regionID))
}

// Cluster management endpoints.
func (e *Endpoints) ListClusters(orgID, projectID string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/projects/%s/clusters",
		url.PathEscape(orgID), url.PathEscape(projectID))
}

func (e *Endpoints) ListOrganizationClusters(orgID string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/clusters",
		url.PathEscape(orgID))
}

func (e *Endpoints) GetCluster(orgID, projectID, clusterID string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/projects/%s/clusters/%s",
		url.PathEscape(orgID), url.PathEscape(projectID), url.PathEscape(clusterID))
}

func (e *Endpoints) CreateCluster(orgID, projectID string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/projects/%s/clusters",
		url.PathEscape(orgID), url.PathEscape(projectID))
}

func (e *Endpoints) UpdateCluster(orgID, projectID, clusterID string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/projects/%s/clusters/%s",
		url.PathEscape(orgID), url.PathEscape(projectID), url.PathEscape(clusterID))
}

func (e *Endpoints) DeleteCluster(orgID, projectID, clusterID string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/projects/%s/clusters/%s",
		url.PathEscape(orgID), url.PathEscape(projectID), url.PathEscape(clusterID))
}

// Machine operation endpoints.
func (e *Endpoints) StartMachine(orgID, projectID, clusterID, machineID string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/projects/%s/clusters/%s/machines/%s/start",
		url.PathEscape(orgID), url.PathEscape(projectID), url.PathEscape(clusterID), url.PathEscape(machineID))
}

func (e *Endpoints) StopMachine(orgID, projectID, clusterID, machineID string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/projects/%s/clusters/%s/machines/%s/stop",
		url.PathEscape(orgID), url.PathEscape(projectID), url.PathEscape(clusterID), url.PathEscape(machineID))
}

func (e *Endpoints) SoftRebootMachine(orgID, projectID, clusterID, machineID string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/projects/%s/clusters/%s/machines/%s/softreboot",
		url.PathEscape(orgID), url.PathEscape(projectID), url.PathEscape(clusterID), url.PathEscape(machineID))
}

func (e *Endpoints) HardRebootMachine(orgID, projectID, clusterID, machineID string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/projects/%s/clusters/%s/machines/%s/hardreboot",
		url.PathEscape(orgID), url.PathEscape(projectID), url.PathEscape(clusterID), url.PathEscape(machineID))
}

func (e *Endpoints) EvictMachines(orgID, projectID, clusterID string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/projects/%s/clusters/%s/evict",
		url.PathEscape(orgID), url.PathEscape(projectID), url.PathEscape(clusterID))
}

// Instance management endpoints (V2 API).
func (e *Endpoints) CreateInstance() string {
	return "/api/v2/instances"
}

func (e *Endpoints) GetInstance(instanceID string) string {
	return fmt.Sprintf("/api/v2/instances/%s", url.PathEscape(instanceID))
}

func (e *Endpoints) DeleteInstance(instanceID string) string {
	return fmt.Sprintf("/api/v2/instances/%s", url.PathEscape(instanceID))
}

func (e *Endpoints) GetInstanceConsoleOutput(instanceID string) string {
	return fmt.Sprintf("/api/v2/instances/%s/consoleoutput", url.PathEscape(instanceID))
}

func (e *Endpoints) GetInstanceSnapshot(instanceID string) string {
	return fmt.Sprintf("/api/v2/instances/%s/snapshot", url.PathEscape(instanceID))
}

// Health and metadata endpoints.
func (e *Endpoints) HealthCheck() string {
	return "/api/v1/health"
}

func (e *Endpoints) OpenAPISpec() string {
	return "/api/v1/openapi.json"
}

func (e *Endpoints) Version() string {
	return "/api/v1/version"
}
