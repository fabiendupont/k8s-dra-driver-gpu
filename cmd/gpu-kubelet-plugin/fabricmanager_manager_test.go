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
	"testing"

	"github.com/stretchr/testify/assert"

	"k8s.io/utils/ptr"

	configapi "github.com/NVIDIA/k8s-dra-driver-gpu/api/nvidia.com/resource/v1beta1"
	"github.com/NVIDIA/k8s-dra-driver-gpu/internal/fabricmanager"
)

// mockFabricManagerClient is a mock implementation for testing.
type mockFabricManagerClient struct {
	topology        *fabricmanager.FabricTopology
	topologyError   error
	activateError   error
	deactivateError error
	statusResponse  *fabricmanager.PartitionStatus
	statusError     error
}

func (m *mockFabricManagerClient) GetTopology(ctx context.Context) (*fabricmanager.FabricTopology, error) {
	if m.topologyError != nil {
		return nil, m.topologyError
	}
	// Return exactly what was set, including nil
	return m.topology, nil
}

func (m *mockFabricManagerClient) ActivatePartition(ctx context.Context, partitionID string) error {
	return m.activateError
}

func (m *mockFabricManagerClient) DeactivatePartition(ctx context.Context, partitionID string) error {
	return m.deactivateError
}

func (m *mockFabricManagerClient) GetPartitionStatus(ctx context.Context, partitionID string) (*fabricmanager.PartitionStatus, error) {
	if m.statusError != nil {
		return nil, m.statusError
	}
	if m.statusResponse == nil {
		return &fabricmanager.PartitionStatus{
			ID:         partitionID,
			State:      "active",
			ActiveGPUs: []string{},
		}, nil
	}
	return m.statusResponse, nil
}

func (m *mockFabricManagerClient) Close() error {
	return nil
}

func TestNewFabricManagerManager(t *testing.T) {
	config := &Config{}

	// Test with feature gate disabled (should not fail)
	manager, err := NewFabricManagerManager(context.Background(), config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if manager == nil {
		t.Fatal("Expected manager, got nil")
	}
}

func TestFabricManagerManagerGetTopology(t *testing.T) {
	manager := &FabricManagerManager{
		topology: &fabricmanager.FabricTopology{
			Partitions: []fabricmanager.FabricPartition{
				{
					ID:   "partition-1",
					Name: "Test Partition",
					GPUs: []string{"gpu-1", "gpu-2"},
				},
			},
		},
	}

	topology := manager.GetTopology()
	if topology == nil {
		t.Fatal("Expected topology, got nil")
	}

	if len(topology.Partitions) != 1 {
		t.Errorf("Expected 1 partition, got %d", len(topology.Partitions))
	}

	if topology.Partitions[0].ID != "partition-1" {
		t.Errorf("Expected partition ID 'partition-1', got '%s'", topology.Partitions[0].ID)
	}
}

func TestFabricManagerManagerFindOptimalPartitionAuto(t *testing.T) {
	topology := &fabricmanager.FabricTopology{
		Partitions: []fabricmanager.FabricPartition{
			{
				ID:   "partition-2",
				Name: "2-GPU Partition",
				GPUs: []string{"gpu-1", "gpu-2"},
			},
			{
				ID:   "partition-4",
				Name: "4-GPU Partition",
				GPUs: []string{"gpu-1", "gpu-2", "gpu-3", "gpu-4"},
			},
		},
	}

	manager := &FabricManagerManager{
		client:   &mockFabricManagerClient{},
		topology: topology,
	}

	config := &configapi.FabricTopologyConfig{
		Strategy:      configapi.FabricTopologyAuto,
		PartitionSize: ptr.To(2),
	}

	// Test finding 2-GPU partition
	partition, err := manager.FindOptimalPartition(context.Background(), []string{"gpu-1", "gpu-2"}, config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if partition == nil {
		t.Fatal("Expected partition, got nil")
	}

	if partition.ID != "partition-2" {
		t.Errorf("Expected partition ID 'partition-2', got '%s'", partition.ID)
	}

	// Test finding 4-GPU partition
	config.PartitionSize = ptr.To(4)
	partition, err = manager.FindOptimalPartition(context.Background(), []string{"gpu-1", "gpu-2", "gpu-3", "gpu-4"}, config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if partition == nil {
		t.Fatal("Expected partition, got nil")
	}

	if partition.ID != "partition-4" {
		t.Errorf("Expected partition ID 'partition-4', got '%s'", partition.ID)
	}
}

func TestFabricManagerManagerFindOptimalPartitionManual(t *testing.T) {
	topology := &fabricmanager.FabricTopology{
		Partitions: []fabricmanager.FabricPartition{
			{
				ID:   "partition-1",
				Name: "Manual Partition 1",
				GPUs: []string{"gpu-1", "gpu-2"},
			},
			{
				ID:   "partition-2",
				Name: "Manual Partition 2",
				GPUs: []string{"gpu-3", "gpu-4"},
			},
		},
	}

	manager := &FabricManagerManager{
		client:   &mockFabricManagerClient{},
		topology: topology,
	}

	config := &configapi.FabricTopologyConfig{
		Strategy:     configapi.FabricTopologyManual,
		PartitionIDs: []string{"partition-1"},
	}

	// Test finding specific partition
	partition, err := manager.FindOptimalPartition(context.Background(), []string{"gpu-1", "gpu-2"}, config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if partition == nil {
		t.Fatal("Expected partition, got nil")
	}

	if partition.ID != "partition-1" {
		t.Errorf("Expected partition ID 'partition-1', got '%s'", partition.ID)
	}
}

func TestFabricManagerManagerFindOptimalPartitionDisabled(t *testing.T) {
	manager := &FabricManagerManager{
		client: &mockFabricManagerClient{},
	}

	config := &configapi.FabricTopologyConfig{
		Strategy: configapi.FabricTopologyDisabled,
	}

	// Test disabled strategy
	partition, err := manager.FindOptimalPartition(context.Background(), []string{"gpu-1", "gpu-2"}, config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if partition != nil {
		t.Errorf("Expected nil partition for disabled strategy, got %v", partition)
	}
}

func TestFabricManagerManagerActivatePartition(t *testing.T) {
	manager := &FabricManagerManager{
		client: &mockFabricManagerClient{},
	}

	err := manager.ActivatePartition(context.Background(), "partition-1")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
}

func TestFabricManagerManagerDeactivatePartition(t *testing.T) {
	manager := &FabricManagerManager{
		client: &mockFabricManagerClient{},
	}

	err := manager.DeactivatePartition(context.Background(), "partition-1")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
}

func TestFabricManagerManagerGetPartitionStatus(t *testing.T) {
	expectedStatus := &fabricmanager.PartitionStatus{
		ID:         "partition-1",
		State:      "active",
		ActiveGPUs: []string{"gpu-1", "gpu-2"},
	}

	manager := &FabricManagerManager{
		client: &mockFabricManagerClient{
			statusResponse: expectedStatus,
		},
	}

	status, err := manager.GetPartitionStatus(context.Background(), "partition-1")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if status == nil {
		t.Fatal("Expected status, got nil")
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
}

func TestFabricManagerManagerContainsAllGPUs(t *testing.T) {
	manager := &FabricManagerManager{}

	tests := []struct {
		name           string
		partitionGPUs  []string
		requestedGPUs  []string
		expectedResult bool
	}{
		{
			name:           "All GPUs present",
			partitionGPUs:  []string{"gpu-1", "gpu-2", "gpu-3"},
			requestedGPUs:  []string{"gpu-1", "gpu-2"},
			expectedResult: true,
		},
		{
			name:           "Some GPUs missing",
			partitionGPUs:  []string{"gpu-1", "gpu-2"},
			requestedGPUs:  []string{"gpu-1", "gpu-3"},
			expectedResult: false,
		},
		{
			name:           "Empty requested GPUs",
			partitionGPUs:  []string{"gpu-1", "gpu-2"},
			requestedGPUs:  []string{},
			expectedResult: true,
		},
		{
			name:           "Exact match",
			partitionGPUs:  []string{"gpu-1", "gpu-2"},
			requestedGPUs:  []string{"gpu-1", "gpu-2"},
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.containsAllGPUs(tt.partitionGPUs, tt.requestedGPUs)
			if result != tt.expectedResult {
				t.Errorf("Expected %v, got %v", tt.expectedResult, result)
			}
		})
	}
}

func TestFabricManagerManagerValidateConfiguration(t *testing.T) {
	t.Run("Nil client", func(t *testing.T) {
		manager := &FabricManagerManager{}
		err := manager.validateFabricManagerConfiguration(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "FabricManager client not initialized")
	})

	t.Run("Client with valid topology", func(t *testing.T) {
		mockClient := &mockFabricManagerClient{
			topology: &fabricmanager.FabricTopology{
				Partitions: []fabricmanager.FabricPartition{
					{
						ID:   "partition-1",
						GPUs: []string{"gpu-1", "gpu-2"},
					},
					{
						ID:   "partition-2",
						GPUs: []string{"gpu-3", "gpu-4"},
					},
				},
				Interconnects: []fabricmanager.Interconnect{
					{
						ID:        "nvlink-1",
						Type:      "NVLink",
						Bandwidth: 600.0,
					},
				},
			},
		}

		manager := &FabricManagerManager{
			client: mockClient,
		}

		err := manager.validateFabricManagerConfiguration(context.Background())
		assert.NoError(t, err)
	})

	t.Run("Client with empty topology", func(t *testing.T) {
		mockClient := &mockFabricManagerClient{
			topology: &fabricmanager.FabricTopology{
				Partitions:    []fabricmanager.FabricPartition{},
				Interconnects: []fabricmanager.Interconnect{},
			},
		}

		manager := &FabricManagerManager{
			client: mockClient,
		}

		err := manager.validateFabricManagerConfiguration(context.Background())
		assert.NoError(t, err) // Should not fail, just log warnings
	})

	t.Run("Client with single GPU partitions", func(t *testing.T) {
		mockClient := &mockFabricManagerClient{
			topology: &fabricmanager.FabricTopology{
				Partitions: []fabricmanager.FabricPartition{
					{
						ID:   "partition-1",
						GPUs: []string{"gpu-1"},
					},
				},
				Interconnects: []fabricmanager.Interconnect{},
			},
		}

		manager := &FabricManagerManager{
			client: mockClient,
		}

		err := manager.validateFabricManagerConfiguration(context.Background())
		assert.NoError(t, err) // Should not fail, just log warnings
	})

	t.Run("Client connection error", func(t *testing.T) {
		mockClient := &mockFabricManagerClient{
			topologyError: fmt.Errorf("connection failed"),
		}

		manager := &FabricManagerManager{
			client: mockClient,
		}

		err := manager.validateFabricManagerConfiguration(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to connect to FabricManager service")
	})

	t.Run("Client returns nil topology", func(t *testing.T) {
		mockClient := &mockFabricManagerClient{
			topology:      nil,
			topologyError: nil, // No error, but returns nil topology
		}

		manager := &FabricManagerManager{
			client: mockClient,
		}

		err := manager.validateFabricManagerConfiguration(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "FabricManager returned nil topology")
	})
}

func TestFabricManagerManagerValidateVirtualizationModelCompatibility(t *testing.T) {
	manager := &FabricManagerManager{}

	testCases := []struct {
		name                string
		config              *configapi.FabricTopologyConfig
		expectError         bool
		expectedErrorSubstr string
	}{
		{
			name: "Nil virtualization model - should pass",
			config: &configapi.FabricTopologyConfig{
				Strategy: configapi.FabricTopologyAuto,
			},
			expectError: false,
		},
		{
			name: "BareMetal virtualization model - should pass",
			config: &configapi.FabricTopologyConfig{
				Strategy:            configapi.FabricTopologyAuto,
				VirtualizationModel: ptr.To(configapi.FabricVirtualizationBareMetal),
			},
			expectError: false,
		},
		{
			name: "FullPassthrough virtualization model - should pass",
			config: &configapi.FabricTopologyConfig{
				Strategy:            configapi.FabricTopologyAuto,
				VirtualizationModel: ptr.To(configapi.FabricVirtualizationFullPassthrough),
			},
			expectError: false,
		},
		{
			name: "SharedNVSwitch virtualization model - should pass",
			config: &configapi.FabricTopologyConfig{
				Strategy:            configapi.FabricTopologyAuto,
				VirtualizationModel: ptr.To(configapi.FabricVirtualizationSharedNVSwitch),
			},
			expectError: false,
		},
		{
			name: "VGPU virtualization model - should fail",
			config: &configapi.FabricTopologyConfig{
				Strategy:            configapi.FabricTopologyAuto,
				VirtualizationModel: ptr.To(configapi.FabricVirtualizationVGPU),
			},
			expectError:         true,
			expectedErrorSubstr: "vGPU virtualization model does not support fabric topology optimization",
		},
		{
			name: "Invalid virtualization model - should fail",
			config: &configapi.FabricTopologyConfig{
				Strategy:            configapi.FabricTopologyAuto,
				VirtualizationModel: ptr.To("InvalidModel"),
			},
			expectError:         true,
			expectedErrorSubstr: "unsupported virtualization model",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := manager.validateVirtualizationModelCompatibility(tc.config)

			if tc.expectError {
				assert.Error(t, err)
				if tc.expectedErrorSubstr != "" {
					assert.Contains(t, err.Error(), tc.expectedErrorSubstr)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFabricManagerManagerVirtualizationModelCapabilities(t *testing.T) {
	manager := &FabricManagerManager{}

	testCases := []struct {
		name                    string
		config                  *configapi.FabricTopologyConfig
		expectedCanManage       bool
		expectedRequiresService bool
		expectedSupportsFabric  bool
	}{
		{
			name: "BareMetal - full capabilities",
			config: &configapi.FabricTopologyConfig{
				Strategy:            configapi.FabricTopologyAuto,
				VirtualizationModel: ptr.To(configapi.FabricVirtualizationBareMetal),
			},
			expectedCanManage:       true,
			expectedRequiresService: false,
			expectedSupportsFabric:  true,
		},
		{
			name: "FullPassthrough - full capabilities",
			config: &configapi.FabricTopologyConfig{
				Strategy:            configapi.FabricTopologyAuto,
				VirtualizationModel: ptr.To(configapi.FabricVirtualizationFullPassthrough),
			},
			expectedCanManage:       true,
			expectedRequiresService: false,
			expectedSupportsFabric:  true,
		},
		{
			name: "SharedNVSwitch - service VM required",
			config: &configapi.FabricTopologyConfig{
				Strategy:            configapi.FabricTopologyAuto,
				VirtualizationModel: ptr.To(configapi.FabricVirtualizationSharedNVSwitch),
			},
			expectedCanManage:       false,
			expectedRequiresService: true,
			expectedSupportsFabric:  true,
		},
		{
			name: "VGPU - no fabric support",
			config: &configapi.FabricTopologyConfig{
				Strategy:            configapi.FabricTopologyAuto,
				VirtualizationModel: ptr.To(configapi.FabricVirtualizationVGPU),
			},
			expectedCanManage:       false,
			expectedRequiresService: false,
			expectedSupportsFabric:  false,
		},
		{
			name: "Nil config - defaults to BareMetal",
			config: &configapi.FabricTopologyConfig{
				Strategy: configapi.FabricTopologyAuto,
			},
			expectedCanManage:       true,
			expectedRequiresService: false,
			expectedSupportsFabric:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			canManage := manager.CanManagePartitions(tc.config)
			requiresService := manager.RequiresServiceVM(tc.config)
			supportsFabric := manager.SupportsFabricTopology(tc.config)

			assert.Equal(t, tc.expectedCanManage, canManage, "CanManagePartitions")
			assert.Equal(t, tc.expectedRequiresService, requiresService, "RequiresServiceVM")
			assert.Equal(t, tc.expectedSupportsFabric, supportsFabric, "SupportsFabricTopology")
		})
	}
}
