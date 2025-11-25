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

//nolint:revive
package handler

import (
	"net/http"

	"github.com/unikorn-cloud/compute/pkg/openapi"
	"github.com/unikorn-cloud/compute/pkg/server/handler/instance"
	"github.com/unikorn-cloud/core/pkg/server/errors"
	"github.com/unikorn-cloud/core/pkg/server/util"
)

func (h *Handler) instanceClient() *instance.Client {
	return instance.NewClient(h.client, h.namespace, h.getIdentityAPIClient, h.getRegionAPIClient)
}

func (h *Handler) GetApiV2Instances(w http.ResponseWriter, r *http.Request, params openapi.GetApiV2InstancesParams) {
	result, err := h.instanceClient().List(r.Context(), params)
	if err != nil {
		errors.HandleError(w, r, err)
		return
	}

	util.WriteJSONResponse(w, r, http.StatusOK, result)
}

func (h *Handler) PostApiV2Instances(w http.ResponseWriter, r *http.Request) {
	request := &openapi.InstanceCreate{}

	if err := util.ReadJSONBody(r, request); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	result, err := h.instanceClient().Create(r.Context(), request)
	if err != nil {
		errors.HandleError(w, r, err)
		return
	}

	util.WriteJSONResponse(w, r, http.StatusCreated, result)
}

func (h *Handler) GetApiV2InstancesInstanceID(w http.ResponseWriter, r *http.Request, instanceID openapi.InstanceIDParameter) {
	result, err := h.instanceClient().Get(r.Context(), instanceID)
	if err != nil {
		errors.HandleError(w, r, err)
		return
	}

	util.WriteJSONResponse(w, r, http.StatusOK, result)
}

func (h *Handler) PutApiV2InstancesInstanceID(w http.ResponseWriter, r *http.Request, instanceID openapi.InstanceIDParameter) {
	request := &openapi.InstanceUpdate{}

	if err := util.ReadJSONBody(r, request); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	result, err := h.instanceClient().Update(r.Context(), instanceID, request)
	if err != nil {
		errors.HandleError(w, r, err)
		return
	}

	util.WriteJSONResponse(w, r, http.StatusAccepted, result)
}

func (h *Handler) DeleteApiV2InstancesInstanceID(w http.ResponseWriter, r *http.Request, instanceID openapi.InstanceIDParameter) {
	if err := h.instanceClient().Delete(r.Context(), instanceID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) GetApiV2InstancesInstanceIDSshkey(w http.ResponseWriter, r *http.Request, instanceID openapi.InstanceIDParameter) {
	result, err := h.instanceClient().SSHKey(r.Context(), instanceID)
	if err != nil {
		errors.HandleError(w, r, err)
		return
	}

	util.WriteJSONResponse(w, r, http.StatusOK, result)
}

func (h *Handler) PostApiV2InstancesInstanceIDStart(w http.ResponseWriter, r *http.Request, instanceID openapi.InstanceIDParameter) {
	if err := h.instanceClient().Start(r.Context(), instanceID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) PostApiV2InstancesInstanceIDStop(w http.ResponseWriter, r *http.Request, instanceID openapi.InstanceIDParameter) {
	if err := h.instanceClient().Stop(r.Context(), instanceID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) PostApiV2InstancesInstanceIDReboot(w http.ResponseWriter, r *http.Request, instanceID openapi.InstanceIDParameter, params openapi.PostApiV2InstancesInstanceIDRebootParams) {
	if err := h.instanceClient().Reboot(r.Context(), instanceID, params); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) GetApiV2InstancesInstanceIDConsoleoutput(w http.ResponseWriter, r *http.Request, instanceID openapi.InstanceIDParameter, params openapi.GetApiV2InstancesInstanceIDConsoleoutputParams) {
	result, err := h.instanceClient().ConsoleOutput(r.Context(), instanceID, params)
	if err != nil {
		errors.HandleError(w, r, err)
		return
	}

	util.WriteJSONResponse(w, r, http.StatusOK, result)
}

func (h *Handler) GetApiV2InstancesInstanceIDConsolesession(w http.ResponseWriter, r *http.Request, instanceID openapi.InstanceIDParameter) {
	result, err := h.instanceClient().ConsoleSession(r.Context(), instanceID)
	if err != nil {
		errors.HandleError(w, r, err)
		return
	}

	util.WriteJSONResponse(w, r, http.StatusOK, result)
}

func (h *Handler) GetApiV2Clusters(w http.ResponseWriter, r *http.Request, params openapi.GetApiV2ClustersParams) {
	result, err := h.clusterClient().ListV2(r.Context(), params)
	if err != nil {
		errors.HandleError(w, r, err)
		return
	}

	util.WriteJSONResponse(w, r, http.StatusOK, result)
}

func (h *Handler) PostApiV2Clusters(w http.ResponseWriter, r *http.Request) {
	request := &openapi.ClusterV2Create{}

	if err := util.ReadJSONBody(r, request); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	result, err := h.clusterClient().CreateV2(r.Context(), request)
	if err != nil {
		errors.HandleError(w, r, err)
		return
	}

	util.WriteJSONResponse(w, r, http.StatusCreated, result)
}

func (h *Handler) GetApiV2ClustersClusterID(w http.ResponseWriter, r *http.Request, clusterID openapi.ClusterIDParameter) {
	result, err := h.clusterClient().GetV2(r.Context(), clusterID)
	if err != nil {
		errors.HandleError(w, r, err)
		return
	}

	util.WriteJSONResponse(w, r, http.StatusOK, result)
}

func (h *Handler) PutApiV2ClustersClusterID(w http.ResponseWriter, r *http.Request, clusterID openapi.ClusterIDParameter) {
	request := &openapi.ClusterV2Update{}

	if err := util.ReadJSONBody(r, request); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	result, err := h.clusterClient().UpdateV2(r.Context(), clusterID, request)
	if err != nil {
		errors.HandleError(w, r, err)
		return
	}

	util.WriteJSONResponse(w, r, http.StatusAccepted, result)
}

func (h *Handler) DeleteApiV2ClustersClusterID(w http.ResponseWriter, r *http.Request, clusterID openapi.ClusterIDParameter) {
	if err := h.clusterClient().DeleteV2(r.Context(), clusterID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}
