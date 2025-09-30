/*
 * Copyright (c) 2025, NVIDIA CORPORATION. All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package v1beta1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/NVIDIA/k8s-dra-driver-gpu/pkg/featuregates"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FabricManagerConfig represents the configuration for FabricManager device allocation.
// This configuration enables fabric partition management for Multi-Node NVLink workloads.
type FabricManagerConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the FabricManager configuration specification.
	Spec FabricManagerConfigSpec `json:"spec"`
}

// FabricManagerConfigSpec defines the specification for FabricManager configuration.
type FabricManagerConfigSpec struct {
	// PartitionName specifies the name of the fabric partition to create.
	// If empty, a default name will be generated.
	// +optional
	PartitionName string `json:"partitionName,omitempty"`

	// CliqueID specifies the clique ID for the fabric partition.
	// If empty, the GPU's default clique ID will be used.
	// +optional
	CliqueID string `json:"cliqueId,omitempty"`

	// Sharing defines how the fabric partition should be shared between workloads.
	// +optional
	Sharing *FabricManagerSharing `json:"sharing,omitempty"`
}

// FabricManagerSharing defines sharing configuration for fabric partitions.
type FabricManagerSharing struct {
	// Strategy defines the sharing strategy for fabric partitions.
	// Currently only "Exclusive" is supported for fabric partitions.
	Strategy FabricManagerSharingStrategy `json:"strategy"`
}

// FabricManagerSharingStrategy defines the available sharing strategies for fabric partitions.
type FabricManagerSharingStrategy string

const (
	// FabricManagerSharingStrategyExclusive means the fabric partition is exclusively allocated to a single workload.
	FabricManagerSharingStrategyExclusive FabricManagerSharingStrategy = "Exclusive"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FabricManagerConfigList contains a list of FabricManagerConfig objects.
type FabricManagerConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FabricManagerConfig `json:"items"`
}

// DefaultFabricManagerConfig provides the default FabricManager configuration.
func DefaultFabricManagerConfig() *FabricManagerConfig {
	config := &FabricManagerConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: GroupName + "/" + Version,
			Kind:       "FabricManagerConfig",
		},
	}

	if featuregates.Enabled(featuregates.FabricManagerSupport) {
		config.Spec.Sharing = &FabricManagerSharing{
			Strategy: FabricManagerSharingStrategyExclusive,
		}
	}

	return config
}

// Normalize sets any implied defaults for the FabricManager configuration.
func (c *FabricManagerConfig) Normalize() error {
	// Set default sharing strategy if not specified
	if c.Spec.Sharing == nil {
		c.Spec.Sharing = &FabricManagerSharing{
			Strategy: FabricManagerSharingStrategyExclusive,
		}
	}
	return nil
}

// Validate ensures the integrity of the FabricManager configuration.
func (c *FabricManagerConfig) Validate() error {
	// Validate sharing strategy
	if c.Spec.Sharing != nil {
		if c.Spec.Sharing.Strategy != FabricManagerSharingStrategyExclusive {
			return fmt.Errorf("invalid sharing strategy: %s", c.Spec.Sharing.Strategy)
		}
	}
	return nil
}
