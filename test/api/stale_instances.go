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

package api

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const staleInstanceMinimumAge = time.Hour

// CleanupStaleTestInstances requests deletion for leaked API test instances and
// logs stale findings as GitHub Actions annotations.
func CleanupStaleTestInstances(ctx context.Context, client *APIClient, organizationID, projectID, prefix string) error {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		fmt.Println("::error::Skipping stale compute instance cleanup: prefix is empty")

		return nil
	}

	instances, err := client.ListInstances(ctx, organizationID, projectID)
	if err != nil {
		return fmt.Errorf("listing instances: %w", err)
	}

	var (
		found int
		now   = time.Now()
	)

	for _, instance := range instances {
		if !strings.HasPrefix(instance.Metadata.Name, prefix) {
			continue
		}

		if now.Sub(instance.Metadata.CreationTime) <= staleInstanceMinimumAge {
			continue
		}

		found++

		fmt.Printf("::error::Found stale compute instance %q (%s), status=%s, created=%s\n",
			instance.Metadata.Name,
			instance.Metadata.Id,
			instance.Metadata.ProvisioningStatus,
			instance.Metadata.CreationTime.Format(time.RFC3339),
		)

		if err := client.DeleteInstance(ctx, instance.Metadata.Id); err != nil {
			fmt.Printf("::error::Failed to delete stale compute instance %q (%s): %v\n",
				instance.Metadata.Name,
				instance.Metadata.Id,
				err,
			)

			continue
		}

		fmt.Printf("Requested cleanup for stale compute instance %q (%s)\n", instance.Metadata.Name, instance.Metadata.Id)
	}

	if found == 0 {
		fmt.Printf("No stale compute API test instances found with prefix %q\n", prefix)
	}

	return nil
}
