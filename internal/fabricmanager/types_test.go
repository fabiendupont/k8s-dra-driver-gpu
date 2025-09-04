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

package fabricmanager

import (
	"testing"
	"time"
)

func TestDefaultFabricManagerConfig(t *testing.T) {
	config := DefaultFabricManagerConfig()

	if config == nil {
		t.Fatal("DefaultFabricManagerConfig returned nil")
	}

	if config.Endpoint != "localhost:50051" {
		t.Errorf("Expected endpoint 'localhost:50051', got '%s'", config.Endpoint)
	}

	if config.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", config.Timeout)
	}

	if config.RetryCount != 3 {
		t.Errorf("Expected retry count 3, got %d", config.RetryCount)
	}

	if config.RetryDelay != 1*time.Second {
		t.Errorf("Expected retry delay 1s, got %v", config.RetryDelay)
	}
}

func TestFabricTopology(t *testing.T) {
	topology := &FabricTopology{
		Partitions: []FabricPartition{
			{
				ID:   "partition-1",
				Name: "High Bandwidth Partition",
				GPUs: []string{"gpu-1", "gpu-2"},
			},
		},
		Interconnects: []Interconnect{
			{
				ID:     "nvlink-1",
				Type:   "NVLink",
				Status: "active",
			},
		},
		Bandwidth: map[string]float64{
			"gpu-1-gpu-2": 600.0,
		},
		Latency: map[string]float64{
			"gpu-1-gpu-2": 0.1,
		},
		Timestamp: time.Now(),
		Source:    "fabricmanager",
	}

	if len(topology.Partitions) != 1 {
		t.Errorf("Expected 1 partition, got %d", len(topology.Partitions))
	}

	if topology.Partitions[0].ID != "partition-1" {
		t.Errorf("Expected partition ID 'partition-1', got '%s'", topology.Partitions[0].ID)
	}

	if len(topology.Interconnects) != 1 {
		t.Errorf("Expected 1 interconnect, got %d", len(topology.Interconnects))
	}

	if topology.Interconnects[0].Type != "NVLink" {
		t.Errorf("Expected interconnect type 'NVLink', got '%s'", topology.Interconnects[0].Type)
	}

	if topology.Bandwidth["gpu-1-gpu-2"] != 600.0 {
		t.Errorf("Expected bandwidth 600.0, got %f", topology.Bandwidth["gpu-1-gpu-2"])
	}

	if topology.Latency["gpu-1-gpu-2"] != 0.1 {
		t.Errorf("Expected latency 0.1, got %f", topology.Latency["gpu-1-gpu-2"])
	}
}

func TestNVLinkTopology(t *testing.T) {
	topology := &NVLinkTopology{
		ConnectedGPUs: []string{"gpu-1", "gpu-2", "gpu-3"},
		Bandwidth: map[string]float64{
			"gpu-1-gpu-2": 600.0,
			"gpu-2-gpu-3": 600.0,
		},
		Latency: map[string]float64{
			"gpu-1-gpu-2": 0.1,
			"gpu-2-gpu-3": 0.1,
		},
		NVLinkVersion: "4.0",
		MaxBandwidth:  600.0,
		MinLatency:    0.1,
	}

	if len(topology.ConnectedGPUs) != 3 {
		t.Errorf("Expected 3 connected GPUs, got %d", len(topology.ConnectedGPUs))
	}

	if topology.NVLinkVersion != "4.0" {
		t.Errorf("Expected NVLink version '4.0', got '%s'", topology.NVLinkVersion)
	}

	if topology.MaxBandwidth != 600.0 {
		t.Errorf("Expected max bandwidth 600.0, got %f", topology.MaxBandwidth)
	}

	if topology.MinLatency != 0.1 {
		t.Errorf("Expected min latency 0.1, got %f", topology.MinLatency)
	}
}

func TestPartitionStatus(t *testing.T) {
	status := &PartitionStatus{
		ID:          "partition-1",
		State:       "active",
		ActiveGPUs:  []string{"gpu-1", "gpu-2"},
		Error:       "",
		LastUpdated: time.Now(),
	}

	if status.ID != "partition-1" {
		t.Errorf("Expected partition ID 'partition-1', got '%s'", status.ID)
	}

	if status.State != "active" {
		t.Errorf("Expected state 'active', got '%s'", status.State)
	}

	if len(status.ActiveGPUs) != 2 {
		t.Errorf("Expected 2 active GPUs, got %d", len(status.ActiveGPUs))
	}

	if status.Error != "" {
		t.Errorf("Expected empty error, got '%s'", status.Error)
	}
}
