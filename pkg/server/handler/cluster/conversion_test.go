/*
Copyright 2025 the Unikorn Authors.
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

package cluster_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	computev1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	computeapi "github.com/unikorn-cloud/compute/pkg/openapi"
	"github.com/unikorn-cloud/compute/pkg/server/handler/cluster"
	"github.com/unikorn-cloud/compute/pkg/server/handler/region/mock"
	corev1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	"k8s.io/utils/ptr"
)

const (
	organizationID = "foo"
	projectID      = "bar"
	regionID       = "baz"

	image1ID = "aeaa976e-b4c7-404c-8f0a-4f987793f7a1"
	image2ID = "95d27d38-c793-4f68-a330-27e7d776db2a"
	image3ID = "f32da863-59ed-4832-b0f1-37a15c94fc79"
)

func images() []regionapi.Image {
	now := time.Now()

	// NOTE: images returned by the region service are ordered by
	// creation time, with the newest first.
	return []regionapi.Image{
		{
			Metadata: coreapi.StaticResourceMetadata{
				Id:           image1ID,
				CreationTime: now.Add(2 * time.Hour),
			},
			Spec: regionapi.ImageSpec{
				Os: regionapi.ImageOS{
					Kernel:  regionapi.OsKernelLinux,
					Family:  regionapi.OsFamilyRedhat,
					Distro:  regionapi.OsDistroRocky,
					Version: "8",
				},
			},
		},
		{
			Metadata: coreapi.StaticResourceMetadata{
				Id:           image2ID,
				CreationTime: now.Add(time.Hour),
			},
			Spec: regionapi.ImageSpec{
				Os: regionapi.ImageOS{
					Kernel:  regionapi.OsKernelLinux,
					Family:  regionapi.OsFamilyDebian,
					Distro:  regionapi.OsDistroUbuntu,
					Version: "24.04",
				},
			},
		},
		{
			Metadata: coreapi.StaticResourceMetadata{
				Id:           image3ID,
				CreationTime: now,
			},
			Spec: regionapi.ImageSpec{
				Os: regionapi.ImageOS{
					Kernel:  regionapi.OsKernelLinux,
					Family:  regionapi.OsFamilyDebian,
					Distro:  regionapi.OsDistroUbuntu,
					Version: "24.04",
				},
			},
		},
	}
}

// TestImageSelectionByID ensures we can select an image by ID.
func TestImageSelectionByID(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	region := mock.NewMockClientInterface(c)

	g := cluster.NewGenerator(nil, nil, region, "", organizationID, regionID, nil)

	// Test 1: selects correct image by ID.
	pool := &computeapi.ComputeClusterWorkloadPool{
		Machine: computeapi.MachinePool{
			Image: computeapi.ComputeImage{
				Id: ptr.To(image2ID),
			},
		},
	}

	region.EXPECT().Images(t.Context(), organizationID, regionID).Return(images(), nil)

	image, err := cluster.ChooseImage(t.Context(), g, regionID, pool, nil)
	require.NoError(t, err)
	require.Equal(t, image2ID, image.Metadata.Id)

	// Test 2: selects another correct image by ID.
	pool = &computeapi.ComputeClusterWorkloadPool{
		Machine: computeapi.MachinePool{
			Image: computeapi.ComputeImage{
				Id: ptr.To(image1ID),
			},
		},
	}

	region.EXPECT().Images(t.Context(), organizationID, regionID).Return(images(), nil)

	image, err = cluster.ChooseImage(t.Context(), g, regionID, pool, nil)
	require.NoError(t, err)
	require.Equal(t, image1ID, image.Metadata.Id)

	// Test 3: Fails fast on missing ID.
	pool = &computeapi.ComputeClusterWorkloadPool{
		Machine: computeapi.MachinePool{
			Image: computeapi.ComputeImage{
				Id: ptr.To("fail"),
			},
		},
	}

	region.EXPECT().Images(t.Context(), organizationID, regionID).Return(images(), nil)

	_, err = cluster.ChooseImage(t.Context(), g, regionID, pool, nil)
	require.Error(t, err)
}

// TestImageSelectionByMetadata ensures we can select an image by metadata.
// The newest will be returned if there are multiple matches.
func TestImageSelectionByMetadata(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	region := mock.NewMockClientInterface(c)

	g := cluster.NewGenerator(nil, nil, region, "", organizationID, regionID, nil)

	// Test 1: selects correct image by metadata.
	pool := &computeapi.ComputeClusterWorkloadPool{
		Machine: computeapi.MachinePool{
			Image: computeapi.ComputeImage{
				Selector: &computeapi.ImageSelector{
					Distro:  regionapi.OsDistroUbuntu,
					Version: "24.04",
				},
			},
		},
	}

	region.EXPECT().Images(t.Context(), organizationID, regionID).Return(images(), nil)

	image, err := cluster.ChooseImage(t.Context(), g, regionID, pool, nil)
	require.NoError(t, err)
	require.Equal(t, image2ID, image.Metadata.Id)

	// Test 2: selects another correct image by metadata.
	pool = &computeapi.ComputeClusterWorkloadPool{
		Machine: computeapi.MachinePool{
			Image: computeapi.ComputeImage{
				Selector: &computeapi.ImageSelector{
					Distro:  regionapi.OsDistroRocky,
					Version: "8",
				},
			},
		},
	}

	region.EXPECT().Images(t.Context(), organizationID, regionID).Return(images(), nil)

	image, err = cluster.ChooseImage(t.Context(), g, regionID, pool, nil)
	require.NoError(t, err)
	require.Equal(t, image1ID, image.Metadata.Id)

	// Test 3: Fails fast on missing image.
	pool = &computeapi.ComputeClusterWorkloadPool{
		Machine: computeapi.MachinePool{
			Image: computeapi.ComputeImage{
				Selector: &computeapi.ImageSelector{
					Distro:  regionapi.OsDistroUbuntu,
					Version: "12.04",
				},
			},
		},
	}

	region.EXPECT().Images(t.Context(), organizationID, regionID).Return(images(), nil)

	_, err = cluster.ChooseImage(t.Context(), g, regionID, pool, nil)
	require.Error(t, err)
}

// TestImageSelectionPreservation ensures when using metadata image selection
// the image is preserved across generations.  This is the same as TestImageSelectionByMetadata
// but pins the image to an earlier version.
func TestImageSelectionPreservation(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	region := mock.NewMockClientInterface(c)

	current := &computev1.ComputeCluster{
		Spec: computev1.ComputeClusterSpec{
			WorkloadPools: &computev1.ComputeClusterWorkloadPoolsSpec{
				Pools: []computev1.ComputeClusterWorkloadPoolSpec{
					{
						Name: "my-pool",
						MachineGeneric: corev1.MachineGeneric{
							ImageID: image3ID,
						},
					},
				},
			},
		},
	}

	g := cluster.NewGenerator(nil, nil, region, "", organizationID, regionID, current)

	// Test 1: preserves non-default image.
	pool := &computeapi.ComputeClusterWorkloadPool{
		Name: "my-pool",
		Machine: computeapi.MachinePool{
			Image: computeapi.ComputeImage{
				Selector: &computeapi.ImageSelector{
					Distro:  regionapi.OsDistroUbuntu,
					Version: "24.04",
				},
			},
		},
	}

	region.EXPECT().Images(t.Context(), organizationID, regionID).Return(images(), nil)

	image, err := cluster.ChooseImage(t.Context(), g, regionID, pool, nil)
	require.NoError(t, err)
	require.Equal(t, image3ID, image.Metadata.Id)

	// Test 2: updates images if the image canont be preserved.
	pool = &computeapi.ComputeClusterWorkloadPool{
		Name: "my-pool",
		Machine: computeapi.MachinePool{
			Image: computeapi.ComputeImage{
				Selector: &computeapi.ImageSelector{
					Distro:  regionapi.OsDistroRocky,
					Version: "8",
				},
			},
		},
	}

	region.EXPECT().Images(t.Context(), organizationID, regionID).Return(images(), nil)

	image, err = cluster.ChooseImage(t.Context(), g, regionID, pool, nil)
	require.NoError(t, err)
	require.Equal(t, image1ID, image.Metadata.Id)
}
