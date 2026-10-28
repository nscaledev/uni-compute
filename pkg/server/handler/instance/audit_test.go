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
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	computev1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	computeids "github.com/unikorn-cloud/compute/pkg/ids"
	computeapi "github.com/unikorn-cloud/compute/pkg/openapi"
	"github.com/unikorn-cloud/compute/pkg/server/handler/instance"
	coreconstants "github.com/unikorn-cloud/core/pkg/constants"
	coreerrors "github.com/unikorn-cloud/core/pkg/server/errors"
	"github.com/unikorn-cloud/identity/pkg/authz"
	identityapi "github.com/unikorn-cloud/identity/pkg/openapi"
	"github.com/unikorn-cloud/identity/pkg/rbac"
	regionconstants "github.com/unikorn-cloud/region/pkg/constants"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	auditNamespace = "audit"
	auditName      = "audited-instance"
	auditInstance  = "0b8f3f8e-6a3e-4c55-9d43-6b2f1a2c9e10"
	auditRegion    = "5d1f6c8e-2b7a-4f0e-9c3d-8a1b2c3d4e5f"
	auditNetwork   = "7e2a9b4c-3d5f-4a6b-8c7d-9e0f1a2b3c4d"
	auditServer    = "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"
)

func instanceACL(operations ...identityapi.AclOperation) *identityapi.Acl {
	return &identityapi.Acl{
		Organizations: &identityapi.AclOrganizationList{
			{
				Id: organizationID,
				Projects: &identityapi.AclProjectList{
					{
						Id: projectID,
						Endpoints: identityapi.AclEndpoints{
							{
								Name:       "compute:instances",
								Operations: operations,
							},
						},
					},
				},
			},
		},
	}
}

// fakeRegion answers the server lookup and every server action an instance
// operation makes, and counts every call so a test can see none were made.
// Anything else is not found, which fails create and update after they have
// authorized.
func fakeRegion(t *testing.T, calls *int) regionapi.ClientWithResponsesInterface {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++

		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/servers":
			_, _ = w.Write([]byte(`[{"metadata":{"id":"` + auditServer + `"}}]`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/snapshot"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v2/servers/"):
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	client, err := regionapi.NewClientWithResponses(server.URL)
	require.NoError(t, err)

	return client
}

func auditedInstance(deleting bool) *computev1.ComputeInstance {
	resource := &computev1.ComputeInstance{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: auditNamespace,
			Name:      auditInstance,
			Labels: map[string]string{
				coreconstants.OrganizationLabel: organizationID,
				coreconstants.ProjectLabel:      projectID,
				coreconstants.NameLabel:         auditName,
				regionconstants.RegionLabel:     auditRegion,
				regionconstants.NetworkLabel:    auditNetwork,
			},
		},
	}

	if deleting {
		resource.DeletionTimestamp = ptr.To(metav1.Now())
		resource.Finalizers = []string{"test"}
	}

	return resource
}

func auditClient(t *testing.T, deleting bool, calls *int) *instance.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, computev1.AddToScheme(scheme))

	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(auditedInstance(deleting)).Build()

	return instance.NewClient(kube, auditNamespace, nil, fakeRegion(t, calls))
}

type mutation struct {
	name   string
	action string
	call   func(ctx context.Context, c *instance.Client, id computeids.InstanceID) error
}

// mutations is every instance endpoint that changes something, with the action
// its audit record must carry.
//
//nolint:gochecknoglobals
var mutations = []mutation{
	{"delete", "delete", func(ctx context.Context, c *instance.Client, id computeids.InstanceID) error {
		return c.Delete(ctx, id)
	}},
	{"start", "start", func(ctx context.Context, c *instance.Client, id computeids.InstanceID) error {
		return c.Start(ctx, id)
	}},
	{"stop", "stop", func(ctx context.Context, c *instance.Client, id computeids.InstanceID) error {
		return c.Stop(ctx, id)
	}},
	{"soft reboot", "reboot", func(ctx context.Context, c *instance.Client, id computeids.InstanceID) error {
		return c.Reboot(ctx, id, computeapi.PostApiV2InstancesInstanceIDRebootParams{})
	}},
	{"hard reboot", "reboot", func(ctx context.Context, c *instance.Client, id computeids.InstanceID) error {
		return c.Reboot(ctx, id, computeapi.PostApiV2InstancesInstanceIDRebootParams{Hard: ptr.To(true)})
	}},
	{"snapshot", "snapshot", func(ctx context.Context, c *instance.Client, id computeids.InstanceID) error {
		_, err := c.Snapshot(ctx, id, computeapi.InstanceSnapshotCreate{})
		return err
	}},
}

// Each mutation is the request's own audited operation, naming the instance,
// its scope and what was done, rather than leaving the record to be guessed.
func TestMutationsRecordTheirOperation(t *testing.T) {
	t.Parallel()

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()

			var calls int

			c := auditClient(t, false, &calls)

			recorder := &authz.Recorder{}
			ctx := authz.NewContext(rbac.NewContext(t.Context(), instanceACL(identityapi.Read, identityapi.Update, identityapi.Delete)), recorder)

			id := computeids.InstanceID(uuid.MustParse(auditInstance))

			require.NoError(t, m.call(ctx, c, id))

			primaries := authz.Primaries(recorder.Decisions())
			require.Len(t, primaries, 1)

			record := primaries[0]
			require.Equal(t, "compute:instances", record.Endpoint)
			require.Equal(t, m.action, record.Operation.Action)
			require.Equal(t, uuid.MustParse(auditInstance), record.ObjectID)
			require.Equal(t, auditName, record.ObjectName)
			require.Equal(t, organizationID, record.Scope.OrganizationID.String())
			require.Equal(t, projectID, record.Scope.ProjectID.String())
		})
	}
}

// Reading an instance is no licence to change it.  Read-only callers are
// refused before compute acts, so region is never asked to do anything, and
// the refusal is recorded as what they attempted.
func TestMutationsRefuseReadOnlyCallers(t *testing.T) {
	t.Parallel()

	update := mutation{"update", "update", func(ctx context.Context, c *instance.Client, id computeids.InstanceID) error {
		_, err := c.Update(ctx, id, &computeapi.InstanceUpdate{})
		return err
	}}

	for _, m := range append([]mutation{update}, mutations...) {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()

			var calls int

			c := auditClient(t, false, &calls)

			recorder := &authz.Recorder{}
			ctx := authz.NewContext(rbac.NewContext(t.Context(), instanceACL(identityapi.Read)), recorder)

			err := m.call(ctx, c, computeids.InstanceID(uuid.MustParse(auditInstance)))
			require.Error(t, err)
			require.True(t, coreerrors.IsForbidden(err), "expected forbidden, got: %v", err)
			require.Zero(t, calls)

			request, state := recorder.Request()
			require.Equal(t, authz.Checked, state)
			require.False(t, request.Allowed)
			require.Equal(t, m.action, request.Operation.Action)
			require.Equal(t, uuid.MustParse(auditInstance), request.ObjectID)
			require.Equal(t, auditName, request.ObjectName)
			require.Equal(t, projectID, request.Scope.ProjectID.String())
			require.Len(t, recorder.Decisions(), 1)
		})
	}
}

// Deleting an instance already being deleted is still a delete: the caller
// must hold delete, and the request is recorded.
func TestDeleteWhileDeletingIsAuthorized(t *testing.T) {
	t.Parallel()

	id := computeids.InstanceID(uuid.MustParse(auditInstance))

	t.Run("read only", func(t *testing.T) {
		t.Parallel()

		var calls int

		c := auditClient(t, true, &calls)
		ctx := rbac.NewContext(t.Context(), instanceACL(identityapi.Read))

		require.True(t, coreerrors.IsForbidden(c.Delete(ctx, id)))
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		var calls int

		c := auditClient(t, true, &calls)

		recorder := &authz.Recorder{}
		ctx := authz.NewContext(rbac.NewContext(t.Context(), instanceACL(identityapi.Delete)), recorder)

		require.NoError(t, c.Delete(ctx, id))
		require.Len(t, authz.Primaries(recorder.Decisions()), 1)
	})
}

// A missing instance is a 404 whatever the caller holds, so an instance's
// existence is not hidden behind a 403, as before.  The attempt is still
// recorded, naming the instance asked for, with no scope as none was found.
func TestMissingInstanceIsNotFound(t *testing.T) {
	t.Parallel()

	var calls int

	c := auditClient(t, false, &calls)

	recorder := &authz.Recorder{}
	ctx := authz.NewContext(rbac.NewContext(t.Context(), &identityapi.Acl{}), recorder)

	id := uuid.New()

	require.True(t, coreerrors.IsHTTPNotFound(c.Delete(ctx, computeids.InstanceID(id))))

	request, state := recorder.Request()
	require.Equal(t, authz.Declared, state)
	require.Equal(t, "delete", request.Operation.Action)
	require.Equal(t, id, request.ObjectID)
	require.False(t, request.Scope.Organization())
}

// Create and update describe themselves before doing any work, so the record
// exists whatever happens after: here region knows of nothing, and both fail.
// A create names no object, as the instance does not exist when authorized.
func TestCreateAndUpdateRecordTheirOperation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		object uuid.UUID
		call   func(ctx context.Context, c *instance.Client) error
	}{
		{"create", uuid.Nil, func(ctx context.Context, c *instance.Client) error {
			_, err := c.Create(ctx, minimalInstanceCreateRequest(organizationID, projectID))
			return err
		}},
		{"update", uuid.MustParse(auditInstance), func(ctx context.Context, c *instance.Client) error {
			request := &computeapi.InstanceUpdate{}
			request.Metadata.Name = auditName

			_, err := c.Update(ctx, computeids.InstanceID(uuid.MustParse(auditInstance)), request)

			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var calls int

			c := auditClient(t, false, &calls)

			recorder := &authz.Recorder{}
			ctx := authz.NewContext(rbac.NewContext(t.Context(), instanceACL(identityapi.Create, identityapi.Update)), recorder)

			require.Error(t, test.call(ctx, c))

			primaries := authz.Primaries(recorder.Decisions())
			require.Len(t, primaries, 1)

			record := primaries[0]
			require.Equal(t, "compute:instances", record.Endpoint)
			require.Equal(t, test.name, record.Operation.Action)
			require.Equal(t, test.object, record.ObjectID)
			require.Equal(t, organizationID, record.Scope.OrganizationID.String())
			require.Equal(t, projectID, record.Scope.ProjectID.String())
		})
	}
}
