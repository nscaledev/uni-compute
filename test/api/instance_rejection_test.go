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

package api_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unikorn-cloud/compute/pkg/openapi"
	computeapi "github.com/unikorn-cloud/compute/test/api"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	coreconfig "github.com/unikorn-cloud/core/pkg/testing/config"
)

// computeServer answers every instance create with createStatus and createBody, and
// records the paths of the DELETEs it receives. A zero deleteStatus answers 202.
type computeServer struct {
	createStatus int
	createBody   string
	deleteStatus int

	mu      sync.Mutex
	deletes []string
}

func (s *computeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		w.WriteHeader(s.createStatus)
		_, _ = w.Write([]byte(s.createBody))
	case http.MethodDelete:
		s.mu.Lock()
		s.deletes = append(s.deletes, r.URL.Path)
		s.mu.Unlock()

		status := s.deleteStatus
		if status == 0 {
			status = http.StatusAccepted
		}

		w.WriteHeader(status)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func clientFor(t *testing.T, s *computeServer) *computeapi.APIClient {
	t.Helper()

	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)

	return computeapi.NewAPIClientWithConfig(&computeapi.TestConfig{
		BaseConfig: coreconfig.BaseConfig{BaseURL: srv.URL, RequestTimeout: 5 * time.Second},
	})
}

func maliciousCreate() openapi.InstanceCreate {
	return openapi.InstanceCreate{Metadata: coreapi.ResourceWriteMetadata{Name: "<script>alert('XSS')</script>"}}
}

func TestAcceptedCreateIsDeleted(t *testing.T) {
	t.Parallel()

	s := &computeServer{createStatus: http.StatusCreated, createBody: `{"metadata":{"id":"leaked-1"}}`}

	err := clientFor(t, s).CreateInstanceExpectRejected(t.Context(), maliciousCreate())

	require.ErrorIs(t, err, computeapi.ErrCreateAccepted)
	assert.Equal(t, []string{"/api/v2/instances/leaked-1"}, s.deletes)
}

func TestAcceptedCreateWithoutAnIDIsReportedByName(t *testing.T) {
	t.Parallel()

	s := &computeServer{createStatus: http.StatusAccepted, createBody: "<html>accepted</html>"}

	err := clientFor(t, s).CreateInstanceExpectRejected(t.Context(), maliciousCreate())

	require.ErrorIs(t, err, computeapi.ErrCreateAccepted)
	require.ErrorContains(t, err, maliciousCreate().Metadata.Name)
	assert.Empty(t, s.deletes)
}

func TestFailedDeleteNamesTheInstance(t *testing.T) {
	t.Parallel()

	s := &computeServer{createStatus: http.StatusCreated, createBody: `{"metadata":{"id":"leaked-2"}}`, deleteStatus: http.StatusInternalServerError}

	err := clientFor(t, s).CreateInstanceExpectRejected(t.Context(), maliciousCreate())

	require.ErrorIs(t, err, computeapi.ErrCreateAccepted)
	require.ErrorContains(t, err, "instance leaked-2 not deleted")
}

func TestRejectedCreateIsNotAccepted(t *testing.T) {
	t.Parallel()

	s := &computeServer{createStatus: http.StatusBadRequest, createBody: `{"error":"invalid_request"}`}

	err := clientFor(t, s).CreateInstanceExpectRejected(t.Context(), maliciousCreate())

	require.Error(t, err)
	require.NotErrorIs(t, err, computeapi.ErrCreateAccepted)
	require.ErrorContains(t, err, "400")
	require.ErrorContains(t, err, "invalid_request")
	assert.Empty(t, s.deletes)
}

func TestDeleteNotFoundIsNotReportedAsDeleted(t *testing.T) {
	t.Parallel()

	s := &computeServer{createStatus: http.StatusCreated, createBody: `{"metadata":{"id":"leaked-3"}}`, deleteStatus: http.StatusNotFound}

	err := clientFor(t, s).CreateInstanceExpectRejected(t.Context(), maliciousCreate())

	require.ErrorIs(t, err, computeapi.ErrCreateAccepted)
	require.ErrorContains(t, err, "instance leaked-3 not deleted")
}

func TestAcceptedWithoutCreatedIsAlsoDeleted(t *testing.T) {
	t.Parallel()

	s := &computeServer{createStatus: http.StatusAccepted, createBody: `{"metadata":{"id":"leaked-4"}}`}

	err := clientFor(t, s).CreateInstanceExpectRejected(t.Context(), maliciousCreate())

	require.ErrorIs(t, err, computeapi.ErrCreateAccepted)
	assert.Equal(t, []string{"/api/v2/instances/leaked-4"}, s.deletes)
}

func TestAcceptedJSONWithoutAnIDSendsNoDelete(t *testing.T) {
	t.Parallel()

	s := &computeServer{createStatus: http.StatusCreated, createBody: `{"metadata":{}}`}

	err := clientFor(t, s).CreateInstanceExpectRejected(t.Context(), maliciousCreate())

	require.ErrorIs(t, err, computeapi.ErrCreateAccepted)
	require.ErrorContains(t, err, maliciousCreate().Metadata.Name)
	assert.Empty(t, s.deletes)
}
