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
	"fmt"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/klog/v2"

	configapi "github.com/NVIDIA/k8s-dra-driver-gpu/api/nvidia.com/resource/v1beta1"
	"github.com/NVIDIA/k8s-dra-driver-gpu/pkg/featuregates"
)

// shouldCreateFabricManagerPartition determines if FabricManager partition should be created.
func (s *DeviceState) shouldCreateFabricManagerPartition(results []resourceapi.DeviceRequestAllocationResult) bool {
	// Check if auto-detection is enabled
	if !featuregates.Enabled(featuregates.AutoFabricManagerDetection) {
		return false
	}

	// Check if multiple GPUs are requested
	if len(results) < 2 {
		return false
	}

	// Check if any GPU supports FabricManager
	for _, result := range results {
		device := s.allocatable[result.Device]
		if device.Type() == GpuDeviceType && s.isFabricManagerCapable(device.Gpu) {
			return true
		}
	}

	return false
}

// autoCreateFabricManagerConfig automatically creates FabricManager config for multi-GPU scenarios.
func (s *DeviceState) autoCreateFabricManagerConfig(results []resourceapi.DeviceRequestAllocationResult, claim *resourceapi.ResourceClaim) (*configapi.FabricManagerConfig, error) {
	if !s.shouldCreateFabricManagerPartition(results) {
		return nil, nil
	}

	// Get optimal clique ID for the GPU combination
	gpuUUIDs := make([]string, len(results))
	for i, result := range results {
		gpuUUIDs[i] = result.Device
	}

	optimalCliqueID, err := s.findOptimalCliqueID(gpuUUIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to find optimal clique ID: %w", err)
	}

	// Create FabricManager config
	config := &configapi.FabricManagerConfig{
		Spec: configapi.FabricManagerConfigSpec{
			PartitionName: fmt.Sprintf("auto-partition-%s", claim.UID),
			CliqueID:      optimalCliqueID,
			Sharing: &configapi.FabricManagerSharing{
				Strategy: configapi.FabricManagerSharingStrategyExclusive,
			},
		},
	}

	return config, nil
}

// findOptimalCliqueID finds the optimal clique ID for a set of GPUs.
func (s *DeviceState) findOptimalCliqueID(gpuUUIDs []string) (string, error) {
	topology, err := s.discoverFabricTopology()
	if err != nil {
		return "", fmt.Errorf("failed to discover fabric topology: %w", err)
	}

	// Find the clique that contains the most GPUs from the requested set
	cliqueScores := make(map[string]int)
	for _, uuid := range gpuUUIDs {
		gpuInfo := topology.GPUs[uuid]
		if gpuInfo != nil {
			cliqueScores[gpuInfo.CliqueID]++
		}
	}

	if len(cliqueScores) == 0 {
		return "", fmt.Errorf("no GPUs found in topology")
	}

	// Find the clique with the highest score
	bestCliqueID := ""
	bestScore := 0
	for cliqueID, score := range cliqueScores {
		if score > bestScore {
			bestScore = score
			bestCliqueID = cliqueID
		}
	}

	// If multiple cliques have the same score, prefer the one with better connectivity
	if bestScore > 0 {
		// Check if there are ties
		tiedCliques := make([]string, 0)
		for cliqueID, score := range cliqueScores {
			if score == bestScore {
				tiedCliques = append(tiedCliques, cliqueID)
			}
		}

		if len(tiedCliques) > 1 {
			// Choose the clique with the best connectivity
			bestCliqueID = s.chooseBestCliqueByConnectivity(tiedCliques, topology)
		}
	}

	if bestCliqueID == "" {
		return "", fmt.Errorf("no suitable clique found for GPUs: %v", gpuUUIDs)
	}

	klog.V(4).Infof("Selected optimal clique ID %s for GPUs %v", bestCliqueID, gpuUUIDs)
	return bestCliqueID, nil
}

// chooseBestCliqueByConnectivity chooses the best clique based on connectivity.
func (s *DeviceState) chooseBestCliqueByConnectivity(cliqueIDs []string, topology *FabricTopology) string {
	if len(cliqueIDs) == 0 {
		return ""
	}

	if len(cliqueIDs) == 1 {
		return cliqueIDs[0]
	}

	bestCliqueID := cliqueIDs[0]
	bestScore := float64(-1)

	for _, cliqueID := range cliqueIDs {
		clique := topology.Cliques[cliqueID]
		if clique == nil {
			continue
		}

		// Score based on bandwidth and connectivity
		score := float64(clique.Bandwidth) / float64(clique.Latency+1) // Avoid division by zero
		if score > bestScore {
			bestScore = score
			bestCliqueID = cliqueID
		}
	}

	klog.V(4).Infof("Selected best clique %s with score %.2f", bestCliqueID, bestScore)
	return bestCliqueID
}

// selectOptimalGpuCombination is unused - functionality moved to fabric_selection.go
// This function has been replaced by selectOptimalGpuCombinationWithFallback in fabric_fallback.go

// isGpuAvailable checks if a GPU is available for allocation.
func (s *DeviceState) isGpuAvailable(uuid string) bool {
	device, exists := s.allocatable[uuid]
	if !exists {
		return false
	}

	// Check if GPU is not already allocated
	// This is a simplified check - in reality, we'd need to check allocation state
	return device.Type() == GpuDeviceType
}

// isFabricManagerCapableFromFabricInfo checks if a GPU supports FabricManager.
func (s *DeviceState) isFabricManagerCapableFromFabricInfo(gpuInfo *FabricGPUInfo) bool {
	// Check if GPU has fabric capability
	return gpuInfo.FabricState == 1 // GPU_FABRIC_STATE_COMPLETED
}

// shouldOptimizeGpuSelection determines if GPU selection should be optimized.
func (s *DeviceState) shouldOptimizeGpuSelection(results []*resourceapi.DeviceRequestAllocationResult) bool {
	// Check if fabric optimization is enabled
	if !featuregates.Enabled(featuregates.FabricAwareOptimization) {
		return false
	}

	// Only optimize if we have multiple GPUs
	return len(results) >= 2
}

// updateResultsWithOptimalGPUs updates the allocation results with optimal GPU selection.
func (s *DeviceState) updateResultsWithOptimalGPUs(results []*resourceapi.DeviceRequestAllocationResult, optimalGPUs []string) []*resourceapi.DeviceRequestAllocationResult {
	if len(results) != len(optimalGPUs) {
		klog.Warningf("Mismatch between results count (%d) and optimal GPUs count (%d)", len(results), len(optimalGPUs))
		return results
	}

	// Create new results with optimal GPUs
	newResults := make([]*resourceapi.DeviceRequestAllocationResult, len(results))
	for i, result := range results {
		newResults[i] = &resourceapi.DeviceRequestAllocationResult{
			Driver:  result.Driver,
			Device:  optimalGPUs[i],
			Pool:    result.Pool,
			Request: result.Request,
		}
	}

	return newResults
}
