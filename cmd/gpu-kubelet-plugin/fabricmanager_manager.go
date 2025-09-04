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

package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/klog/v2"

	configapi "github.com/NVIDIA/k8s-dra-driver-gpu/api/nvidia.com/resource/v1beta1"
	"github.com/NVIDIA/k8s-dra-driver-gpu/internal/fabricmanager"
	"github.com/NVIDIA/k8s-dra-driver-gpu/pkg/featuregates"
)

// FabricManagerManager manages FabricManager client and fabric topology operations.
type FabricManagerManager struct {
	client   fabricmanager.Client
	topology *fabricmanager.FabricTopology
	mutex    sync.RWMutex
}

// NewFabricManagerManager creates a new FabricManager manager.
func NewFabricManagerManager(ctx context.Context, config *Config) (*FabricManagerManager, error) {
	if !featuregates.Enabled(featuregates.FabricTopologySupport) {
		klog.V(4).Info("FabricTopologySupport feature gate is disabled, skipping FabricManager initialization")
		return &FabricManagerManager{}, nil
	}

	// Create FabricManager client with default configuration
	// In a real implementation, this would be configured via command line flags or config file
	fmConfig := fabricmanager.DefaultFabricManagerConfig()
	client, err := fabricmanager.NewClient(fmConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create FabricManager client: %w", err)
	}

	manager := &FabricManagerManager{
		client: client,
	}

	// Discover initial fabric topology
	if err := manager.discoverTopology(ctx); err != nil {
		klog.Warningf("Failed to discover initial fabric topology: %v", err)
		// Don't fail initialization if topology discovery fails
		// The system can still function without fabric topology optimization
	}

	// Validate FabricManager service is properly configured
	if err := manager.validateFabricManagerConfiguration(ctx); err != nil {
		klog.Warningf("FabricManager configuration validation failed: %v", err)
		// Don't fail initialization, but log the configuration issue
	}

	return manager, nil
}

// discoverTopology discovers the current fabric topology from FabricManager.
func (m *FabricManagerManager) discoverTopology(ctx context.Context) error {
	if m.client == nil {
		return fmt.Errorf("FabricManager client not initialized")
	}

	topology, err := m.client.GetTopology(ctx)
	if err != nil {
		return fmt.Errorf("failed to get fabric topology: %w", err)
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.topology = topology

	klog.V(4).Infof("Discovered fabric topology with %d partitions and %d interconnects",
		len(topology.Partitions), len(topology.Interconnects))

	return nil
}

// GetTopology returns the current fabric topology.
func (m *FabricManagerManager) GetTopology() *fabricmanager.FabricTopology {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.topology
}

// RefreshTopology refreshes the fabric topology from FabricManager.
func (m *FabricManagerManager) RefreshTopology(ctx context.Context) error {
	return m.discoverTopology(ctx)
}

// FindOptimalPartition finds the optimal fabric partition for the given GPU UUIDs.
func (m *FabricManagerManager) FindOptimalPartition(ctx context.Context, gpuUUIDs []string, config *configapi.FabricTopologyConfig) (*fabricmanager.FabricPartition, error) {
	if config == nil {
		return nil, fmt.Errorf("fabric topology config is nil")
	}

	// Handle disabled strategy early - no need for client or topology
	if config.Strategy == configapi.FabricTopologyDisabled {
		return nil, nil // No fabric optimization
	}

	// Validate virtualization model compatibility
	if err := m.validateVirtualizationModelCompatibility(config); err != nil {
		return nil, fmt.Errorf("virtualization model validation failed: %w", err)
	}

	if m.client == nil {
		return nil, fmt.Errorf("FabricManager client not initialized")
	}

	m.mutex.RLock()
	topology := m.topology
	m.mutex.RUnlock()

	if topology == nil {
		return nil, fmt.Errorf("fabric topology not available")
	}

	switch config.Strategy {
	case configapi.FabricTopologyAuto:
		return m.findOptimalPartitionAuto(topology, gpuUUIDs, config)
	case configapi.FabricTopologyManual:
		return m.findOptimalPartitionManual(topology, gpuUUIDs, config)
	default:
		return nil, fmt.Errorf("unsupported fabric topology strategy: %s", config.Strategy)
	}
}

// findOptimalPartitionAuto finds the optimal partition using automatic strategy.
func (m *FabricManagerManager) findOptimalPartitionAuto(topology *fabricmanager.FabricTopology, gpuUUIDs []string, config *configapi.FabricTopologyConfig) (*fabricmanager.FabricPartition, error) {
	partitionSize, err := config.GetPartitionSize()
	if err != nil {
		return nil, fmt.Errorf("failed to get partition size: %w", err)
	}

	// Find partitions that match the requested size and contain the requested GPUs
	var candidatePartitions []*fabricmanager.FabricPartition
	for _, partition := range topology.Partitions {
		if len(partition.GPUs) == partitionSize {
			// Check if all requested GPUs are in this partition
			if m.containsAllGPUs(partition.GPUs, gpuUUIDs) {
				candidatePartitions = append(candidatePartitions, &partition)
			}
		}
	}

	if len(candidatePartitions) == 0 {
		return nil, fmt.Errorf("no suitable fabric partition found for %d GPUs", partitionSize)
	}

	// For now, return the first suitable partition
	// In a more sophisticated implementation, we could rank partitions by:
	// - Bandwidth availability
	// - Latency characteristics
	// - Current utilization
	return candidatePartitions[0], nil
}

// findOptimalPartitionManual finds the optimal partition using manual strategy.
func (m *FabricManagerManager) findOptimalPartitionManual(topology *fabricmanager.FabricTopology, gpuUUIDs []string, config *configapi.FabricTopologyConfig) (*fabricmanager.FabricPartition, error) {
	partitionIDs, err := config.GetPartitionIDs()
	if err != nil {
		return nil, fmt.Errorf("failed to get partition IDs: %w", err)
	}

	// Find the first partition that matches one of the requested IDs and contains the GPUs
	for _, partitionID := range partitionIDs {
		for _, partition := range topology.Partitions {
			if partition.ID == partitionID && m.containsAllGPUs(partition.GPUs, gpuUUIDs) {
				return &partition, nil
			}
		}
	}

	return nil, fmt.Errorf("no suitable fabric partition found with IDs %v", partitionIDs)
}

// containsAllGPUs checks if the partition contains all the requested GPU UUIDs.
func (m *FabricManagerManager) containsAllGPUs(partitionGPUs []string, requestedGPUs []string) bool {
	partitionSet := make(map[string]bool)
	for _, gpu := range partitionGPUs {
		partitionSet[gpu] = true
	}

	for _, requestedGPU := range requestedGPUs {
		if !partitionSet[requestedGPU] {
			return false
		}
	}
	return true
}

// validateFabricManagerConfiguration validates that FabricManager is properly configured.
func (m *FabricManagerManager) validateFabricManagerConfiguration(ctx context.Context) error {
	if m.client == nil {
		return fmt.Errorf("FabricManager client not initialized")
	}

	// Test basic connectivity by attempting to get topology
	testCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	topology, err := m.client.GetTopology(testCtx)
	if err != nil {
		return fmt.Errorf("failed to connect to FabricManager service: %w", err)
	}

	// Validate topology structure
	if topology == nil {
		return fmt.Errorf("FabricManager returned nil topology")
	}

	if len(topology.Partitions) == 0 {
		klog.V(4).Info("FabricManager topology has no partitions - fabric optimization may not be available")
	}

	// Check for minimum required partitions for common use cases
	hasValidPartitions := false
	for _, partition := range topology.Partitions {
		if len(partition.GPUs) >= 2 {
			hasValidPartitions = true
			break
		}
	}

	if !hasValidPartitions {
		klog.V(4).Info("FabricManager topology has no partitions with 2+ GPUs - multi-GPU fabric optimization may not be available")
	}

	// Validate interconnects
	if len(topology.Interconnects) == 0 {
		klog.V(4).Info("FabricManager topology has no interconnects - fabric optimization may not be available")
	}

	return nil
}

// ActivatePartition activates a fabric partition.
func (m *FabricManagerManager) ActivatePartition(ctx context.Context, partitionID string) error {
	if m.client == nil {
		return fmt.Errorf("FabricManager client not initialized")
	}

	if err := m.client.ActivatePartition(ctx, partitionID); err != nil {
		return fmt.Errorf("failed to activate partition %s: %w", partitionID, err)
	}

	klog.V(4).Infof("Successfully activated fabric partition: %s", partitionID)
	return nil
}

// DeactivatePartition deactivates a fabric partition.
func (m *FabricManagerManager) DeactivatePartition(ctx context.Context, partitionID string) error {
	if m.client == nil {
		return fmt.Errorf("FabricManager client not initialized")
	}

	if err := m.client.DeactivatePartition(ctx, partitionID); err != nil {
		return fmt.Errorf("failed to deactivate partition %s: %w", partitionID, err)
	}

	klog.V(4).Infof("Successfully deactivated fabric partition: %s", partitionID)
	return nil
}

// GetPartitionStatus gets the status of a fabric partition.
func (m *FabricManagerManager) GetPartitionStatus(ctx context.Context, partitionID string) (*fabricmanager.PartitionStatus, error) {
	if m.client == nil {
		return nil, fmt.Errorf("FabricManager client not initialized")
	}

	status, err := m.client.GetPartitionStatus(ctx, partitionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get partition status for %s: %w", partitionID, err)
	}

	return status, nil
}

// validateVirtualizationModelCompatibility validates that the virtualization model is compatible with the current environment.
func (m *FabricManagerManager) validateVirtualizationModelCompatibility(config *configapi.FabricTopologyConfig) error {
	if config.VirtualizationModel == nil {
		// Default to BareMetal if not specified
		return nil
	}

	model := *config.VirtualizationModel

	// Check if the virtualization model is supported and compatible with partition management
	switch model {
	case configapi.FabricVirtualizationBareMetal:
		// BareMetal supports full partition management
		klog.V(4).Info("BareMetal virtualization model selected - full partition management available")
		return nil

	case configapi.FabricVirtualizationFullPassthrough:
		// FullPassthrough supports partition management but requires VM environment with GPU passthrough
		klog.V(4).Info("FullPassthrough virtualization model selected - partition management available with GPU passthrough")
		return nil

	case configapi.FabricVirtualizationSharedNVSwitch:
		// SharedNVSwitch requires service VM for partition management
		// Guest VMs cannot directly manage partitions
		klog.V(4).Info("SharedNVSwitch virtualization model selected - partition management via service VM only")
		return nil

	case configapi.FabricVirtualizationVGPU:
		// vGPU does not support fabric partitions or NVLink optimization
		return fmt.Errorf("vGPU virtualization model does not support fabric topology optimization - NVLink partitions are not available with vGPU")

	default:
		return fmt.Errorf("unsupported virtualization model: %s", model)
	}
}

// CanManagePartitions checks if the current virtualization model supports direct partition management.
func (m *FabricManagerManager) CanManagePartitions(config *configapi.FabricTopologyConfig) bool {
	if config == nil || config.VirtualizationModel == nil {
		// Default to BareMetal which supports partition management
		return true
	}

	model := *config.VirtualizationModel
	switch model {
	case configapi.FabricVirtualizationBareMetal, configapi.FabricVirtualizationFullPassthrough:
		// These models support direct partition management
		return true
	case configapi.FabricVirtualizationSharedNVSwitch, configapi.FabricVirtualizationVGPU:
		// These models do not support direct partition management
		return false
	default:
		// Unknown model - be conservative
		return false
	}
}

// RequiresServiceVM checks if the virtualization model requires a service VM for partition management.
func (m *FabricManagerManager) RequiresServiceVM(config *configapi.FabricTopologyConfig) bool {
	if config == nil || config.VirtualizationModel == nil {
		return false
	}

	model := *config.VirtualizationModel
	return model == configapi.FabricVirtualizationSharedNVSwitch
}

// SupportsFabricTopology checks if the virtualization model supports fabric topology optimization.
func (m *FabricManagerManager) SupportsFabricTopology(config *configapi.FabricTopologyConfig) bool {
	if config == nil || config.VirtualizationModel == nil {
		// Default to BareMetal which supports fabric topology
		return true
	}

	model := *config.VirtualizationModel
	switch model {
	case configapi.FabricVirtualizationBareMetal, configapi.FabricVirtualizationFullPassthrough, configapi.FabricVirtualizationSharedNVSwitch:
		// These models support fabric topology optimization
		return true
	case configapi.FabricVirtualizationVGPU:
		// vGPU does not support fabric topology optimization
		return false
	default:
		// Unknown model - be conservative
		return false
	}
}

// Close closes the FabricManager client.
func (m *FabricManagerManager) Close() error {
	if m.client == nil {
		return nil
	}

	return m.client.Close()
}
