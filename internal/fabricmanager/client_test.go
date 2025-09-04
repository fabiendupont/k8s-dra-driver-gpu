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
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
)

// mockFabricManagerClient is a mock implementation of FabricManagerClient for testing.
type mockFabricManagerClient struct {
	topology        *GetTopologyResponse
	activateError   error
	deactivateError error
	statusResponse  *GetPartitionStatusResponse
	statusError     error
}

func (m *mockFabricManagerClient) GetTopology(ctx context.Context, in *GetTopologyRequest, opts ...grpc.CallOption) (*GetTopologyResponse, error) {
	if m.topology == nil {
		return &GetTopologyResponse{
			Partitions:    []*FabricPartitionResponse{},
			Interconnects: []*InterconnectResponse{},
			Bandwidth:     make(map[string]float64),
			Latency:       make(map[string]float64),
			Timestamp:     time.Now().Unix(),
			Source:        "mock",
		}, nil
	}
	return m.topology, nil
}

func (m *mockFabricManagerClient) ActivatePartition(ctx context.Context, in *ActivatePartitionRequest, opts ...grpc.CallOption) (*ActivatePartitionResponse, error) {
	if m.activateError != nil {
		return nil, m.activateError
	}
	return &ActivatePartitionResponse{
		Success: true,
		Message: "Partition activated successfully",
	}, nil
}

func (m *mockFabricManagerClient) DeactivatePartition(ctx context.Context, in *DeactivatePartitionRequest, opts ...grpc.CallOption) (*DeactivatePartitionResponse, error) {
	if m.deactivateError != nil {
		return nil, m.deactivateError
	}
	return &DeactivatePartitionResponse{
		Success: true,
		Message: "Partition deactivated successfully",
	}, nil
}

func (m *mockFabricManagerClient) GetPartitionStatus(ctx context.Context, in *GetPartitionStatusRequest, opts ...grpc.CallOption) (*GetPartitionStatusResponse, error) {
	if m.statusError != nil {
		return nil, m.statusError
	}
	if m.statusResponse == nil {
		return &GetPartitionStatusResponse{
			PartitionId: in.PartitionId,
			State:       "active",
			ActiveGpus:  []string{},
			LastUpdated: time.Now().Unix(),
		}, nil
	}
	return m.statusResponse, nil
}

func (m *mockFabricManagerClient) Close() error {
	return nil
}

func TestNewClient(t *testing.T) {
	// Test with default config
	client, err := NewClient(nil)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("Expected client, got nil")
	}
	defer client.Close()

	// Test with custom config
	config := &FabricManagerConfig{
		Endpoint:   "localhost:50052",
		Timeout:    10 * time.Second,
		RetryCount: 2,
		RetryDelay: 500 * time.Millisecond,
	}

	client2, err := NewClient(config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if client2 == nil {
		t.Fatal("Expected client, got nil")
	}
	defer client2.Close()
}

func TestClientGetTopology(t *testing.T) {
	// Create a mock client wrapper
	mockGrpcClient := &mockFabricManagerClient{
		topology: &GetTopologyResponse{
			Partitions: []*FabricPartitionResponse{
				{
					ID:         "partition-1",
					Name:       "Test Partition",
					GPUs:       []string{"gpu-1", "gpu-2"},
					NVSwitches: []string{"nvswitch-1"},
					Topology: &NVLinkTopologyResponse{
						ConnectedGPUs: []string{"gpu-1", "gpu-2"},
						Bandwidth:     map[string]float64{"gpu-1-gpu-2": 600.0},
						Latency:       map[string]float64{"gpu-1-gpu-2": 0.1},
						NVLinkVersion: "4.0",
						MaxBandwidth:  600.0,
						MinLatency:    0.1,
					},
					Status: &PartitionStatusResponse{
						ID:          "partition-1",
						State:       "active",
						ActiveGPUs:  []string{"gpu-1", "gpu-2"},
						LastUpdated: time.Now().Unix(),
					},
					CreatedAt: time.Now().Unix(),
					UpdatedAt: time.Now().Unix(),
				},
			},
			Interconnects: []*InterconnectResponse{
				{
					ID:          "nvlink-1",
					Type:        "NVLink",
					ConnectedTo: []string{"gpu-1", "gpu-2"},
					Bandwidth:   600.0,
					Latency:     0.1,
					Status:      "active",
				},
			},
			Bandwidth: map[string]float64{
				"gpu-1-gpu-2": 600.0,
			},
			Latency: map[string]float64{
				"gpu-1-gpu-2": 0.1,
			},
			Timestamp: time.Now().Unix(),
			Source:    "mock",
		},
	}

	client := &clientWrapper{
		grpcClient: mockGrpcClient,
		config:     DefaultFabricManagerConfig(),
	}

	ctx := context.Background()
	topology, err := client.GetTopology(ctx)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if topology == nil {
		t.Fatal("Expected topology, got nil")
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

	if topology.Source != "mock" {
		t.Errorf("Expected source 'mock', got '%s'", topology.Source)
	}
}

func TestClientActivatePartition(t *testing.T) {
	mockGrpcClient := &mockFabricManagerClient{}
	client := &clientWrapper{
		grpcClient: mockGrpcClient,
		config:     DefaultFabricManagerConfig(),
	}

	ctx := context.Background()
	err := client.ActivatePartition(ctx, "partition-1")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
}

func TestClientDeactivatePartition(t *testing.T) {
	mockGrpcClient := &mockFabricManagerClient{}
	client := &clientWrapper{
		grpcClient: mockGrpcClient,
		config:     DefaultFabricManagerConfig(),
	}

	ctx := context.Background()
	err := client.DeactivatePartition(ctx, "partition-1")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
}

func TestClientGetPartitionStatus(t *testing.T) {
	mockGrpcClient := &mockFabricManagerClient{
		statusResponse: &GetPartitionStatusResponse{
			PartitionId: "partition-1",
			State:       "active",
			ActiveGpus:  []string{"gpu-1", "gpu-2"},
			LastUpdated: time.Now().Unix(),
		},
	}

	client := &clientWrapper{
		grpcClient: mockGrpcClient,
		config:     DefaultFabricManagerConfig(),
	}

	ctx := context.Background()
	status, err := client.GetPartitionStatus(ctx, "partition-1")
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

func TestClientClose(t *testing.T) {
	client := &clientWrapper{
		grpcClient: &mockFabricManagerClient{},
		config:     DefaultFabricManagerConfig(),
	}

	err := client.Close()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
}
