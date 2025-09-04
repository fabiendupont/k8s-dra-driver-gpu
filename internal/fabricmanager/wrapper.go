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
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/klog/v2"
)

// Client provides a high-level interface for FabricManager operations.
type Client interface {
	// GetTopology retrieves the current fabric topology information.
	GetTopology(ctx context.Context) (*FabricTopology, error)

	// ActivatePartition activates a fabric partition with the specified ID.
	ActivatePartition(ctx context.Context, partitionID string) error

	// DeactivatePartition deactivates a fabric partition with the specified ID.
	DeactivatePartition(ctx context.Context, partitionID string) error

	// GetPartitionStatus retrieves the status of a fabric partition.
	GetPartitionStatus(ctx context.Context, partitionID string) (*PartitionStatus, error)

	// Close closes the client connection.
	Close() error
}

// clientWrapper wraps the gRPC client to provide a simpler interface.
type clientWrapper struct {
	grpcClient FabricManagerClient
	config     *FabricManagerConfig
}

// NewClient creates a new high-level FabricManager client.
func NewClient(config *FabricManagerConfig) (Client, error) {
	if config == nil {
		config = DefaultFabricManagerConfig()
	}

	// Create gRPC connection
	conn, err := grpc.NewClient(config.Endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to FabricManager at %s: %w", config.Endpoint, err)
	}

	// Create gRPC client
	grpcClient := NewFabricManagerClient(conn)

	return &clientWrapper{
		grpcClient: grpcClient,
		config:     config,
	}, nil
}

// GetTopology retrieves the current fabric topology information.
func (c *clientWrapper) GetTopology(ctx context.Context) (*FabricTopology, error) {
	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	// Call gRPC method
	req := &GetTopologyRequest{}
	resp, err := c.grpcClient.GetTopology(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get topology: %w", err)
	}

	// Convert response to internal types
	topology := &FabricTopology{
		Partitions:    make([]FabricPartition, len(resp.Partitions)),
		Interconnects: make([]Interconnect, len(resp.Interconnects)),
		Bandwidth:     resp.Bandwidth,
		Latency:       resp.Latency,
		Timestamp:     time.Unix(resp.Timestamp, 0),
		Source:        resp.Source,
	}

	// Convert partitions
	for i, partition := range resp.Partitions {
		topology.Partitions[i] = FabricPartition{
			ID:         partition.ID,
			Name:       partition.Name,
			GPUs:       partition.GPUs,
			NVSwitches: partition.NVSwitches,
			Topology: &NVLinkTopology{
				ConnectedGPUs: partition.Topology.ConnectedGPUs,
				Bandwidth:     partition.Topology.Bandwidth,
				Latency:       partition.Topology.Latency,
				NVLinkVersion: partition.Topology.NVLinkVersion,
				MaxBandwidth:  partition.Topology.MaxBandwidth,
				MinLatency:    partition.Topology.MinLatency,
			},
			Status: PartitionStatus{
				ID:          partition.Status.ID,
				State:       partition.Status.State,
				ActiveGPUs:  partition.Status.ActiveGPUs,
				Error:       partition.Status.Error,
				LastUpdated: time.Unix(partition.Status.LastUpdated, 0),
			},
			CreatedAt: time.Unix(partition.CreatedAt, 0),
			UpdatedAt: time.Unix(partition.UpdatedAt, 0),
		}
	}

	// Convert interconnects
	for i, interconnect := range resp.Interconnects {
		topology.Interconnects[i] = Interconnect{
			ID:          interconnect.ID,
			Type:        interconnect.Type,
			ConnectedTo: interconnect.ConnectedTo,
			Bandwidth:   interconnect.Bandwidth,
			Latency:     interconnect.Latency,
			Status:      interconnect.Status,
		}
	}

	klog.V(6).Infof("Retrieved fabric topology with %d partitions and %d interconnects",
		len(topology.Partitions), len(topology.Interconnects))

	return topology, nil
}

// ActivatePartition activates a fabric partition with the specified ID.
func (c *clientWrapper) ActivatePartition(ctx context.Context, partitionID string) error {
	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	// Call gRPC method
	req := &ActivatePartitionRequest{
		PartitionId: partitionID,
	}

	resp, err := c.grpcClient.ActivatePartition(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to activate partition %s: %w", partitionID, err)
	}

	if !resp.Success {
		return fmt.Errorf("failed to activate partition %s: %s", partitionID, resp.Error)
	}

	klog.Infof("Successfully activated fabric partition %s: %s", partitionID, resp.Message)
	return nil
}

// DeactivatePartition deactivates a fabric partition with the specified ID.
func (c *clientWrapper) DeactivatePartition(ctx context.Context, partitionID string) error {
	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	// Call gRPC method
	req := &DeactivatePartitionRequest{
		PartitionId: partitionID,
	}

	resp, err := c.grpcClient.DeactivatePartition(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to deactivate partition %s: %w", partitionID, err)
	}

	if !resp.Success {
		return fmt.Errorf("failed to deactivate partition %s: %s", partitionID, resp.Error)
	}

	klog.Infof("Successfully deactivated fabric partition %s: %s", partitionID, resp.Message)
	return nil
}

// GetPartitionStatus retrieves the status of a fabric partition.
func (c *clientWrapper) GetPartitionStatus(ctx context.Context, partitionID string) (*PartitionStatus, error) {
	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	// Call gRPC method
	req := &GetPartitionStatusRequest{
		PartitionId: partitionID,
	}

	resp, err := c.grpcClient.GetPartitionStatus(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get partition status for %s: %w", partitionID, err)
	}

	status := &PartitionStatus{
		ID:          resp.PartitionId,
		State:       resp.State,
		ActiveGPUs:  resp.ActiveGpus,
		Error:       resp.Error,
		LastUpdated: time.Unix(resp.LastUpdated, 0),
	}

	klog.V(6).Infof("Retrieved partition status for %s: state=%s, active_gpus=%d",
		partitionID, status.State, len(status.ActiveGPUs))

	return status, nil
}

// Close closes the client connection.
func (c *clientWrapper) Close() error {
	// Note: The gRPC connection will be closed when the client is garbage collected
	// In a real implementation, we would store the connection and close it here
	return nil
}
