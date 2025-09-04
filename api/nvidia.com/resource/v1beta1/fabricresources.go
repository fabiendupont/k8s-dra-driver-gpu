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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FabricResourceAllocation represents allocated fabric resources for a resource claim.
type FabricResourceAllocation struct {
	// NVSwitchDevices contains the allocated NVSwitch device IDs
	NVSwitchDevices []string `json:"nvSwitchDevices,omitempty"`

	// NVLinkChannels contains the allocated NVLink channel IDs
	NVLinkChannels []string `json:"nvLinkChannels,omitempty"`

	// AvailablePartitions contains the fabric partitions available to the allocation
	AvailablePartitions []FabricPartitionInfo `json:"availablePartitions,omitempty"`

	// AllocatedPartition contains the specific partition assigned to this allocation
	AllocatedPartition *FabricPartitionInfo `json:"allocatedPartition,omitempty"`

	// VirtualizationModel indicates how fabric resources should be managed
	VirtualizationModel string `json:"virtualizationModel"`

	// FabricTopologyInfo contains topology information for guest consumption.
	FabricTopologyInfo *FabricTopologyInfo `json:"fabricTopologyInfo,omitempty"`
}

// FabricPartitionInfo represents information about a fabric partition.
type FabricPartitionInfo struct {
	// ID is the unique identifier for the partition
	ID string `json:"id"`

	// GPUs contains the GPU UUIDs in this partition
	GPUs []string `json:"gpus"`

	// Bandwidth is the available bandwidth in GB/s
	Bandwidth float64 `json:"bandwidth"`

	// Latency is the latency in milliseconds
	Latency float64 `json:"latency"`

	// Interconnects contains the NVLink interconnects in this partition
	Interconnects []FabricInterconnectInfo `json:"interconnects,omitempty"`
}

// FabricInterconnectInfo represents information about a fabric interconnect.
type FabricInterconnectInfo struct {
	// ID is the unique identifier for the interconnect
	ID string `json:"id"`

	// Type is the type of interconnect (e.g., "NVLink")
	Type string `json:"type"`

	// Bandwidth is the bandwidth in GB/s
	Bandwidth float64 `json:"bandwidth"`

	// Latency is the latency in milliseconds
	Latency float64 `json:"latency"`
}

// FabricTopologyInfo contains topology information for guest consumption.
type FabricTopologyInfo struct {
	// Partitions contains all available partitions
	Partitions []FabricPartitionInfo `json:"partitions"`

	// Interconnects contains all interconnects
	Interconnects []FabricInterconnectInfo `json:"interconnects"`

	// TotalBandwidth is the total fabric bandwidth in GB/s
	TotalBandwidth float64 `json:"totalBandwidth"`

	// TotalLatency is the average fabric latency in milliseconds
	TotalLatency float64 `json:"totalLatency"`
}

// FabricResourceAllocationStatus represents the status of fabric resource allocation.
type FabricResourceAllocationStatus struct {
	// Phase indicates the current phase of the allocation
	Phase FabricAllocationPhase `json:"phase"`

	// Message provides additional information about the allocation
	Message string `json:"message,omitempty"`

	// LastTransitionTime is when the status last changed
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`
}

// FabricAllocationPhase represents the phase of fabric resource allocation.
type FabricAllocationPhase string

const (
	// FabricAllocationPending indicates the allocation is pending.
	FabricAllocationPending FabricAllocationPhase = "Pending"

	// FabricAllocationAllocated indicates resources have been allocated.
	FabricAllocationAllocated FabricAllocationPhase = "Allocated"

	// FabricAllocationFailed indicates the allocation failed.
	FabricAllocationFailed FabricAllocationPhase = "Failed"

	// FabricAllocationReleased indicates resources have been released.
	FabricAllocationReleased FabricAllocationPhase = "Released"
)

// IsValid checks if the fabric resource allocation is valid.
func (f *FabricResourceAllocation) IsValid() bool {
	if f == nil {
		return false
	}

	// Must have a virtualization model
	if f.VirtualizationModel == "" {
		return false
	}

	// For SharedNVSwitch, must have an allocated partition
	if f.VirtualizationModel == FabricVirtualizationSharedNVSwitch {
		return f.AllocatedPartition != nil
	}

	// For FullPassthrough, must have fabric topology info
	if f.VirtualizationModel == FabricVirtualizationFullPassthrough {
		return f.FabricTopologyInfo != nil
	}

	// For BareMetal, must have either allocated partition or fabric topology info
	if f.VirtualizationModel == FabricVirtualizationBareMetal {
		return f.AllocatedPartition != nil || f.FabricTopologyInfo != nil
	}

	// For VGPU, no fabric resources should be allocated
	if f.VirtualizationModel == FabricVirtualizationVGPU {
		return len(f.NVSwitchDevices) == 0 && len(f.NVLinkChannels) == 0 &&
			len(f.AvailablePartitions) == 0 && f.AllocatedPartition == nil
	}

	return true
}

// GetPartitionByID finds a partition by its ID.
func (f *FabricResourceAllocation) GetPartitionByID(id string) *FabricPartitionInfo {
	for _, partition := range f.AvailablePartitions {
		if partition.ID == id {
			return &partition
		}
	}
	return nil
}

// GetOptimalPartition finds the optimal partition based on requirements.
func (f *FabricResourceAllocation) GetOptimalPartition(gpuUUIDs []string, minBandwidth, maxLatency float64) *FabricPartitionInfo {
	var bestPartition *FabricPartitionInfo
	var bestScore float64 = -1

	for _, partition := range f.AvailablePartitions {
		// Check if partition contains all requested GPUs
		if !f.containsAllGPUs(partition.GPUs, gpuUUIDs) {
			continue
		}

		// Check bandwidth requirement
		if minBandwidth > 0 && partition.Bandwidth < minBandwidth {
			continue
		}

		// Check latency requirement
		if maxLatency > 0 && partition.Latency > maxLatency {
			continue
		}

		// Calculate score (bandwidth / latency ratio)
		score := partition.Bandwidth / (partition.Latency + 0.001) // Add small value to avoid division by zero
		if score > bestScore {
			bestScore = score
			bestPartition = &partition
		}
	}

	return bestPartition
}

// containsAllGPUs checks if the partition contains all requested GPU UUIDs.
func (f *FabricResourceAllocation) containsAllGPUs(partitionGPUs []string, requestedGPUs []string) bool {
	if len(requestedGPUs) == 0 {
		return true
	}

	partitionGPUSet := make(map[string]bool)
	for _, gpu := range partitionGPUs {
		partitionGPUSet[gpu] = true
	}

	for _, requestedGPU := range requestedGPUs {
		if !partitionGPUSet[requestedGPU] {
			return false
		}
	}

	return true
}
