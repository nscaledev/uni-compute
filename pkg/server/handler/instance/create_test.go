/*
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

package instance_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	computev1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	computeapi "github.com/unikorn-cloud/compute/pkg/openapi"
	"github.com/unikorn-cloud/compute/pkg/server/handler/instance"
	coreconstants "github.com/unikorn-cloud/core/pkg/constants"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	coreerrors "github.com/unikorn-cloud/core/pkg/server/errors"
	identityids "github.com/unikorn-cloud/identity/pkg/ids"
	"github.com/unikorn-cloud/identity/pkg/middleware/authorization"
	identityapi "github.com/unikorn-cloud/identity/pkg/openapi"
	identitymock "github.com/unikorn-cloud/identity/pkg/openapi/mock"
	"github.com/unikorn-cloud/identity/pkg/principal"
	"github.com/unikorn-cloud/identity/pkg/rbac"
	regionconstants "github.com/unikorn-cloud/region/pkg/constants"
	idstest "github.com/unikorn-cloud/region/pkg/ids/idstest"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// otherOrganizationID, the ID of an organization other than the instance's,
// comes from snapshot_test.go.
const (
	createNamespace    = "test"
	createRegionID     = "a73e9c26-af56-4562-8352-9512e0586f3b"
	createNetworkID    = "5b2c9a8e-7f1d-4e3a-9c6b-2d4e8f1a3b5c"
	createAllocationID = "8e7d6c5b-4a39-4281-9f0e-1d2c3b4a5968"
	otherProjectID     = "6f4e2d0c-8b6a-4f28-9e1c-3b5d7f9a1c2e"
)

func createContext(t *testing.T, organizationIDs []string) context.Context {
	t.Helper()

	ctx := rbac.NewContext(t.Context(), instanceACL(identityapi.Create))
	ctx = authorization.NewContext(ctx, &authorization.Info{Userinfo: &identityapi.Userinfo{Sub: "user"}})

	return principal.NewContext(ctx, &principal.Principal{Actor: "user", OrganizationIDs: organizationIDs})
}

// callerCanSee follows the rule that identity applies to an impersonated read.
// The ACL covers every organization of the caller, and the injected
// organization counts only when that list is empty.
func callerCanSee(ctx context.Context, organizationID string) bool {
	p, err := principal.FromContext(ctx)
	if err != nil || !principal.ImpersonateFromContext(ctx) {
		return false
	}

	organizationIDs := p.OrganizationIDs
	if len(organizationIDs) == 0 && p.OrganizationID != "" {
		organizationIDs = []string{p.OrganizationID}
	}

	return slices.Contains(organizationIDs, organizationID)
}

// newCreateRegionClient serves a network in the given scope and the flavor and
// image the request references. It refuses the network read unless the call is
// impersonated and the caller can see the organization of the network, because
// region authorizes that read against the caller's access. It counts every
// request that is not a read.
func newCreateRegionClient(t *testing.T, networkOrganizationID, networkProjectID, networkRegionID string, writes *int) regionapi.ClientWithResponsesInterface {
	t.Helper()

	network := regionapi.NetworkV2Read{
		Metadata: coreapi.ProjectScopedResourceReadMetadata{
			Id:             createNetworkID,
			OrganizationId: networkOrganizationID,
			ProjectId:      networkProjectID,
		},
		Status: regionapi.NetworkV2Status{RegionId: networkRegionID},
	}
	flavors := []regionapi.Flavor{{
		Metadata: coreapi.StaticResourceMetadata{Id: instanceFlavorID},
		Spec:     regionapi.FlavorSpec{Architecture: regionapi.ArchitectureX8664, Disk: 20},
	}}
	images := []regionapi.Image{{
		Metadata: coreapi.StaticResourceMetadata{Id: instanceImageID},
		Spec: regionapi.ImageSpec{
			Architecture:   regionapi.ArchitectureX8664,
			SizeGiB:        10,
			Virtualization: regionapi.ImageVirtualizationVirtualized,
		},
		Status: regionapi.ImageStatus{State: regionapi.ImageStateReady},
	}}

	regionClient, err := regionapi.NewClientWithResponses("http://region.example", regionapi.WithHTTPClient(regionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			*writes++
		}

		var body []byte

		status := http.StatusOK

		switch {
		case r.URL.Path == "/api/v2/networks/"+createNetworkID:
			if !callerCanSee(r.Context(), networkOrganizationID) {
				status = http.StatusForbidden
			}

			body = mustJSON(t, network)
		case strings.HasSuffix(r.URL.Path, "/flavors"):
			body = mustJSON(t, flavors)
		case strings.HasSuffix(r.URL.Path, "/images"):
			body = mustJSON(t, images)
		default:
			t.Errorf("unexpected region request %s %s", r.Method, r.URL.Path)
		}

		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    r,
		}, nil
	})))
	require.NoError(t, err)

	return regionClient
}

// TestCreateRequiresNetworkInInstanceProject checks that an instance uses a
// network from its own organization and project, even when the caller can read
// a network elsewhere. The first three cases differ in one dimension only, so a
// check on the other dimension cannot hide a missing one. The cross-organization
// case has a caller in both organizations, as region refuses the read for any
// other caller. The last case has a bad region as well. Only a scope check that
// runs first returns the 422 that names the network.
func TestCreateRequiresNetworkInInstanceProject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                  string
		networkOrganizationID string
		networkProjectID      string
		networkRegionID       string
		callerOrganizationIDs []string
		created               bool
	}{
		{name: "network in the instance project", networkOrganizationID: organizationID, networkProjectID: projectID, networkRegionID: createRegionID, created: true},
		{name: "network in another project of the same organization", networkOrganizationID: organizationID, networkProjectID: otherProjectID, networkRegionID: createRegionID},
		{name: "network in another organization of a caller in both", networkOrganizationID: otherOrganizationID, networkProjectID: projectID, networkRegionID: createRegionID, callerOrganizationIDs: []string{organizationID, otherOrganizationID}},
		{name: "network scope is checked before the region is parsed", networkOrganizationID: organizationID, networkProjectID: otherProjectID, networkRegionID: "not-a-uuid"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()
			require.NoError(t, computev1.AddToScheme(scheme))

			mapper := meta.NewDefaultRESTMapper(nil)
			mapper.Add(computev1.SchemeGroupVersion.WithKind("ComputeInstance"), meta.RESTScopeNamespace)

			kube := fake.NewClientBuilder().WithScheme(scheme).WithRESTMapper(mapper).Build()

			identity := identitymock.NewMockClientWithResponsesInterface(gomock.NewController(t))

			var allocationCalls int

			identity.EXPECT().
				PostApiV1OrganizationsOrganizationIDProjectsProjectIDAllocationsWithResponse(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(func(context.Context, identityids.OrganizationID, identityids.ProjectID, identityapi.AllocationWrite, ...identityapi.RequestEditorFn) (*identityapi.PostApiV1OrganizationsOrganizationIDProjectsProjectIDAllocationsResponse, error) {
					allocationCalls++

					return &identityapi.PostApiV1OrganizationsOrganizationIDProjectsProjectIDAllocationsResponse{
						HTTPResponse: &http.Response{StatusCode: http.StatusCreated},
						JSON201:      &identityapi.AllocationResponse{Metadata: coreapi.ProjectScopedResourceReadMetadata{Id: createAllocationID}},
					}, nil
				}).
				AnyTimes()

			var regionWrites int

			c := instance.NewClient(kube, createNamespace, identity, newCreateRegionClient(t, tc.networkOrganizationID, tc.networkProjectID, tc.networkRegionID, &regionWrites))

			request := &computeapi.InstanceCreate{
				Metadata: coreapi.ResourceWriteMetadata{Name: "test-instance"},
				Spec: computeapi.InstanceCreateSpec{
					OrganizationId: identityids.MustParseOrganizationID(organizationID),
					ProjectId:      identityids.MustParseProjectID(projectID),
					NetworkId:      idstest.MustParseNetworkID(createNetworkID),
					FlavorId:       idstest.MustParseFlavorID(instanceFlavorID),
					ImageId:        idstest.MustParseImageID(instanceImageID),
				},
			}

			read, err := c.Create(createContext(t, tc.callerOrganizationIDs), request)

			stored := &computev1.ComputeInstanceList{}
			require.NoError(t, kube.List(t.Context(), stored))

			if !tc.created {
				require.True(t, coreerrors.IsUnprocessableContent(err), "expected 422, got: %v", err)
				require.ErrorContains(t, err, "networkId")
				require.Nil(t, read)
				require.Zero(t, allocationCalls, "a refused create must not allocate quota")
				require.Zero(t, regionWrites, "a refused create must not write to region")
				require.Empty(t, stored.Items, "no instance exists after a refused create")

				return
			}

			require.NoError(t, err)
			require.Equal(t, 1, allocationCalls)
			require.Zero(t, regionWrites, "create must not write to region")
			require.Equal(t, organizationID, read.Metadata.OrganizationId)
			require.Equal(t, projectID, read.Metadata.ProjectId)
			require.Equal(t, createNetworkID, read.Status.NetworkId)
			require.Equal(t, createRegionID, read.Status.RegionId)

			require.Len(t, stored.Items, 1)
			require.Equal(t, createNetworkID, stored.Items[0].Labels[regionconstants.NetworkLabel])
			require.Equal(t, projectID, stored.Items[0].Labels[coreconstants.ProjectLabel])
		})
	}
}
