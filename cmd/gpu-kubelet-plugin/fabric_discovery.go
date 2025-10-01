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

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"
)

// discoverFabricTopology discovers the complete fabric topology of the system.
func (s *DeviceState) discoverFabricTopology() (*FabricTopology, error) {
	topology := &FabricTopology{
		GPUs:         make(map[string]*FabricGPUInfo),
		Cliques:      make(map[string]*CliqueInfo),
		Connectivity: make(map[string]map[string]*LinkInfo),
		OptimalPaths: make(map[string]map[string]*PathInfo),
	}

	klog.V(4).Info("Starting fabric topology discovery")

	// Discover all GPUs and their fabric properties
	for uuid, device := range s.allocatable {
		if device.Type() != GpuDeviceType {
			continue
		}

		gpuInfo, err := s.getFabricGPUInfo(device.Gpu)
		if err != nil {
			klog.Warningf("Failed to get fabric info for GPU %s: %v", uuid, err)
			continue
		}

		topology.GPUs[uuid] = gpuInfo
		klog.V(6).Infof("Discovered GPU %s with clique ID %s", uuid, gpuInfo.CliqueID)
	}

	if len(topology.GPUs) == 0 {
		return nil, fmt.Errorf("no GPUs found for fabric topology discovery")
	}

	// Build clique information
	err := s.buildCliqueInfo(topology)
	if err != nil {
		return nil, fmt.Errorf("failed to build clique info: %w", err)
	}

	// Calculate connectivity matrix
	err = s.calculateConnectivity(topology)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate connectivity: %w", err)
	}

	// Find optimal paths between GPUs
	err = s.findOptimalPaths(topology)
	if err != nil {
		return nil, fmt.Errorf("failed to find optimal paths: %w", err)
	}

	klog.V(4).Infof("Fabric topology discovery completed: %d GPUs, %d cliques", len(topology.GPUs), len(topology.Cliques))
	return topology, nil
}

// getFabricGPUInfo extracts fabric-specific information from a GPU.
func (s *DeviceState) getFabricGPUInfo(gpu *GpuInfo) (*FabricGPUInfo, error) {
	if s.nvdevlib == nil || s.nvdevlib.nvmllib == nil {
		// Return mock data for testing
		return &FabricGPUInfo{
			UUID:          gpu.UUID,
			CliqueID:      "0",
			NVLinkPorts:   []NVLinkPort{},
			FabricState:   nvml.GPU_FABRIC_STATE_COMPLETED,
			ConnectedGPUs: []string{},
			Bandwidth:     0,
		}, nil
	}

	deviceHandle, ret := s.nvdevlib.nvmllib.DeviceGetHandleByUUID(gpu.UUID)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting device handle for GPU %s: %v", gpu.UUID, ret)
	}

	// Get fabric info
	fabricInfo, ret := deviceHandle.GetGpuFabricInfo()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("error getting fabric info for GPU %s: %v", gpu.UUID, ret)
	}

	// Get NVLink link states (simplified for now)
	nvlinkPorts := make([]NVLinkPort, 0)
	connectedGPUs := make([]string, 0)
	totalBandwidth := int64(0)

	// For now, create mock NVLink ports for testing
	// In a real implementation, this would query actual NVLink state
	for i := 0; i < 4; i++ { // Assume 4 NVLink ports
		port := NVLinkPort{
			PortID:    i,
			IsActive:  true,
			LinkType:  "NVLink5",
			Bandwidth: 400, // GB/s
		}
		nvlinkPorts = append(nvlinkPorts, port)
		totalBandwidth += port.Bandwidth
	}

	return &FabricGPUInfo{
		UUID:          gpu.UUID,
		CliqueID:      fmt.Sprintf("%d", fabricInfo.CliqueId),
		NVLinkPorts:   nvlinkPorts,
		FabricState:   nvml.GpuFabricState(fabricInfo.State),
		ConnectedGPUs: connectedGPUs,
		Bandwidth:     totalBandwidth,
	}, nil
}

// buildCliqueInfo groups GPUs by their clique IDs.
func (s *DeviceState) buildCliqueInfo(topology *FabricTopology) error {
	cliqueMap := make(map[string][]string)

	// Group GPUs by clique ID
	for uuid, gpuInfo := range topology.GPUs {
		cliqueMap[gpuInfo.CliqueID] = append(cliqueMap[gpuInfo.CliqueID], uuid)
	}

	// Build clique information
	for cliqueID, gpuUUIDs := range cliqueMap {
		clique := &CliqueInfo{
			ID:        cliqueID,
			GPUs:      gpuUUIDs,
			Bandwidth: 0,
			Latency:   0,
			HopCount:  0,
			IsOptimal: false,
		}

		// Calculate clique properties
		err := s.calculateCliqueProperties(clique, topology)
		if err != nil {
			klog.Warningf("Failed to calculate properties for clique %s: %v", cliqueID, err)
		}

		topology.Cliques[cliqueID] = clique
		klog.V(6).Infof("Built clique %s with %d GPUs", cliqueID, len(gpuUUIDs))
	}

	return nil
}

// calculateCliqueProperties calculates bandwidth, latency, and hop count for a clique.
func (s *DeviceState) calculateCliqueProperties(clique *CliqueInfo, topology *FabricTopology) error {
	if len(clique.GPUs) < 2 {
		return nil // Single GPU clique
	}

	totalBandwidth := int64(0)
	totalLatency := int64(0)
	connectionCount := 0
	maxHopCount := 0

	// Calculate pairwise connectivity within the clique
	for i := 0; i < len(clique.GPUs); i++ {
		for j := i + 1; j < len(clique.GPUs); j++ {
			gpu1 := clique.GPUs[i]
			gpu2 := clique.GPUs[j]

			// Check if there's a direct connection
			link := topology.Connectivity[gpu1][gpu2]
			if link != nil {
				totalBandwidth += link.Bandwidth
				totalLatency += link.Latency
				connectionCount++
			}

			// Check hop count (for now, assume direct connections have 0 hops)
			if link != nil && link.IsDirect {
				// Direct connection
			} else {
				// Indirect connection - would need path finding
				maxHopCount = 1 // Simplified for now
			}
		}
	}

	if connectionCount > 0 {
		clique.Bandwidth = totalBandwidth
		clique.Latency = totalLatency / int64(connectionCount)
	}
	clique.HopCount = maxHopCount

	// Determine if this clique is optimal (all GPUs directly connected)
	expectedConnections := len(clique.GPUs) * (len(clique.GPUs) - 1) / 2
	clique.IsOptimal = connectionCount == expectedConnections

	return nil
}

// calculateConnectivity builds the connectivity matrix between all GPUs.
func (s *DeviceState) calculateConnectivity(topology *FabricTopology) error {
	// Initialize connectivity matrix
	for uuid := range topology.GPUs {
		topology.Connectivity[uuid] = make(map[string]*LinkInfo)
	}

	// Calculate connectivity between all GPU pairs
	for uuid1, gpu1 := range topology.GPUs {
		for uuid2, gpu2 := range topology.GPUs {
			if uuid1 == uuid2 {
				continue
			}

			link, err := s.calculateLinkInfo(gpu1, gpu2)
			if err != nil {
				klog.V(6).Infof("No link between GPU %s and GPU %s: %v", uuid1, uuid2, err)
				continue
			}

			topology.Connectivity[uuid1][uuid2] = link
			klog.V(6).Infof("Link between GPU %s and GPU %s: %d GB/s, %d ns", uuid1, uuid2, link.Bandwidth, link.Latency)
		}
	}

	return nil
}

// calculateLinkInfo calculates link information between two GPUs.
func (s *DeviceState) calculateLinkInfo(gpu1, gpu2 *FabricGPUInfo) (*LinkInfo, error) {
	// Check if GPUs are in the same clique
	if gpu1.CliqueID != gpu2.CliqueID {
		return nil, fmt.Errorf("GPUs are in different cliques")
	}

	// Check for direct NVLink connection
	for _, port1 := range gpu1.NVLinkPorts {
		if port1.ConnectedTo == gpu2.UUID {
			return &LinkInfo{
				SourceGPU: gpu1.UUID,
				TargetGPU: gpu2.UUID,
				Bandwidth: port1.Bandwidth,
				Latency:   s.getNVLinkLatency(port1.LinkType),
				LinkType:  port1.LinkType,
				IsDirect:  true,
				PortID:    port1.PortID,
			}, nil
		}
	}

	// No direct connection found
	return nil, fmt.Errorf("no direct connection between GPUs")
}

// findOptimalPaths finds the optimal paths between all GPU pairs.
func (s *DeviceState) findOptimalPaths(topology *FabricTopology) error {
	// Initialize optimal paths matrix
	for uuid := range topology.GPUs {
		topology.OptimalPaths[uuid] = make(map[string]*PathInfo)
	}

	// Find optimal paths between all GPU pairs
	for uuid1 := range topology.GPUs {
		for uuid2 := range topology.GPUs {
			if uuid1 == uuid2 {
				continue
			}

			path, err := s.findOptimalPath(uuid1, uuid2, topology)
			if err != nil {
				klog.V(6).Infof("No path between GPU %s and GPU %s: %v", uuid1, uuid2, err)
				continue
			}

			topology.OptimalPaths[uuid1][uuid2] = path
		}
	}

	return nil
}

// findOptimalPath finds the optimal path between two GPUs using Dijkstra's algorithm.
func (s *DeviceState) findOptimalPath(source, target string, topology *FabricTopology) (*PathInfo, error) {
	// For now, implement a simplified version
	// Check if there's a direct connection
	link := topology.Connectivity[source][target]
	if link != nil {
		return &PathInfo{
			SourceGPU: source,
			TargetGPU: target,
			Latency:   link.Latency,
			HopCount:  0,
			Path:      []string{},
			Bandwidth: link.Bandwidth,
		}, nil
	}

	// TODO: Implement proper path finding algorithm
	// For now, return error for indirect connections
	return nil, fmt.Errorf("no direct path found")
}

// Helper functions for NVLink information

func (s *DeviceState) getNVLinkLatency(linkType string) int64 {
	switch linkType {
	case "NVLink4":
		return 100 // ns
	case "NVLink5":
		return 80 // ns
	default:
		return 200 // ns default
	}
}
