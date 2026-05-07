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
	unikornv1core "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	unikornv1region "github.com/unikorn-cloud/region/pkg/apis/unikorn/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ComputeInstanceList is a typed list of instances.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ComputeInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ComputeInstance `json:"items"`
}

// ComputeInstance is an object representing a Compute instance.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope=Namespaced,categories=unikorn
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="display name",type="string",JSONPath=".metadata.labels['unikorn-cloud\\.org/name']"
// +kubebuilder:printcolumn:name="status",type="string",JSONPath=".status.conditions[?(@.type==\"Available\")].reason"
// +kubebuilder:printcolumn:name="age",type="date",JSONPath=".metadata.creationTimestamp"
type ComputeInstance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ComputeInstanceSpec   `json:"spec"`
	Status            ComputeInstanceStatus `json:"status,omitempty"`
}

type ComputeInstanceSpec struct {
	unikornv1core.MachineGeneric `json:",inline"`
	// Pause, if true, will inhibit reconciliation.
	Pause bool `json:"pause,omitempty"`
	// Tags are aribrary user data.
	Tags unikornv1core.TagList `json:"tags,omitempty"`
	// Network is networking options.
	Networking *ComputeInstanceNetworking `json:"networking,omitempty"`
	// SSHCertificateAuthorityID is an optional project scoped OpenSSH user CA trust anchor.
	SSHCertificateAuthorityID *string `json:"sshCertificateAuthorityId,omitempty"`
	// UserData is passed to cloud-init and may be a script, a multipart MIME archive etc.
	// as permitted by the cloud-init specification.
	UserData []byte `json:"userData,omitempty"`
}

type ComputeInstanceNetworking struct {
	// PublicIP specifies whether to create a public IP address.
	PublicIP bool `json:"publicIp,omitempty"`
	// SecurityGroupIDs are a list of security group IDs to apply to
	// the instance's network device.
	SecurityGroupIDs []string `json:"securityGroupIDs,omitempty"`
	// AllowedSourceAddresses defines a set of network prefixes that are
	// allowed to egress from the instance.  For use where the instance is
	// being used as a router for NFV.
	AllowedSourceAddresses []unikornv1core.IPv4Prefix `json:"allowedSourceAddresses,omitempty"`
}

type ComputeInstanceStatus struct {
	// PrivateIP is the private IP address.
	// TODO: should be IPv4Address.
	PrivateIP *string `json:"privateIp,omitempty"`
	// PublicIP is the public IP address if requested.
	// TODO: should be IPv4Address.
	PublicIP *string `json:"publicIp,omitempty"`
	// MACAddress is the MAC address of the instance's primary network interface.
	MACAddress *string `json:"macAddress,omitempty"`
	// PowerState is the current status of the machine.
	PowerState *unikornv1region.InstanceLifecyclePhase `json:"powerState,omitempty"`
	// Conditions is a set of status conditions for the machine.
	Conditions []unikornv1core.Condition `json:"conditions,omitempty"`
}
