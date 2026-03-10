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
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	computeapi "github.com/unikorn-cloud/compute/pkg/openapi"
	"github.com/unikorn-cloud/compute/pkg/server/handler/instance"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	coreerrors "github.com/unikorn-cloud/core/pkg/server/errors"
	identityapi "github.com/unikorn-cloud/identity/pkg/openapi"
	identitymock "github.com/unikorn-cloud/identity/pkg/openapi/mock"
	"github.com/unikorn-cloud/identity/pkg/rbac"
)

const (
	organizationID = "foo"
	projectID      = "bar"
)

// aclWithOrgScopeCreate grants compute:instances/Create at organization scope,
// so Create must verify the project via the identity API.
func aclWithOrgScopeCreate() *identityapi.Acl {
	return &identityapi.Acl{
		Organizations: &identityapi.AclOrganizationList{
			{
				Id: organizationID,
				Endpoints: &identityapi.AclEndpoints{
					{
						Name:       "compute:instances",
						Operations: identityapi.AclOperations{identityapi.Create},
					},
				},
			},
		},
	}
}

// minimalInstanceCreateRequest returns an InstanceCreate request body with
// the given organization and project IDs.
func minimalInstanceCreateRequest(orgID, projID string) *computeapi.InstanceCreate {
	return &computeapi.InstanceCreate{
		Metadata: coreapi.ResourceWriteMetadata{
			Name: "test-instance",
		},
		Spec: computeapi.InstanceCreateSpec{
			OrganizationId: orgID,
			ProjectId:      projID,
		},
	}
}

// TestInstanceCreateRBACOrgScopedProjectNotFound verifies that Create returns a
// 404 Not Found when the caller has org-scoped ACL but supplies a project ID
// that does not exist.
func TestInstanceCreateRBACOrgScopedProjectNotFound(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	mockIdentity := identitymock.NewMockClientWithResponsesInterface(ctrl)
	mockIdentity.EXPECT().
		GetApiV1OrganizationsOrganizationIDProjectsProjectIDWithResponse(gomock.Any(), organizationID, "nonexistent-project").
		Return(&identityapi.GetApiV1OrganizationsOrganizationIDProjectsProjectIDResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusNotFound},
		}, nil)

	c := instance.NewClient(nil, "", mockIdentity, nil)

	ctx := rbac.NewContext(t.Context(), aclWithOrgScopeCreate())

	_, err := c.Create(ctx, minimalInstanceCreateRequest(organizationID, "nonexistent-project"))

	require.Error(t, err)
	require.True(t, coreerrors.IsHTTPNotFound(err), "expected 404 not found, got: %v", err)
}

// TestInstanceCreateRBACNoPermissions verifies that Create returns a forbidden
// error when the caller has no relevant permissions.
func TestInstanceCreateRBACNoPermissions(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	mockIdentity := identitymock.NewMockClientWithResponsesInterface(ctrl)
	// No EXPECT calls — the identity API must not be contacted.

	c := instance.NewClient(nil, "", mockIdentity, nil)

	ctx := rbac.NewContext(t.Context(), &identityapi.Acl{})

	_, err := c.Create(ctx, minimalInstanceCreateRequest(organizationID, projectID))

	require.Error(t, err)
	require.True(t, coreerrors.IsForbidden(err), "expected forbidden, got: %v", err)
}
