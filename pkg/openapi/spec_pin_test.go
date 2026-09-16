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

package openapi_test

import (
	"os"
	"regexp"
	"testing"

	"golang.org/x/mod/modfile"
)

// The region schema is pulled in by remote $ref, so the pinned tag must track
// the Go module or the embedded spec validates responses against a stale enum.
func TestRegionSpecPinMatchesGoModule(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}

	mod, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatal(err)
	}

	want := ""

	for _, r := range mod.Require {
		if r.Mod.Path == "github.com/unikorn-cloud/region" {
			want = r.Mod.Version
		}
	}

	if want == "" {
		t.Fatal("go.mod does not require github.com/unikorn-cloud/region")
	}

	pin := regexp.MustCompile(`unikorn-cloud/region/([^/]+)/pkg/openapi`)

	for _, file := range []string{"server.spec.yaml", "config.yaml"} {
		spec, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		for _, m := range pin.FindAllSubmatch(spec, -1) {
			if got := string(m[1]); got != want {
				t.Errorf("%s pins region %s, go.mod requires %s", file, got, want)
			}
		}
	}
}
