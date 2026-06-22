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
	"fmt"
	"net/http"

	"github.com/unikorn-cloud/compute/pkg/openapi"
	"github.com/unikorn-cloud/compute/pkg/server/handler/region"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	"github.com/unikorn-cloud/core/pkg/server/errors"
	"github.com/unikorn-cloud/core/pkg/server/util"
	identityapi "github.com/unikorn-cloud/identity/pkg/openapi"
	"github.com/unikorn-cloud/identity/pkg/principal"
	"github.com/unikorn-cloud/identity/pkg/rbac"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Handler struct {
	client    client.Client
	namespace string
	options   *Options
	identity  identityapi.ClientWithResponsesInterface
	region    regionapi.ClientWithResponsesInterface
}

func New(client client.Client, namespace string, options *Options, identity identityapi.ClientWithResponsesInterface, region regionapi.ClientWithResponsesInterface) (*Handler, error) {
	return &Handler{
		client:    client,
		namespace: namespace,
		options:   options,
		identity:  identity,
		region:    region,
	}, nil
}

// GetWellKnownOpenidProtectedResource implements RFC 9728.
func (h *Handler) GetWellKnownOpenidProtectedResource(w http.ResponseWriter, r *http.Request) {
	authenticationServer, err := identityapi.Host(h.identity)
	if err != nil {
		errors.HandleError(w, r, err)
		return
	}

	result := &coreapi.OpenidProtectedResource{
		Resource: "https://" + r.Host,
		AuthorizationServers: coreapi.AuthorizationServerList{
			authenticationServer,
		},
		ScopesSupported: coreapi.ScopeList{
			"openid",
			"email",
			"profile",
		},
		BearerMethodsSupported: coreapi.BearerMethodList{
			coreapi.Header,
		},
	}

	h.options.setCacheable(w)
	util.WriteJSONResponse(w, r, http.StatusOK, result)
}

func (h *Handler) regionClient() *region.Client {
	return region.New(h.region)
}

func (h *Handler) GetApiV1OrganizationsOrganizationIDRegions(w http.ResponseWriter, r *http.Request, organizationID openapi.OrganizationIDParameter) {
	ctx := r.Context()

	if err := rbac.AllowOrganizationScopeID(ctx, "compute:regions", identityapi.Read, organizationID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	ctx = principal.NewImpersonateContext(ctx)

	result, err := h.regionClient().List(ctx, organizationID)
	if err != nil {
		errors.HandleError(w, r, fmt.Errorf("%w: unable to read regions", err))
		return
	}

	util.WriteJSONResponse(w, r, http.StatusOK, result)
}

func (h *Handler) GetApiV1OrganizationsOrganizationIDRegionsRegionIDFlavors(w http.ResponseWriter, r *http.Request, organizationID openapi.OrganizationIDParameter, regionID openapi.RegionIDParameter) {
	ctx := r.Context()

	if err := rbac.AllowOrganizationScopeID(ctx, "compute:flavors", identityapi.Read, organizationID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	ctx = principal.NewImpersonateContext(ctx)

	result, err := h.regionClient().Flavors(ctx, organizationID, regionID)
	if err != nil {
		errors.HandleError(w, r, fmt.Errorf("%w: unable to read flavors", err))
		return
	}

	util.WriteJSONResponse(w, r, http.StatusOK, result)
}

func (h *Handler) GetApiV1OrganizationsOrganizationIDRegionsRegionIDImages(w http.ResponseWriter, r *http.Request, organizationID openapi.OrganizationIDParameter, regionID openapi.RegionIDParameter) {
	ctx := r.Context()

	if err := rbac.AllowOrganizationScopeID(ctx, "compute:images", identityapi.Read, organizationID); err != nil {
		errors.HandleError(w, r, err)
		return
	}

	ctx = principal.NewImpersonateContext(ctx)

	result, err := h.regionClient().Images(ctx, organizationID, regionID)
	if err != nil {
		errors.HandleError(w, r, fmt.Errorf("%w: unable to read images", err))
		return
	}

	util.WriteJSONResponse(w, r, http.StatusOK, result)
}
