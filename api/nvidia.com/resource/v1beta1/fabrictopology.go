/*
 * Copyright (c) 2024, NVIDIA CORPORATION.  All rights reserved.
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
	"strings"

	"k8s.io/utils/ptr"
)

// These constants represent the different fabric topology strategies.
const (
	// FabricTopologyAuto automatically selects the best fabric topology based on available resources.
	FabricTopologyAuto = "Auto"
	// FabricTopologyManual allows manual specification of fabric partitions.
	FabricTopologyManual = "Manual"
	// FabricTopologyDisabled disables fabric topology optimization.
	FabricTopologyDisabled = "Disabled"
)

// These constants represent the different fabric partition sizes.
const (
	// FabricPartitionSize2 represents a 2-GPU fabric partition.
	FabricPartitionSize2 = 2
	// FabricPartitionSize4 represents a 4-GPU fabric partition.
	FabricPartitionSize4 = 4
	// FabricPartitionSize8 represents an 8-GPU fabric partition.
	FabricPartitionSize8 = 8
)

// These constants represent the different virtualization models supported by FabricManager.
const (
	// FabricVirtualizationBareMetal - Bare metal mode with direct GPU access.
	FabricVirtualizationBareMetal = "BareMetal"
	// FabricVirtualizationFullPassthrough - Full passthrough virtualization model.
	FabricVirtualizationFullPassthrough = "FullPassthrough"
	// FabricVirtualizationSharedNVSwitch - Shared NVSwitch virtualization model.
	FabricVirtualizationSharedNVSwitch = "SharedNVSwitch"
	// FabricVirtualizationVGPU - vGPU virtualization model.
	FabricVirtualizationVGPU = "VGPU"
)

// FabricTopologyStrategy encodes the valid fabric topology strategies as a string.
type FabricTopologyStrategy string

// FabricTopologyConfig provides the configuration for fabric topology optimization.
type FabricTopologyConfig struct {
	// Strategy defines how fabric topology should be configured
	Strategy FabricTopologyStrategy `json:"strategy"`
	// VirtualizationModel specifies the virtualization model for fabric topology
	// This determines how FabricManager handles GPU partitions in different environments
	VirtualizationModel *string `json:"virtualizationModel,omitempty"`
	// PartitionSize specifies the desired partition size for automatic topology selection
	// Only used when Strategy is set to FabricTopologyAuto
	PartitionSize *int `json:"partitionSize,omitempty"`
	// PartitionIDs specifies the exact fabric partition IDs to use
	// Only used when Strategy is set to FabricTopologyManual
	PartitionIDs []string `json:"partitionIds,omitempty"`
	// MinBandwidth specifies the minimum bandwidth requirement in GB/s
	MinBandwidth *float64 `json:"minBandwidth,omitempty"`
	// MaxLatency specifies the maximum latency requirement in milliseconds
	MaxLatency *float64 `json:"maxLatency,omitempty"`
	// PreferNVLink indicates whether to prefer NVLink over PCIe when possible
	PreferNVLink *bool `json:"preferNVLink,omitempty"`
}

// DefaultFabricTopologyConfig provides the default fabric topology configuration.
func DefaultFabricTopologyConfig() *FabricTopologyConfig {
	return &FabricTopologyConfig{
		Strategy:            FabricTopologyAuto,
		VirtualizationModel: ptr.To(FabricVirtualizationBareMetal),
		PartitionSize:       ptr.To(FabricPartitionSize4),
		MinBandwidth:        ptr.To(600.0), // 600 GB/s typical NVLink bandwidth
		MaxLatency:          ptr.To(0.1),   // 0.1ms typical NVLink latency
		PreferNVLink:        ptr.To(true),
	}
}

// IsAuto checks if the Auto strategy is applied.
func (c *FabricTopologyConfig) IsAuto() bool {
	if c == nil {
		return false
	}
	return c.Strategy == FabricTopologyAuto
}

// IsManual checks if the Manual strategy is applied.
func (c *FabricTopologyConfig) IsManual() bool {
	if c == nil {
		return false
	}
	return c.Strategy == FabricTopologyManual
}

// IsDisabled checks if the Disabled strategy is applied.
func (c *FabricTopologyConfig) IsDisabled() bool {
	if c == nil {
		return false
	}
	return c.Strategy == FabricTopologyDisabled
}

// GetPartitionSize returns the partition size for auto strategy.
func (c *FabricTopologyConfig) GetPartitionSize() (int, error) {
	if c == nil {
		return 0, fmt.Errorf("no fabric topology config set")
	}
	if c.Strategy != FabricTopologyAuto {
		return 0, fmt.Errorf("strategy is not set to '%v'", FabricTopologyAuto)
	}
	if c.PartitionSize == nil {
		return FabricPartitionSize4, nil // Default partition size
	}
	return *c.PartitionSize, nil
}

// GetPartitionIDs returns the partition IDs for manual strategy.
func (c *FabricTopologyConfig) GetPartitionIDs() ([]string, error) {
	if c == nil {
		return nil, fmt.Errorf("no fabric topology config set")
	}
	if c.Strategy != FabricTopologyManual {
		return nil, fmt.Errorf("strategy is not set to '%v'", FabricTopologyManual)
	}
	if len(c.PartitionIDs) == 0 {
		return nil, fmt.Errorf("no partition IDs specified for manual strategy")
	}
	return c.PartitionIDs, nil
}

// Validate ensures that FabricTopologyConfig has a valid set of values.
func (c *FabricTopologyConfig) Validate() error {
	if c == nil {
		return nil
	}

	// Validate strategy
	if err := c.Strategy.Validate(); err != nil {
		return err
	}

	// Validate virtualization model
	if c.VirtualizationModel != nil {
		if err := validateVirtualizationModel(*c.VirtualizationModel); err != nil {
			return err
		}
	}

	// Validate strategy-specific settings
	switch c.Strategy {
	case FabricTopologyAuto:
		if c.PartitionSize != nil {
			if err := validatePartitionSize(*c.PartitionSize); err != nil {
				return err
			}
		}
		if len(c.PartitionIDs) > 0 {
			return fmt.Errorf("partition IDs should not be specified for auto strategy")
		}
	case FabricTopologyManual:
		if len(c.PartitionIDs) == 0 {
			return fmt.Errorf("partition IDs must be specified for manual strategy")
		}
		for i, id := range c.PartitionIDs {
			if strings.TrimSpace(id) == "" {
				return fmt.Errorf("partition ID at index %d is empty", i)
			}
		}
		if c.PartitionSize != nil {
			return fmt.Errorf("partition size should not be specified for manual strategy")
		}
	case FabricTopologyDisabled:
		if c.PartitionSize != nil {
			return fmt.Errorf("partition size should not be specified for disabled strategy")
		}
		if len(c.PartitionIDs) > 0 {
			return fmt.Errorf("partition IDs should not be specified for disabled strategy")
		}
	}

	// Validate bandwidth and latency constraints
	if c.MinBandwidth != nil && *c.MinBandwidth < 0 {
		return fmt.Errorf("minimum bandwidth must not be negative")
	}
	if c.MaxLatency != nil && *c.MaxLatency < 0 {
		return fmt.Errorf("maximum latency must not be negative")
	}

	return nil
}

// validatePartitionSize ensures the partition size is valid.
func validatePartitionSize(size int) error {
	switch size {
	case FabricPartitionSize2, FabricPartitionSize4, FabricPartitionSize8:
		return nil
	default:
		return fmt.Errorf("invalid partition size: %d, must be one of [%d, %d, %d]",
			size, FabricPartitionSize2, FabricPartitionSize4, FabricPartitionSize8)
	}
}

// Validate ensures that FabricTopologyStrategy has a valid set of values.
func (s FabricTopologyStrategy) Validate() error {
	switch s {
	case FabricTopologyAuto, FabricTopologyManual, FabricTopologyDisabled:
		return nil
	default:
		return fmt.Errorf("unknown fabric topology strategy: %v", s)
	}
}

// validateVirtualizationModel validates the virtualization model.
func validateVirtualizationModel(model string) error {
	switch model {
	case FabricVirtualizationBareMetal, FabricVirtualizationFullPassthrough,
		FabricVirtualizationSharedNVSwitch, FabricVirtualizationVGPU:
		return nil
	default:
		return fmt.Errorf("unknown virtualization model: %v. Supported models: %v, %v, %v, %v",
			model, FabricVirtualizationBareMetal, FabricVirtualizationFullPassthrough,
			FabricVirtualizationSharedNVSwitch, FabricVirtualizationVGPU)
	}
}
