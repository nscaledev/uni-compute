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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	computeapi "github.com/unikorn-cloud/compute/test/api"
)

func TestRunSSHCommandWithUserFallbackAuthFactoryError(t *testing.T) {
	t.Parallel()

	_, _, err := computeapi.RunSSHCommandWithUserFallback("127.0.0.1:22", []string{"cloud-user"}, func(string) ([]ssh.AuthMethod, error) {
		return nil, assert.AnError
	}, time.Second, "true")

	require.Error(t, err)
	assert.ErrorContains(t, err, "building SSH auth methods for user \"cloud-user\"")
}
