/*
 * Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
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
	"k8s.io/utils/ptr"

	"github.com/NVIDIA/k8s-dra-driver-gpu/pkg/featuregates"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GpuConfig holds the set of parameters for configuring a GPU.
type GpuConfig struct {
	metav1.TypeMeta      `json:",inline"`
	Sharing              *GpuSharing           `json:"sharing,omitempty"`
	FabricTopologyConfig *FabricTopologyConfig `json:"fabricTopologyConfig,omitempty"`
}

// DefaultGpuConfig provides the default GPU configuration.
func DefaultGpuConfig() *GpuConfig {
	config := &GpuConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: GroupName + "/" + Version,
			Kind:       GpuConfigKind,
		},
	}

	if featuregates.Enabled(featuregates.TimeSlicingSettings) {
		config.Sharing = &GpuSharing{
			Strategy: TimeSlicingStrategy,
			TimeSlicingConfig: &TimeSlicingConfig{
				Interval: ptr.To(DefaultTimeSlice),
			},
		}
	}

	if featuregates.Enabled(featuregates.FabricTopologySupport) {
		config.FabricTopologyConfig = DefaultFabricTopologyConfig()
	}

	return config
}

// Normalize updates a GpuConfig config with implied default values based on other settings.
func (c *GpuConfig) Normalize() error {
	if c.Sharing == nil {
		if !featuregates.Enabled(featuregates.TimeSlicingSettings) {
			return nil
		}
		c.Sharing = &GpuSharing{
			Strategy: TimeSlicingStrategy,
		}
	}

	if featuregates.Enabled(featuregates.TimeSlicingSettings) {
		if c.Sharing.Strategy == TimeSlicingStrategy && c.Sharing.TimeSlicingConfig == nil {
			c.Sharing.TimeSlicingConfig = &TimeSlicingConfig{
				Interval: ptr.To(DefaultTimeSlice),
			}
		}
	}

	if featuregates.Enabled(featuregates.MPSSupport) {
		if c.Sharing.Strategy == MpsStrategy && c.Sharing.MpsConfig == nil {
			c.Sharing.MpsConfig = &MpsConfig{}
		}
	}

	if featuregates.Enabled(featuregates.FabricTopologySupport) {
		if c.FabricTopologyConfig == nil {
			c.FabricTopologyConfig = DefaultFabricTopologyConfig()
		}
	}

	return nil
}

// Validate ensures that GpuConfig has a valid set of values.
func (c *GpuConfig) Validate() error {
	if c.Sharing != nil {
		if err := c.Sharing.Validate(); err != nil {
			return err
		}
	}

	if c.FabricTopologyConfig != nil {
		if err := c.FabricTopologyConfig.Validate(); err != nil {
			return err
		}
	}

	// Validate compatibility between sharing strategy and fabric topology
	if c.Sharing != nil && c.FabricTopologyConfig != nil {
		if err := c.validateSharingFabricCompatibility(); err != nil {
			return err
		}
	}

	return nil
}

// validateSharingFabricCompatibility validates that the sharing strategy is compatible with fabric topology.
func (c *GpuConfig) validateSharingFabricCompatibility() error {
	// Check if both configs are present
	if c.Sharing == nil || c.FabricTopologyConfig == nil {
		return nil
	}

	// Only validate if fabric topology is not disabled
	if c.FabricTopologyConfig.Strategy == FabricTopologyDisabled {
		return nil
	}

	switch c.Sharing.Strategy {
	case MpsStrategy:
		// MPS is not compatible with fabric topology optimization
		// MPS creates a single control daemon that manages all processes,
		// which conflicts with fabric partition management
		return fmt.Errorf("MPS sharing strategy is not compatible with fabric topology optimization. Consider using TimeSlicing or disabling fabric topology")

	case TimeSlicingStrategy:
		// TimeSlicing is compatible with fabric topology
		// TimeSlicing works at the CUDA context level and doesn't interfere with fabric partitions
		return nil

	default:
		// Unknown sharing strategy - be conservative and reject
		return fmt.Errorf("unknown sharing strategy %s is not validated for fabric topology compatibility", c.Sharing.Strategy)
	}
}
