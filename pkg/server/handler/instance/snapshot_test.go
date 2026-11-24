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
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	computev1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	computeids "github.com/unikorn-cloud/compute/pkg/ids"
	computeapi "github.com/unikorn-cloud/compute/pkg/openapi"
	"github.com/unikorn-cloud/compute/pkg/server/handler/instance"
	coreconstants "github.com/unikorn-cloud/core/pkg/constants"
	coreerrors "github.com/unikorn-cloud/core/pkg/server/errors"
	"github.com/unikorn-cloud/core/pkg/server/reservedtags"
	identityapi "github.com/unikorn-cloud/identity/pkg/openapi"
	"github.com/unikorn-cloud/identity/pkg/rbac"
	regionconstants "github.com/unikorn-cloud/region/pkg/constants"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	otherOrganizationID = "3f0c8d7a-5b1e-4c92-a6d4-7e8f9a0b1c2d"
	snapshotNamespace   = "snapshot"
	snapshotInstanceID  = "e3a7c1d9-4f28-4b6a-8d05-1c9b7f2e6a40"
	snapshotRegionID    = "6b9d0f3e-8a21-4c57-b4e9-3d7a1c5f8e02"
	snapshotNetworkID   = "c84e1a7b-2f96-4d03-a5b8-9e6c0d3f7a21"
	snapshotServerID    = "5c0e2a64-7d1b-4a38-9f6e-2b8d4c1a7e93"
)

// recordingRegion is a fake Region that remembers each request it receives.
type recordingRegion struct {
	mu       sync.Mutex
	requests []string
}

func (r *recordingRegion) record(request *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.requests = append(r.requests, request.Method+" "+request.URL.Path)
}

func (r *recordingRegion) received() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.requests...)
}

func storedInstance() *computev1.ComputeInstance {
	return &computev1.ComputeInstance{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: snapshotNamespace,
			Name:      snapshotInstanceID,
			Labels: map[string]string{
				coreconstants.OrganizationLabel: organizationID,
				coreconstants.ProjectLabel:      projectID,
				coreconstants.NameLabel:         "snapshot-source",
				regionconstants.RegionLabel:     snapshotRegionID,
				regionconstants.NetworkLabel:    snapshotNetworkID,
			},
		},
	}
}

func snapshotClient(t *testing.T) (*instance.Client, *recordingRegion) {
	t.Helper()

	region := &recordingRegion{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		region.record(r)

		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/servers":
			_, _ = w.Write([]byte(`[{"metadata":{"id":"` + snapshotServerID + `"}}]`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/snapshot"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	regionClient, err := regionapi.NewClientWithResponses(server.URL)
	require.NoError(t, err)

	scheme := runtime.NewScheme()
	require.NoError(t, computev1.AddToScheme(scheme))

	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(storedInstance()).Build()

	return instance.NewClient(kube, snapshotNamespace, nil, regionClient, reservedtags.Options{}), region
}

func grant(name string, operations ...identityapi.AclOperation) identityapi.AclEndpoint {
	return identityapi.AclEndpoint{Name: name, Operations: operations}
}

// snapshotACL describes a caller with the given grants in the instance's project,
// at organization scope and at global scope.
func snapshotACL(project, organization, global identityapi.AclEndpoints) *identityapi.Acl {
	acl := &identityapi.Acl{
		Organizations: &identityapi.AclOrganizationList{
			{
				Id:        organizationID,
				Endpoints: &organization,
				Projects: &identityapi.AclProjectList{
					{Id: projectID, Endpoints: project},
				},
			},
		},
	}

	if global != nil {
		acl.Global = &global
	}

	return acl
}

// The snapshot is an image, and an image belongs to the organization, so the
// caller needs image create at organization scope as well as instance update.
// The successes show that the fake Region records a call when one happens, so
// an empty record for a refusal means that compute made no call.
func TestSnapshotNeedsImageCreateAtOrganizationScope(t *testing.T) {
	t.Parallel()

	instanceUpdate := identityapi.AclEndpoints{grant("compute:instances", identityapi.Update)}

	otherOrganizationImageCreate := snapshotACL(instanceUpdate, nil, nil)
	*otherOrganizationImageCreate.Organizations = append(*otherOrganizationImageCreate.Organizations, identityapi.AclOrganization{
		Id:        otherOrganizationID,
		Endpoints: &identityapi.AclEndpoints{grant("region:images", identityapi.Create)},
	})

	tests := []struct {
		name    string
		acl     *identityapi.Acl
		allowed bool
	}{
		{
			name: "instance update and image create at organization scope",
			acl: snapshotACL(instanceUpdate,
				identityapi.AclEndpoints{grant("region:images", identityapi.Create)}, nil),
			allowed: true,
		},
		{
			name: "instance update and image create at global scope",
			acl: snapshotACL(nil, nil,
				identityapi.AclEndpoints{grant("compute:instances", identityapi.Update), grant("region:images", identityapi.Create)}),
			allowed: true,
		},
		{
			name: "instance update and no image permission",
			acl:  snapshotACL(instanceUpdate, nil, nil),
		},
		{
			name: "instance update and image read at organization scope",
			acl: snapshotACL(instanceUpdate,
				identityapi.AclEndpoints{grant("region:images", identityapi.Read)}, nil),
		},
		{
			name: "instance update and image create at project scope only",
			acl: snapshotACL(
				identityapi.AclEndpoints{grant("compute:instances", identityapi.Update), grant("region:images", identityapi.Create)}, nil, nil),
		},
		{
			name: "instance update and image create in another organization",
			acl:  otherOrganizationImageCreate,
		},
		{
			name: "image create and no instance update",
			acl: snapshotACL(
				identityapi.AclEndpoints{grant("compute:instances", identityapi.Read)},
				identityapi.AclEndpoints{grant("region:images", identityapi.Create)}, nil),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			c, region := snapshotClient(t)

			ctx := rbac.NewContext(t.Context(), test.acl)

			result, err := c.Snapshot(ctx, computeids.InstanceID(uuid.MustParse(snapshotInstanceID)), computeapi.InstanceSnapshotCreate{})

			if test.allowed {
				require.NoError(t, err)
				require.NotNil(t, result)

				var snapshots []string

				for _, request := range region.received() {
					if strings.HasSuffix(request, "/snapshot") {
						snapshots = append(snapshots, request)
					}
				}

				require.Equal(t, []string{"POST /api/v2/servers/" + snapshotServerID + "/snapshot"}, snapshots)

				return
			}

			require.Error(t, err)
			require.True(t, coreerrors.IsForbidden(err), "expected forbidden, got: %v", err)
			require.Empty(t, region.received(), "Region was called for a refused snapshot")
		})
	}
}
