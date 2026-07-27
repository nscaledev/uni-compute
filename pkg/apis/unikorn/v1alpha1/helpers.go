/*
Copyright 2024-2025 the Unikorn Authors.
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

package v1alpha1

import (
	"errors"

	unikornv1core "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	unikornv1region "github.com/unikorn-cloud/region/pkg/apis/unikorn/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

var (
	// ErrMissingLabel is raised when an expected label is not present on
	// a resource.
	ErrMissingLabel = errors.New("expected label is missing")

	// ErrApplicationLookup is raised when the named application is not
	// present in an application bundle bundle.
	ErrApplicationLookup = errors.New("failed to lookup an application")
)

// Paused implements the ReconcilePauser interface.
func (c *ComputeInstance) Paused() bool {
	return c.Spec.Pause
}

// StatusConditionRead scans the status conditions for an existing condition whose type
// matches.
func (c *ComputeInstance) StatusConditionRead(t unikornv1core.ConditionType) (*metav1.Condition, error) {
	return unikornv1core.GetCondition(c.Status.Conditions, t)
}

// SetProvisioningCondition sets the Available condition with a reason drawn from
// the provisioning vocabulary.
func (c *ComputeInstance) SetProvisioningCondition(status corev1.ConditionStatus, reason unikornv1core.ProvisioningConditionReason, message string) {
	unikornv1core.UpdateCondition(&c.Status.Conditions, unikornv1core.ConditionAvailable, status, string(reason), message)
}

// SetHealthCondition sets the Healthy condition with a reason drawn from the
// health vocabulary. The instance mirrors the backing region server's health
// verdict onto this condition.
func (c *ComputeInstance) SetHealthCondition(status corev1.ConditionStatus, reason unikornv1core.HealthConditionReason, message string) {
	unikornv1core.UpdateCondition(&c.Status.Conditions, unikornv1core.ConditionHealthy, status, string(reason), message)
}

// SetActiveCondition sets the generic Active condition (the lifecycle/power axis)
// to the state mirrored from the backing region server, reusing region's
// domain-owned ActiveConditionReason vocabulary. As on the region server, the
// condition's status and message are pure projections of the reason (True only
// when Running), so the setter takes only the reason.
func (c *ComputeInstance) SetActiveCondition(reason unikornv1region.ActiveConditionReason) {
	unikornv1core.UpdateCondition(&c.Status.Conditions, unikornv1core.ConditionActive, reason.ConditionStatus(), string(reason), reason.Message())
}

// GetActiveCondition reads the Active condition, narrowing its reason to region's
// lifecycle/power vocabulary via core's generic typed handling.
func GetActiveCondition(r unikornv1core.StatusConditionReader) (*unikornv1core.TypedCondition[unikornv1region.ActiveConditionReason], error) {
	return unikornv1core.GetTypedCondition[unikornv1region.ActiveConditionReason](r, unikornv1core.ConditionActive)
}

// ResourceLabels generates a set of labels to uniquely identify the resource
// if it were to be placed in a single global namespace.
func (c *ComputeInstance) ResourceLabels() (labels.Set, error) {
	//nolint:nilnil
	return nil, nil
}

// PublicIPEnabled tells us if the instance has a public IP requested.
func (c *ComputeInstance) PublicIPEnabled() bool {
	return c.Spec.Networking != nil && c.Spec.Networking.PublicIP
}
