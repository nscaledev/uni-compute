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

package region_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	computeregion "github.com/unikorn-cloud/compute/pkg/server/handler/region"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	identityids "github.com/unikorn-cloud/identity/pkg/ids"
	regionids "github.com/unikorn-cloud/region/pkg/ids"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	"k8s.io/utils/ptr"
)

func TestFlavorsFiltersPinnedOnly(t *testing.T) {
	t.Parallel()

	const (
		organizationID = "d4600d6e-e965-4b44-a808-84fb2fa36702"
		regionID       = "a73e9c26-af56-4562-8352-9512e0586f3b"
	)

	flavors := []regionapi.Flavor{
		flavor("general", nil),
		flavor("pinned", ptr.To(true)),
		flavor("explicitly-unpinned", ptr.To(false)),
	}

	body, err := json.Marshal(flavors)
	require.NoError(t, err)

	client, err := regionapi.NewClientWithResponses("http://region.example", regionapi.WithHTTPClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "/api/v1/organizations/"+organizationID+"/regions/"+regionID+"/flavors", r.URL.Path)

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    r,
		}, nil
	})))
	require.NoError(t, err)

	filtered, err := computeregion.New(client).Flavors(t.Context(), identityids.MustParseOrganizationID(organizationID), regionids.MustParseRegionID(regionID))
	require.NoError(t, err)

	require.Len(t, filtered, 2)
	assert.Equal(t, "general", filtered[0].Metadata.Id)
	assert.Equal(t, "explicitly-unpinned", filtered[1].Metadata.Id)
}

func TestImagesKeepsCatalogFiltered(t *testing.T) {
	t.Parallel()

	const (
		organizationID = "d4600d6e-e965-4b44-a808-84fb2fa36702"
		regionID       = "a73e9c26-af56-4562-8352-9512e0586f3b"
	)

	softwareVersions := regionapi.SoftwareVersions{
		"kubernetes": "v1.33.0",
	}
	images := []regionapi.Image{
		image("general", nil),
		image("software-versioned", &softwareVersions),
	}

	body, err := json.Marshal(images)
	require.NoError(t, err)

	client, err := regionapi.NewClientWithResponses("http://region.example", regionapi.WithHTTPClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "/api/v1/organizations/"+organizationID+"/regions/"+regionID+"/images", r.URL.Path)
		assert.Empty(t, r.URL.RawQuery)

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    r,
		}, nil
	})))
	require.NoError(t, err)

	regionClient := computeregion.New(client)
	catalog, err := regionClient.Images(t.Context(), identityids.MustParseOrganizationID(organizationID), regionids.MustParseRegionID(regionID))
	require.NoError(t, err)
	require.Len(t, catalog, 1)
	assert.Equal(t, "general", catalog[0].Metadata.Id)
}

func TestAvailableImagesUsesV1AndFiltersNotReady(t *testing.T) {
	t.Parallel()

	const (
		organizationID = "d4600d6e-e965-4b44-a808-84fb2fa36702"
		regionID       = "a73e9c26-af56-4562-8352-9512e0586f3b"
	)

	softwareVersions := regionapi.SoftwareVersions{
		"kubernetes": "v1.33.0",
	}
	notReady := image("not-ready", nil)
	notReady.Status.State = regionapi.ImageStateCreating
	images := []regionapi.Image{
		image("general", nil),
		image("software-versioned", &softwareVersions),
		notReady,
	}

	body, err := json.Marshal(images)
	require.NoError(t, err)

	client, err := regionapi.NewClientWithResponses("http://region.example", regionapi.WithHTTPClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "/api/v1/organizations/"+organizationID+"/regions/"+regionID+"/images", r.URL.Path)
		assert.Empty(t, r.URL.RawQuery)

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    r,
		}, nil
	})))
	require.NoError(t, err)

	available, err := computeregion.New(client).AvailableImages(t.Context(), identityids.MustParseOrganizationID(organizationID), regionids.MustParseRegionID(regionID))
	require.NoError(t, err)
	require.Len(t, available, 2)
	assert.Equal(t, "general", available[0].Metadata.Id)
	assert.Equal(t, "software-versioned", available[1].Metadata.Id)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func flavor(id string, pinnedOnly *bool) regionapi.Flavor {
	return regionapi.Flavor{
		Metadata: coreapi.StaticResourceMetadata{
			Id: id,
		},
		Spec: regionapi.FlavorSpec{
			Architecture: regionapi.ArchitectureX8664,
			Cpus:         1,
			Disk:         20,
			Memory:       1,
			PinnedOnly:   pinnedOnly,
		},
	}
}

func image(id string, softwareVersions *regionapi.SoftwareVersions) regionapi.Image {
	return regionapi.Image{
		Metadata: coreapi.StaticResourceMetadata{
			Id: id,
		},
		Spec: regionapi.ImageSpec{
			Architecture:     regionapi.ArchitectureX8664,
			SizeGiB:          10,
			SoftwareVersions: softwareVersions,
			Virtualization:   regionapi.ImageVirtualizationVirtualized,
		},
		Status: regionapi.ImageStatus{
			State: regionapi.ImageStateReady,
		},
	}
}
