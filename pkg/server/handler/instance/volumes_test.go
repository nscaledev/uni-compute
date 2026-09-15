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
	"testing"

	"github.com/stretchr/testify/assert"

	computeapi "github.com/unikorn-cloud/compute/pkg/openapi"
	"github.com/unikorn-cloud/compute/pkg/server/handler/instance"
	idstest "github.com/unikorn-cloud/region/pkg/ids/idstest"
)

func TestUpdatedVolumes(t *testing.T) {
	t.Parallel()

	current := []string{"f5c1ccbf-cbdd-49fd-b5b9-90902464851f"}
	empty := computeapi.InstanceVolumeList{}
	replacement := computeapi.InstanceVolumeList{idstest.MustParseVolumeID("3a21348e-d20a-459c-a57f-d1b24c94ba7f")}

	assert.Equal(t, current, instance.UpdatedVolumes(nil, current))
	assert.Empty(t, instance.UpdatedVolumes(&empty, current))
	assert.Equal(t, []string{replacement[0].String()}, instance.UpdatedVolumes(&replacement, current))
}
