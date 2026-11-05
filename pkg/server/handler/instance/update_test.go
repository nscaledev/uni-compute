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

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	computev1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	computeids "github.com/unikorn-cloud/compute/pkg/ids"
	computeapi "github.com/unikorn-cloud/compute/pkg/openapi"
	"github.com/unikorn-cloud/compute/pkg/server/handler/instance"
	coreconstants "github.com/unikorn-cloud/core/pkg/constants"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	"github.com/unikorn-cloud/identity/pkg/middleware/authorization"
	identityapi "github.com/unikorn-cloud/identity/pkg/openapi"
	identitymock "github.com/unikorn-cloud/identity/pkg/openapi/mock"
	"github.com/unikorn-cloud/identity/pkg/principal"
	"github.com/unikorn-cloud/identity/pkg/rbac"
	regionconstants "github.com/unikorn-cloud/region/pkg/constants"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestUpdateUserData drives an instance PUT through Update and checks the
// stored ComputeInstance. The stored payload is a part-handler, which the
// current rules reject, so keeping it also shows it was not re-validated.
func TestUpdateUserData(t *testing.T) {
	t.Parallel()

	const (
		instanceID   = "0f0c7d3e-5d3c-4b8e-9a4e-6b1f2c3d4e5f"
		regionID     = "a73e9c26-af56-4562-8352-9512e0586f3b"
		networkID    = "5b2c9a8e-7f1d-4e3a-9c6b-2d4e8f1a3b5c"
		allocationID = "8e7d6c5b-4a39-4281-9f0e-1d2c3b4a5968"
	)

	stored := []byte("#part-handler\ndef list_types():\n    return []\n")
	replacement := []byte("#cloud-config\nusers: []\n")

	tests := []struct {
		name     string
		userData string
		expected []byte
	}{
		{name: "omitted keeps", expected: stored},
		{name: "null keeps", userData: `,"userData":null`, expected: stored},
		{name: "empty clears", userData: `,"userData":""`, expected: nil},
		{name: "value replaces", userData: `,"userData":"` + string(mustJSONBytes(t, replacement)) + `"`, expected: replacement},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()
			require.NoError(t, computev1.AddToScheme(scheme))

			current := &computev1.ComputeInstance{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test",
					Name:      instanceID,
					Labels: map[string]string{
						coreconstants.NameLabel:         "test-instance",
						coreconstants.OrganizationLabel: organizationID,
						coreconstants.ProjectLabel:      projectID,
						regionconstants.RegionLabel:     regionID,
						regionconstants.NetworkLabel:    networkID,
					},
					Annotations: map[string]string{
						coreconstants.AllocationAnnotation: allocationID,
						coreconstants.CreatorAnnotation:    "user",
					},
				},
				Spec: computev1.ComputeInstanceSpec{
					UserData: stored,
				},
			}
			current.Spec.FlavorID = instanceFlavorID
			current.Spec.ImageID = instanceImageID

			mapper := meta.NewDefaultRESTMapper(nil)
			mapper.Add(computev1.SchemeGroupVersion.WithKind("ComputeInstance"), meta.RESTScopeNamespace)

			kube := fake.NewClientBuilder().WithScheme(scheme).WithRESTMapper(mapper).WithObjects(current).Build()

			ctrl := gomock.NewController(t)
			identity := identitymock.NewMockClientWithResponsesInterface(ctrl)
			identity.EXPECT().
				PutApiV1OrganizationsOrganizationIDProjectsProjectIDAllocationsAllocationIDWithResponse(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&identityapi.PutApiV1OrganizationsOrganizationIDProjectsProjectIDAllocationsAllocationIDResponse{
					HTTPResponse: &http.Response{StatusCode: http.StatusOK},
				}, nil)

			c := instance.NewClient(kube, "test", identity, newFlavorAndImageRegionClient(t))

			body := `{"metadata":{"name":"test-instance"},"spec":{"flavorId":"` + instanceFlavorID + `","imageId":"` + instanceImageID + `"` + tc.userData + `}}`

			request := &computeapi.InstanceUpdate{}
			require.NoError(t, json.Unmarshal([]byte(body), request))

			_, err := c.Update(updateContext(t), computeids.MustParseInstanceID(instanceID), request)
			require.NoError(t, err)

			result := &computev1.ComputeInstance{}
			require.NoError(t, kube.Get(t.Context(), client.ObjectKeyFromObject(current), result))
			require.Equal(t, tc.expected, result.Spec.UserData)
		})
	}
}

func updateContext(t *testing.T) context.Context {
	t.Helper()

	acl := &identityapi.Acl{
		Organizations: &identityapi.AclOrganizationList{
			{
				Id: organizationID,
				Endpoints: &identityapi.AclEndpoints{
					{
						Name:       "compute:instances",
						Operations: identityapi.AclOperations{identityapi.Read, identityapi.Update},
					},
				},
			},
		},
	}

	ctx := rbac.NewContext(t.Context(), acl)
	ctx = authorization.NewContext(ctx, &authorization.Info{Userinfo: &identityapi.Userinfo{Sub: "user"}})

	return principal.NewContext(ctx, &principal.Principal{Actor: "user"})
}

// newFlavorAndImageRegionClient serves the flavor and image lists the update
// path reads, with one compatible flavor and image.
func newFlavorAndImageRegionClient(t *testing.T) regionapi.ClientWithResponsesInterface {
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

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()

	out, err := json.Marshal(v)
	require.NoError(t, err)

	return out
}

// mustJSONBytes returns the base64 form JSON uses for a byte slice, unquoted.
func mustJSONBytes(t *testing.T, v []byte) []byte {
	t.Helper()

	out := mustJSON(t, v)

	return out[1 : len(out)-1]
}
