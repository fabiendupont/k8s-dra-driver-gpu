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

	"k8s.io/klog/v2"

	cdispec "tags.cncf.io/container-device-interface/specs-go"

	configapi "github.com/NVIDIA/k8s-dra-driver-gpu/api/nvidia.com/resource/v1beta1"
	"github.com/NVIDIA/k8s-dra-driver-gpu/internal/fabricmanager"
)

// FabricTopologyHandler defines the interface for virtualization model-specific fabric handling.
type FabricTopologyHandler interface {
	// AllocateFabricResources allocates fabric resources for the given GPUs
	AllocateFabricResources(ctx context.Context, gpuUUIDs []string, config *configapi.FabricTopologyConfig, fmManager *FabricManagerManager) (*configapi.FabricResourceAllocation, error)

	// GenerateCDISpec generates CDI device specification for the allocated resources
	GenerateCDISpec(allocated *configapi.FabricResourceAllocation) *cdispec.Device
}

// BareMetalHandler handles fabric topology for bare metal deployments.
type BareMetalHandler struct{}

func (h *BareMetalHandler) AllocateFabricResources(ctx context.Context, gpuUUIDs []string, config *configapi.FabricTopologyConfig, fmManager *FabricManagerManager) (*configapi.FabricResourceAllocation, error) {
	if fmManager == nil {
		return nil, fmt.Errorf("FabricManager not available")
	}

	// Find optimal partition for bare metal
	partition, err := fmManager.FindOptimalPartition(ctx, gpuUUIDs, config)
	if err != nil {
		return nil, fmt.Errorf("failed to find optimal partition: %w", err)
	}

	if partition == nil {
		klog.V(4).Info("No suitable fabric partition found for bare metal allocation")
		return &configapi.FabricResourceAllocation{
			VirtualizationModel: configapi.FabricVirtualizationBareMetal,
		}, nil
	}

	// Activate the partition
	if err := fmManager.ActivatePartition(ctx, partition.ID); err != nil {
		return nil, fmt.Errorf("failed to activate partition %s: %w", partition.ID, err)
	}

	// Get topology info for CDI generation
	topology := fmManager.GetTopology()
	var topologyInfo *configapi.FabricTopologyInfo
	if topology != nil {
		topologyInfo = h.convertTopologyInfo(topology)
	}

	// Convert to our partition info format
	partitionInfo := &configapi.FabricPartitionInfo{
		ID:        partition.ID,
		GPUs:      partition.GPUs,
		Bandwidth: h.calculatePartitionBandwidth(partition, topology),
		Latency:   h.calculatePartitionLatency(partition, topology),
	}

	return &configapi.FabricResourceAllocation{
		VirtualizationModel: configapi.FabricVirtualizationBareMetal,
		AllocatedPartition:  partitionInfo,
		FabricTopologyInfo:  topologyInfo,
	}, nil
}

func (h *BareMetalHandler) GenerateCDISpec(allocated *configapi.FabricResourceAllocation) *cdispec.Device {
	if allocated == nil {
		return nil
	}

	device := &cdispec.Device{
		ContainerEdits: cdispec.ContainerEdits{
			Env: []string{
				fmt.Sprintf("NVIDIA_FABRIC_VIRTUALIZATION_MODEL=%s", allocated.VirtualizationModel),
			},
		},
	}

	// Add partition information
	if allocated.AllocatedPartition != nil {
		device.ContainerEdits.Env = append(device.ContainerEdits.Env,
			fmt.Sprintf("NVIDIA_FABRIC_PARTITION_ID=%s", allocated.AllocatedPartition.ID),
			fmt.Sprintf("NVIDIA_FABRIC_PARTITION_GPUS=%s", fmt.Sprintf("%v", allocated.AllocatedPartition.GPUs)),
			fmt.Sprintf("NVIDIA_FABRIC_PARTITION_BANDWIDTH=%.2f", allocated.AllocatedPartition.Bandwidth),
			fmt.Sprintf("NVIDIA_FABRIC_PARTITION_LATENCY=%.3f", allocated.AllocatedPartition.Latency),
		)
	}

	// Add topology information for guest consumption
	if allocated.FabricTopologyInfo != nil {
		device.ContainerEdits.Env = append(device.ContainerEdits.Env,
			fmt.Sprintf("NVIDIA_FABRIC_TOTAL_BANDWIDTH=%.2f", allocated.FabricTopologyInfo.TotalBandwidth),
			fmt.Sprintf("NVIDIA_FABRIC_TOTAL_LATENCY=%.3f", allocated.FabricTopologyInfo.TotalLatency),
		)
	}

	return device
}

// FullPassthroughHandler handles fabric topology for full passthrough virtualization.
type FullPassthroughHandler struct{}

func (h *FullPassthroughHandler) AllocateFabricResources(ctx context.Context, gpuUUIDs []string, config *configapi.FabricTopologyConfig, fmManager *FabricManagerManager) (*configapi.FabricResourceAllocation, error) {
	if fmManager == nil {
		return nil, fmt.Errorf("FabricManager not available")
	}

	// Get fabric topology information for guest VM consumption
	topology := fmManager.GetTopology()
	if topology == nil {
		return nil, fmt.Errorf("failed to get fabric topology")
	}

	// Convert topology to our format
	topologyInfo := h.convertTopologyInfo(topology)

	// Find available partitions that contain the requested GPUs
	var availablePartitions []configapi.FabricPartitionInfo
	for _, partition := range topology.Partitions {
		if h.containsAllGPUs(partition.GPUs, gpuUUIDs) {
			availablePartitions = append(availablePartitions, configapi.FabricPartitionInfo{
				ID:        partition.ID,
				GPUs:      partition.GPUs,
				Bandwidth: h.calculatePartitionBandwidth(&partition, topology),
				Latency:   h.calculatePartitionLatency(&partition, topology),
			})
		}
	}

	// Extract NVSwitch and NVLink resources
	var nvSwitchDevices []string
	var nvLinkChannels []string
	for _, interconnect := range topology.Interconnects {
		switch interconnect.Type {
		case "NVSwitch":
			nvSwitchDevices = append(nvSwitchDevices, interconnect.ID)
		case "NVLink":
			nvLinkChannels = append(nvLinkChannels, interconnect.ID)
		}
	}

	return &configapi.FabricResourceAllocation{
		VirtualizationModel: configapi.FabricVirtualizationFullPassthrough,
		NVSwitchDevices:     nvSwitchDevices,
		NVLinkChannels:      nvLinkChannels,
		AvailablePartitions: availablePartitions,
		FabricTopologyInfo:  topologyInfo,
	}, nil
}

func (h *FullPassthroughHandler) GenerateCDISpec(allocated *configapi.FabricResourceAllocation) *cdispec.Device {
	if allocated == nil {
		return nil
	}

	device := &cdispec.Device{
		ContainerEdits: cdispec.ContainerEdits{
			Env: []string{
				fmt.Sprintf("NVIDIA_FABRIC_VIRTUALIZATION_MODEL=%s", allocated.VirtualizationModel),
			},
		},
	}

	// Add fabric topology information for guest VM
	if allocated.FabricTopologyInfo != nil {
		device.ContainerEdits.Env = append(device.ContainerEdits.Env,
			fmt.Sprintf("NVIDIA_FABRIC_TOTAL_BANDWIDTH=%.2f", allocated.FabricTopologyInfo.TotalBandwidth),
			fmt.Sprintf("NVIDIA_FABRIC_TOTAL_LATENCY=%.3f", allocated.FabricTopologyInfo.TotalLatency),
		)
	}

	// Add available partitions information
	if len(allocated.AvailablePartitions) > 0 {
		device.ContainerEdits.Env = append(device.ContainerEdits.Env,
			fmt.Sprintf("NVIDIA_FABRIC_AVAILABLE_PARTITIONS=%d", len(allocated.AvailablePartitions)),
		)
	}

	// Add NVSwitch and NVLink resource information
	if len(allocated.NVSwitchDevices) > 0 {
		device.ContainerEdits.Env = append(device.ContainerEdits.Env,
			fmt.Sprintf("NVIDIA_FABRIC_NVSWITCH_DEVICES=%s", fmt.Sprintf("%v", allocated.NVSwitchDevices)),
		)
	}

	if len(allocated.NVLinkChannels) > 0 {
		device.ContainerEdits.Env = append(device.ContainerEdits.Env,
			fmt.Sprintf("NVIDIA_FABRIC_NVLINK_CHANNELS=%s", fmt.Sprintf("%v", allocated.NVLinkChannels)),
		)
	}

	return device
}

// SharedNVSwitchHandler handles fabric topology for shared NVSwitch virtualization.
type SharedNVSwitchHandler struct{}

func (h *SharedNVSwitchHandler) AllocateFabricResources(ctx context.Context, gpuUUIDs []string, config *configapi.FabricTopologyConfig, fmManager *FabricManagerManager) (*configapi.FabricResourceAllocation, error) {
	if fmManager == nil {
		return nil, fmt.Errorf("FabricManager not available")
	}

	// DRA driver acts as service VM - find and allocate partition
	partition, err := fmManager.FindOptimalPartition(ctx, gpuUUIDs, config)
	if err != nil {
		return nil, fmt.Errorf("failed to find optimal partition: %w", err)
	}

	if partition == nil {
		klog.V(4).Info("No suitable fabric partition found for shared NVSwitch allocation")
		return &configapi.FabricResourceAllocation{
			VirtualizationModel: configapi.FabricVirtualizationSharedNVSwitch,
		}, nil
	}

	// Activate the partition (DRA driver manages it)
	if err := fmManager.ActivatePartition(ctx, partition.ID); err != nil {
		return nil, fmt.Errorf("failed to activate partition %s: %w", partition.ID, err)
	}

	// Convert to our partition info format
	partitionInfo := &configapi.FabricPartitionInfo{
		ID:        partition.ID,
		GPUs:      partition.GPUs,
		Bandwidth: h.calculatePartitionBandwidth(partition, nil), // No topology available for SharedNVSwitch
		Latency:   h.calculatePartitionLatency(partition, nil),
	}

	return &configapi.FabricResourceAllocation{
		VirtualizationModel: configapi.FabricVirtualizationSharedNVSwitch,
		AllocatedPartition:  partitionInfo,
	}, nil
}

func (h *SharedNVSwitchHandler) GenerateCDISpec(allocated *configapi.FabricResourceAllocation) *cdispec.Device {
	if allocated == nil {
		return nil
	}

	device := &cdispec.Device{
		ContainerEdits: cdispec.ContainerEdits{
			Env: []string{
				fmt.Sprintf("NVIDIA_FABRIC_VIRTUALIZATION_MODEL=%s", allocated.VirtualizationModel),
			},
		},
	}

	// Add assigned partition information (guest can't modify)
	if allocated.AllocatedPartition != nil {
		device.ContainerEdits.Env = append(device.ContainerEdits.Env,
			fmt.Sprintf("NVIDIA_FABRIC_ASSIGNED_PARTITION_ID=%s", allocated.AllocatedPartition.ID),
			fmt.Sprintf("NVIDIA_FABRIC_ASSIGNED_PARTITION_GPUS=%s", fmt.Sprintf("%v", allocated.AllocatedPartition.GPUs)),
			fmt.Sprintf("NVIDIA_FABRIC_ASSIGNED_PARTITION_BANDWIDTH=%.2f", allocated.AllocatedPartition.Bandwidth),
			fmt.Sprintf("NVIDIA_FABRIC_ASSIGNED_PARTITION_LATENCY=%.3f", allocated.AllocatedPartition.Latency),
		)
	}

	return device
}

// VGPUHandler handles fabric topology for vGPU virtualization (no fabric support).
type VGPUHandler struct{}

func (h *VGPUHandler) AllocateFabricResources(ctx context.Context, gpuUUIDs []string, config *configapi.FabricTopologyConfig, fmManager *FabricManagerManager) (*configapi.FabricResourceAllocation, error) {
	// vGPU does not support fabric topology optimization
	klog.V(4).Info("vGPU virtualization model selected - fabric topology optimization not available")

	return &configapi.FabricResourceAllocation{
		VirtualizationModel: configapi.FabricVirtualizationVGPU,
	}, nil
}

func (h *VGPUHandler) GenerateCDISpec(allocated *configapi.FabricResourceAllocation) *cdispec.Device {
	if allocated == nil {
		return nil
	}

	// vGPU gets minimal fabric information
	device := &cdispec.Device{
		ContainerEdits: cdispec.ContainerEdits{
			Env: []string{
				fmt.Sprintf("NVIDIA_FABRIC_VIRTUALIZATION_MODEL=%s", allocated.VirtualizationModel),
				"NVIDIA_FABRIC_TOPOLOGY_ENABLED=false",
			},
		},
	}

	return device
}

// Helper functions

func (h *BareMetalHandler) convertTopologyInfo(topology *fabricmanager.FabricTopology) *configapi.FabricTopologyInfo {
	if topology == nil {
		return nil
	}

	var partitions []configapi.FabricPartitionInfo
	for _, partition := range topology.Partitions {
		partitions = append(partitions, configapi.FabricPartitionInfo{
			ID:        partition.ID,
			GPUs:      partition.GPUs,
			Bandwidth: h.calculatePartitionBandwidth(&partition, topology),
			Latency:   h.calculatePartitionLatency(&partition, topology),
		})
	}

	var interconnects []configapi.FabricInterconnectInfo
	for _, interconnect := range topology.Interconnects {
		interconnects = append(interconnects, configapi.FabricInterconnectInfo{
			ID:        interconnect.ID,
			Type:      interconnect.Type,
			Bandwidth: interconnect.Bandwidth,
			Latency:   interconnect.Latency,
		})
	}

	// Calculate total bandwidth and latency
	totalBandwidth := 0.0
	totalLatency := 0.0
	for _, partition := range partitions {
		totalBandwidth += partition.Bandwidth
		totalLatency += partition.Latency
	}
	if len(partitions) > 0 {
		totalLatency /= float64(len(partitions)) // Average latency
	}

	return &configapi.FabricTopologyInfo{
		Partitions:     partitions,
		Interconnects:  interconnects,
		TotalBandwidth: totalBandwidth,
		TotalLatency:   totalLatency,
	}
}

func (h *FullPassthroughHandler) convertTopologyInfo(topology *fabricmanager.FabricTopology) *configapi.FabricTopologyInfo {
	// Same conversion logic as BareMetalHandler
	bareMetalHandler := &BareMetalHandler{}
	return bareMetalHandler.convertTopologyInfo(topology)
}

func (h *FullPassthroughHandler) containsAllGPUs(partitionGPUs []string, requestedGPUs []string) bool {
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

// calculatePartitionBandwidth calculates the bandwidth for a partition.
func (h *BareMetalHandler) calculatePartitionBandwidth(partition *fabricmanager.FabricPartition, topology *fabricmanager.FabricTopology) float64 {
	if partition == nil {
		return 0.0
	}

	// If partition has topology info, use it
	if partition.Topology != nil {
		return partition.Topology.MaxBandwidth
	}

	// Fallback: calculate from topology bandwidth map
	if topology != nil && topology.Bandwidth != nil {
		// Sum up bandwidth for all GPUs in this partition
		totalBandwidth := 0.0
		for _, gpu := range partition.GPUs {
			if bw, exists := topology.Bandwidth[gpu]; exists {
				totalBandwidth += bw
			}
		}
		return totalBandwidth
	}

	// Default fallback
	return 100.0 // GB/s
}

// calculatePartitionLatency calculates the latency for a partition.
func (h *BareMetalHandler) calculatePartitionLatency(partition *fabricmanager.FabricPartition, topology *fabricmanager.FabricTopology) float64 {
	if partition == nil {
		return 0.0
	}

	// If partition has topology info, use it
	if partition.Topology != nil {
		return partition.Topology.MinLatency
	}

	// Fallback: calculate from topology latency map
	if topology != nil && topology.Latency != nil {
		// Average latency for all GPUs in this partition
		totalLatency := 0.0
		count := 0
		for _, gpu := range partition.GPUs {
			if lat, exists := topology.Latency[gpu]; exists {
				totalLatency += lat
				count++
			}
		}
		if count > 0 {
			return totalLatency / float64(count)
		}
	}

	// Default fallback
	return 0.1 // ms
}

// calculatePartitionBandwidth calculates the bandwidth for a partition (FullPassthroughHandler).
func (h *FullPassthroughHandler) calculatePartitionBandwidth(partition *fabricmanager.FabricPartition, topology *fabricmanager.FabricTopology) float64 {
	// Same logic as BareMetalHandler
	bareMetalHandler := &BareMetalHandler{}
	return bareMetalHandler.calculatePartitionBandwidth(partition, topology)
}

// calculatePartitionLatency calculates the latency for a partition (FullPassthroughHandler).
func (h *FullPassthroughHandler) calculatePartitionLatency(partition *fabricmanager.FabricPartition, topology *fabricmanager.FabricTopology) float64 {
	// Same logic as BareMetalHandler
	bareMetalHandler := &BareMetalHandler{}
	return bareMetalHandler.calculatePartitionLatency(partition, topology)
}

// calculatePartitionBandwidth calculates the bandwidth for a partition (SharedNVSwitchHandler).
func (h *SharedNVSwitchHandler) calculatePartitionBandwidth(partition *fabricmanager.FabricPartition, topology *fabricmanager.FabricTopology) float64 {
	// Same logic as BareMetalHandler
	bareMetalHandler := &BareMetalHandler{}
	return bareMetalHandler.calculatePartitionBandwidth(partition, topology)
}

// calculatePartitionLatency calculates the latency for a partition (SharedNVSwitchHandler).
func (h *SharedNVSwitchHandler) calculatePartitionLatency(partition *fabricmanager.FabricPartition, topology *fabricmanager.FabricTopology) float64 {
	// Same logic as BareMetalHandler
	bareMetalHandler := &BareMetalHandler{}
	return bareMetalHandler.calculatePartitionLatency(partition, topology)
}

// GetFabricHandler returns the appropriate handler for the virtualization model.
func GetFabricHandler(virtualizationModel string) FabricTopologyHandler {
	switch virtualizationModel {
	case configapi.FabricVirtualizationBareMetal:
		return &BareMetalHandler{}
	case configapi.FabricVirtualizationFullPassthrough:
		return &FullPassthroughHandler{}
	case configapi.FabricVirtualizationSharedNVSwitch:
		return &SharedNVSwitchHandler{}
	case configapi.FabricVirtualizationVGPU:
		return &VGPUHandler{}
	default:
		// Default to BareMetal for unknown models
		return &BareMetalHandler{}
	}
}
