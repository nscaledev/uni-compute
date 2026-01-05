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

//nolint:revive
package handler

import (
	"net/http"
	"slices"

	"github.com/unikorn-cloud/compute/pkg/openapi"
	"github.com/unikorn-cloud/compute/pkg/server/handler/cluster"
	"github.com/unikorn-cloud/compute/pkg/server/handler/region"
	"github.com/unikorn-cloud/core/pkg/server/errors"
	"github.com/unikorn-cloud/core/pkg/server/util"
	identityapi "github.com/unikorn-cloud/identity/pkg/openapi"
	"github.com/unikorn-cloud/identity/pkg/rbac"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Handler struct {
	// client gives cached access to Compute.
	client client.Client

	// namespace is where the controller is running.
	namespace string

	// options allows behaviour to be defined on the CLI.
	options *Options

	// identity is a client to access the identity service.
	identity identityapi.ClientWithResponsesInterface

	// region is a client to access regions.
	region regionapi.ClientWithResponsesInterface
}

func New(client client.Client, namespace string, options *Options, identity identityapi.ClientWithResponsesInterface, region regionapi.ClientWithResponsesInterface) (*Handler, error) {
	h := &Handler{
		client:    client,
		namespace: namespace,
		options:   options,
		identity:  identity,
		region:    region,
	}

	return h, nil
}

/*
func (h *Handler) setCacheable(w http.ResponseWriter) {
	w.Header().Add("Cache-Control", fmt.Sprintf("max-age=%d", h.options.CacheMaxAge/time.Second))
	w.Header().Add("Cache-Control", "private")
}
*/

func (h *Handler) setUncacheable(w http.ResponseWriter) {
	w.Header().Add("Cache-Control", "no-cache")
}

func (h *Handler) regionClient() *region.Client {
	return region.New(h.region)
}

func (h *Handler) GetApiV1OrganizationsOrganizationIDRegions(w http.ResponseWriter, r *http.Request, organizationID openapi.OrganizationIDParameter) {
	ctx := r.Context()

	if err := rbac.AllowOrganizationScope(ctx, "compute:regions", identityapi.Read, organizationID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	result, err := h.regionClient().List(ctx, organizationID)
	if err != nil {
		errors.HandleError(w, r, errors.OAuth2ServerError("unable to read regions").WithError(err))
		return
	}

	util.WriteJSONResponse(w, r, http.StatusOK, result)
}

func (h *Handler) GetApiV1OrganizationsOrganizationIDRegionsRegionIDFlavors(w http.ResponseWriter, r *http.Request, organizationID openapi.OrganizationIDParameter, regionID openapi.RegionIDParameter) {
	ctx := r.Context()

	if err := rbac.AllowOrganizationScope(ctx, "compute:flavors", identityapi.Read, organizationID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	result, err := h.regionClient().Flavors(ctx, organizationID, regionID)
	if err != nil {
		errors.HandleError(w, r, errors.OAuth2ServerError("unable to read flavors").WithError(err))
		return
	}

	util.WriteJSONResponse(w, r, http.StatusOK, result)
}

func (h *Handler) GetApiV1OrganizationsOrganizationIDRegionsRegionIDImages(w http.ResponseWriter, r *http.Request, organizationID openapi.OrganizationIDParameter, regionID openapi.RegionIDParameter) {
	ctx := r.Context()

	if err := rbac.AllowOrganizationScope(ctx, "compute:images", identityapi.Read, organizationID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	result, err := h.regionClient().Images(ctx, organizationID, regionID)
	if err != nil {
		errors.HandleError(w, r, errors.OAuth2ServerError("unable to read images").WithError(err))
		return
	}

	util.WriteJSONResponse(w, r, http.StatusOK, result)
}

func (h *Handler) clusterClient() *cluster.Client {
	return cluster.NewClient(h.client, h.namespace, &h.options.Cluster, h.identity, h.region)
}

func (h *Handler) GetApiV1OrganizationsOrganizationIDClusters(w http.ResponseWriter, r *http.Request, organizationID openapi.OrganizationIDParameter, params openapi.GetApiV1OrganizationsOrganizationIDClustersParams) {
	ctx := r.Context()

	result, err := h.clusterClient().List(ctx, organizationID, params)
	if err != nil {
		errors.HandleError(w, r, err)
		return
	}

	result = slices.DeleteFunc(result, func(resource openapi.ComputeClusterRead) bool {
		return rbac.AllowProjectScope(ctx, "compute:clusters", identityapi.Read, organizationID, resource.Metadata.ProjectId) != nil
	})

	h.setUncacheable(w)
	util.WriteJSONResponse(w, r, http.StatusOK, result)
}

func (h *Handler) PostApiV1OrganizationsOrganizationIDProjectsProjectIDClusters(w http.ResponseWriter, r *http.Request, organizationID openapi.OrganizationIDParameter, projectID openapi.ProjectIDParameter) {
	ctx := r.Context()

	if err := rbac.AllowProjectScope(ctx, "compute:clusters", identityapi.Create, organizationID, projectID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	request := &openapi.ComputeClusterWrite{}

	if err := util.ReadJSONBody(r, request); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	result, err := h.clusterClient().Create(ctx, organizationID, projectID, request)
	if err != nil {
		errors.HandleError(w, r, err)
		return
	}

	h.setUncacheable(w)
	util.WriteJSONResponse(w, r, http.StatusAccepted, result)
}

func (h *Handler) DeleteApiV1OrganizationsOrganizationIDProjectsProjectIDClustersClusterID(w http.ResponseWriter, r *http.Request, organizationID openapi.OrganizationIDParameter, projectID openapi.ProjectIDParameter, clusterID openapi.ClusterIDParameter) {
	ctx := r.Context()

	if err := rbac.AllowProjectScope(ctx, "compute:clusters", identityapi.Delete, organizationID, projectID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	if err := h.clusterClient().Delete(ctx, organizationID, projectID, clusterID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	h.setUncacheable(w)
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) GetApiV1OrganizationsOrganizationIDProjectsProjectIDClustersClusterID(w http.ResponseWriter, r *http.Request, organizationID openapi.OrganizationIDParameter, projectID openapi.ProjectIDParameter, clusterID openapi.ClusterIDParameter) {
	ctx := r.Context()

	result, err := h.clusterClient().Get(ctx, organizationID, projectID, clusterID)
	if err != nil {
		errors.HandleError(w, r, err)
		return
	}

	if err = rbac.AllowProjectScope(ctx, "compute:clusters", identityapi.Read, organizationID, result.Metadata.ProjectId); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	h.setUncacheable(w)
	util.WriteJSONResponse(w, r, http.StatusOK, result)
}

func (h *Handler) PutApiV1OrganizationsOrganizationIDProjectsProjectIDClustersClusterID(w http.ResponseWriter, r *http.Request, organizationID openapi.OrganizationIDParameter, projectID openapi.ProjectIDParameter, clusterID openapi.ClusterIDParameter) {
	ctx := r.Context()

	if err := rbac.AllowProjectScope(ctx, "compute:clusters", identityapi.Update, organizationID, projectID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	request := &openapi.ComputeClusterWrite{}

	if err := util.ReadJSONBody(r, request); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	if err := h.clusterClient().Update(ctx, organizationID, projectID, clusterID, request); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	h.setUncacheable(w)
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) PostApiV1OrganizationsOrganizationIDProjectsProjectIDClustersClusterIDEvict(w http.ResponseWriter, r *http.Request, organizationID openapi.OrganizationIDParameter, projectID openapi.ProjectIDParameter, clusterID openapi.ClusterIDParameter) {
	ctx := r.Context()

	if err := rbac.AllowProjectScope(ctx, "compute:clusters", identityapi.Update, organizationID, projectID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	request := &openapi.EvictionWrite{}

	if err := util.ReadJSONBody(r, request); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	if err := h.clusterClient().Evict(ctx, organizationID, projectID, clusterID, request); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	h.setUncacheable(w)
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) GetApiV1OrganizationsOrganizationIDProjectsProjectIDClustersClusterIDMachinesMachineIDConsoleoutput(w http.ResponseWriter, r *http.Request, organizationID openapi.OrganizationIDParameter, projectID openapi.ProjectIDParameter, clusterID openapi.ClusterIDParameter, machineID openapi.MachineIDParameter, params openapi.GetApiV1OrganizationsOrganizationIDProjectsProjectIDClustersClusterIDMachinesMachineIDConsoleoutputParams) {
	ctx := r.Context()

	if err := rbac.AllowProjectScope(r.Context(), "compute:clusters", identityapi.Read, organizationID, projectID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	result, err := h.clusterClient().GetConsoleOutput(ctx, organizationID, projectID, clusterID, machineID, &params)
	if err != nil {
		errors.HandleError(w, r, err)
		return
	}

	h.setUncacheable(w)
	util.WriteJSONResponse(w, r, http.StatusOK, result)
}

func (h *Handler) GetApiV1OrganizationsOrganizationIDProjectsProjectIDClustersClusterIDMachinesMachineIDConsolesessions(w http.ResponseWriter, r *http.Request, organizationID openapi.OrganizationIDParameter, projectID openapi.ProjectIDParameter, clusterID openapi.ClusterIDParameter, machineID openapi.MachineIDParameter) {
	ctx := r.Context()

	if err := rbac.AllowProjectScope(r.Context(), "compute:clusters", identityapi.Read, organizationID, projectID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	result, err := h.clusterClient().CreateConsoleSession(ctx, organizationID, projectID, clusterID, machineID)
	if err != nil {
		errors.HandleError(w, r, err)
		return
	}

	h.setUncacheable(w)
	util.WriteJSONResponse(w, r, http.StatusOK, result)
}

func (h *Handler) PostApiV1OrganizationsOrganizationIDProjectsProjectIDClustersClusterIDMachinesMachineIDHardreboot(w http.ResponseWriter, r *http.Request, organizationID openapi.OrganizationIDParameter, projectID openapi.ProjectIDParameter, clusterID openapi.ClusterIDParameter, machineID openapi.MachineIDParameter) {
	ctx := r.Context()

	if err := rbac.AllowProjectScope(ctx, "compute:clusters", identityapi.Update, organizationID, projectID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	if err := h.clusterClient().HardRebootMachine(ctx, organizationID, projectID, clusterID, machineID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	h.setUncacheable(w)
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) PostApiV1OrganizationsOrganizationIDProjectsProjectIDClustersClusterIDMachinesMachineIDSoftreboot(w http.ResponseWriter, r *http.Request, organizationID openapi.OrganizationIDParameter, projectID openapi.ProjectIDParameter, clusterID openapi.ClusterIDParameter, machineID openapi.MachineIDParameter) {
	ctx := r.Context()

	if err := rbac.AllowProjectScope(ctx, "compute:clusters", identityapi.Update, organizationID, projectID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	if err := h.clusterClient().SoftRebootMachine(ctx, organizationID, projectID, clusterID, machineID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	h.setUncacheable(w)
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) PostApiV1OrganizationsOrganizationIDProjectsProjectIDClustersClusterIDMachinesMachineIDStart(w http.ResponseWriter, r *http.Request, organizationID openapi.OrganizationIDParameter, projectID openapi.ProjectIDParameter, clusterID openapi.ClusterIDParameter, machineID openapi.MachineIDParameter) {
	ctx := r.Context()

	if err := rbac.AllowProjectScope(ctx, "compute:clusters", identityapi.Update, organizationID, projectID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	if err := h.clusterClient().StartMachine(ctx, organizationID, projectID, clusterID, machineID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	h.setUncacheable(w)
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) PostApiV1OrganizationsOrganizationIDProjectsProjectIDClustersClusterIDMachinesMachineIDStop(w http.ResponseWriter, r *http.Request, organizationID openapi.OrganizationIDParameter, projectID openapi.ProjectIDParameter, clusterID openapi.ClusterIDParameter, machineID openapi.MachineIDParameter) {
	ctx := r.Context()

	if err := rbac.AllowProjectScope(ctx, "compute:clusters", identityapi.Update, organizationID, projectID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	if err := h.clusterClient().StopMachine(ctx, organizationID, projectID, clusterID, machineID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	h.setUncacheable(w)
	w.WriteHeader(http.StatusAccepted)
}
