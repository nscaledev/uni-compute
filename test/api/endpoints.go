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
