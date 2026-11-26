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
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	computev1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	computeids "github.com/unikorn-cloud/compute/pkg/ids"
	computeapi "github.com/unikorn-cloud/compute/pkg/openapi"
	"github.com/unikorn-cloud/compute/pkg/server/handler/instance"
	unikornv1core "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	coreconstants "github.com/unikorn-cloud/core/pkg/constants"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	coreerrors "github.com/unikorn-cloud/core/pkg/server/errors"
	"github.com/unikorn-cloud/core/pkg/server/reservedtags"
	"github.com/unikorn-cloud/identity/pkg/middleware/authorization"
	identityapi "github.com/unikorn-cloud/identity/pkg/openapi"
	identitymock "github.com/unikorn-cloud/identity/pkg/openapi/mock"
	"github.com/unikorn-cloud/identity/pkg/principal"
	"github.com/unikorn-cloud/identity/pkg/rbac"
	regionconstants "github.com/unikorn-cloud/region/pkg/constants"
	regionids "github.com/unikorn-cloud/region/pkg/ids"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	reservedTagEndpoint = "k8s:tags"
	reservedTagPrefix   = "k8s.example.org/"
	reservedTagName     = reservedTagPrefix + "owner"

	reservedTagInstanceID = "3a1f6d2e-8c4b-4e7a-9b5d-1c2e3f4a5b6c"
	reservedTagRegionID   = "a73e9c26-af56-4562-8352-9512e0586f3b"
	reservedTagNetworkID  = "5b2c9a8e-7f1d-4e3a-9c6b-2d4e8f1a3b5c"
)

func reservedTagOptions() reservedtags.Options {
	return reservedtags.Options{Prefixes: map[string]string{reservedTagPrefix: reservedTagEndpoint}}
}

// reservedTagContext is a request by a user holding compute:instances for
// create and update in the project, and the reserved tag endpoint for owned.
func reservedTagContext(t *testing.T, owned identityapi.AclOperations) context.Context {
	t.Helper()

	endpoints := identityapi.AclEndpoints{
		{
			Name:       "compute:instances",
			Operations: identityapi.AclOperations{identityapi.Create, identityapi.Read, identityapi.Update},
		},
	}

	if len(owned) > 0 {
		endpoints = append(endpoints, identityapi.AclEndpoint{
			Name:       reservedTagEndpoint,
			Operations: owned,
		})
	}

	acl := &identityapi.Acl{
		Organizations: &identityapi.AclOrganizationList{
			{
				Id: organizationID,
				Projects: &identityapi.AclProjectList{
					{Id: projectID, Endpoints: endpoints},
				},
			},
		},
	}

	ctx := rbac.NewContext(t.Context(), acl)
	ctx = authorization.NewContext(ctx, &authorization.Info{Userinfo: &identityapi.Userinfo{Sub: "user"}})

	return principal.NewContext(ctx, &principal.Principal{Actor: "user"})
}

func reservedTagKube(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, computev1.AddToScheme(scheme))

	mapper := meta.NewDefaultRESTMapper(nil)
	mapper.Add(computev1.SchemeGroupVersion.WithKind("ComputeInstance"), meta.RESTScopeNamespace)

	return fake.NewClientBuilder().WithScheme(scheme).WithRESTMapper(mapper).WithObjects(objects...).Build()
}

// reservedTagRegion serves the network, flavors and images a create reads.
func reservedTagRegion(t *testing.T) regionapi.ClientWithResponsesInterface {
	t.Helper()

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
		var body []byte

		switch {
		case strings.HasSuffix(r.URL.Path, "/networks/"+reservedTagNetworkID):
			body = []byte(`{"metadata":{"id":"` + reservedTagNetworkID + `","organizationId":"` + organizationID + `","projectId":"` + projectID + `"},"status":{"regionId":"` + reservedTagRegionID + `"}}`)
		case strings.HasSuffix(r.URL.Path, "/flavors"):
			body = mustJSON(t, flavors)
		case strings.HasSuffix(r.URL.Path, "/images"):
			body = mustJSON(t, images)
		default:
			t.Errorf("unexpected region request %s %s", r.Method, r.URL.Path)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    r,
		}, nil
	})))
	require.NoError(t, err)

	return regionClient
}

func reservedTagCreateRequest() *computeapi.InstanceCreate {
	request := minimalInstanceCreateRequest(projectID)
	request.Metadata.Tags = &coreapi.TagList{
		{Name: reservedTagName, Value: "a"},
		{Name: "team", Value: "x"},
	}
	request.Spec.NetworkId = regionids.NetworkID(uuid.MustParse(reservedTagNetworkID))
	request.Spec.FlavorId = regionids.FlavorID(uuid.MustParse(instanceFlavorID))
	request.Spec.ImageId = regionids.ImageID(uuid.MustParse(instanceImageID))

	return request
}

// A create carrying a reserved tag is refused unless the caller owns its
// prefix, before anything is allocated or stored.
func TestCreateReservedTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		options   reservedtags.Options
		owned     identityapi.AclOperations
		forbidden bool
	}{
		{name: "non-owner is refused", options: reservedTagOptions(), forbidden: true},
		{name: "update-only holder is refused", options: reservedTagOptions(), owned: identityapi.AclOperations{identityapi.Update}, forbidden: true},
		{name: "owner stores tags", options: reservedTagOptions(), owned: identityapi.AclOperations{identityapi.Create}},
		{name: "nothing configured stores tags", options: reservedtags.Options{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			kube := reservedTagKube(t)

			ctrl := gomock.NewController(t)
			identity := identitymock.NewMockClientWithResponsesInterface(ctrl)

			if !tc.forbidden {
				identity.EXPECT().
					PostApiV1OrganizationsOrganizationIDProjectsProjectIDAllocationsWithResponse(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&identityapi.PostApiV1OrganizationsOrganizationIDProjectsProjectIDAllocationsResponse{
						HTTPResponse: &http.Response{StatusCode: http.StatusCreated},
						JSON201:      &identityapi.AllocationResponse{Metadata: coreapi.ProjectScopedResourceReadMetadata{Id: "8e7d6c5b-4a39-4281-9f0e-1d2c3b4a5968"}},
					}, nil)
			}

			c := instance.NewClient(kube, "test", identity, reservedTagRegion(t), tc.options)

			_, err := c.Create(reservedTagContext(t, tc.owned), reservedTagCreateRequest())

			instances := &computev1.ComputeInstanceList{}
			require.NoError(t, kube.List(t.Context(), instances))

			if tc.forbidden {
				require.True(t, coreerrors.IsForbidden(err), "expected 403, got: %v", err)
				require.ErrorContains(t, err, reservedTagName)
				require.Empty(t, instances.Items)

				return
			}

			require.NoError(t, err)
			require.Len(t, instances.Items, 1)
			require.Equal(t, unikornv1core.TagList{
				{Name: reservedTagName, Value: "a"},
				{Name: "team", Value: "x"},
			}, instances.Items[0].Spec.Tags)
		})
	}
}

// An update keeps the stored tags under a prefix the caller does not own,
// whatever the request says, and applies everything else.
func TestUpdateReservedTags(t *testing.T) {
	t.Parallel()

	stored := unikornv1core.TagList{
		{Name: reservedTagName, Value: "a"},
		{Name: "team", Value: "x"},
	}

	tests := []struct {
		name     string
		options  reservedtags.Options
		owned    identityapi.AclOperations
		tags     string
		expected unikornv1core.TagList
	}{
		{
			name:     "non-owner omits",
			options:  reservedTagOptions(),
			tags:     `[{"name":"team","value":"y"}]`,
			expected: unikornv1core.TagList{{Name: "team", Value: "y"}, {Name: reservedTagName, Value: "a"}},
		},
		{
			name:     "non-owner changes",
			options:  reservedTagOptions(),
			tags:     `[{"name":"` + reservedTagName + `","value":"b"},{"name":"team","value":"y"}]`,
			expected: unikornv1core.TagList{{Name: "team", Value: "y"}, {Name: reservedTagName, Value: "a"}},
		},
		{
			name:     "create-only holder omits, stored kept",
			options:  reservedTagOptions(),
			owned:    identityapi.AclOperations{identityapi.Create},
			tags:     `[{"name":"team","value":"y"}]`,
			expected: unikornv1core.TagList{{Name: "team", Value: "y"}, {Name: reservedTagName, Value: "a"}},
		},
		{
			name:     "owner omits",
			options:  reservedTagOptions(),
			owned:    identityapi.AclOperations{identityapi.Update},
			tags:     `[{"name":"team","value":"y"}]`,
			expected: unikornv1core.TagList{{Name: "team", Value: "y"}},
		},
		{
			name:     "nothing configured",
			options:  reservedtags.Options{},
			tags:     `[{"name":"team","value":"y"}]`,
			expected: unikornv1core.TagList{{Name: "team", Value: "y"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			current := &computev1.ComputeInstance{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test",
					Name:      reservedTagInstanceID,
					Labels: map[string]string{
						coreconstants.NameLabel:         "test-instance",
						coreconstants.OrganizationLabel: organizationID,
						coreconstants.ProjectLabel:      projectID,
						regionconstants.RegionLabel:     reservedTagRegionID,
						regionconstants.NetworkLabel:    reservedTagNetworkID,
					},
					Annotations: map[string]string{
						coreconstants.AllocationAnnotation: "8e7d6c5b-4a39-4281-9f0e-1d2c3b4a5968",
						coreconstants.CreatorAnnotation:    "user",
					},
				},
				Spec: computev1.ComputeInstanceSpec{
					Tags: stored,
				},
			}
			current.Spec.FlavorID = instanceFlavorID
			current.Spec.ImageID = instanceImageID

			kube := reservedTagKube(t, current)

			ctrl := gomock.NewController(t)
			identity := identitymock.NewMockClientWithResponsesInterface(ctrl)
			identity.EXPECT().
				PutApiV1OrganizationsOrganizationIDProjectsProjectIDAllocationsAllocationIDWithResponse(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&identityapi.PutApiV1OrganizationsOrganizationIDProjectsProjectIDAllocationsAllocationIDResponse{
					HTTPResponse: &http.Response{StatusCode: http.StatusOK},
				}, nil)

			c := instance.NewClient(kube, "test", identity, newFlavorAndImageRegionClient(t), tc.options)

			body := `{"metadata":{"name":"test-instance","tags":` + tc.tags + `},"spec":{"flavorId":"` + instanceFlavorID + `","imageId":"` + instanceImageID + `"}}`

			request := &computeapi.InstanceUpdate{}
			require.NoError(t, json.Unmarshal([]byte(body), request))

			_, err := c.Update(reservedTagContext(t, tc.owned), computeids.MustParseInstanceID(reservedTagInstanceID), request)
			require.NoError(t, err)

			result := &computev1.ComputeInstance{}
			require.NoError(t, kube.Get(t.Context(), client.ObjectKeyFromObject(current), result))
			require.Equal(t, tc.expected, result.Spec.Tags)
		})
	}
}
