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
	"time"

	"google.golang.org/grpc"
)

// FabricManagerClient defines the interface for communicating with FabricManager service.
type FabricManagerClient interface {
	// GetTopology retrieves the current fabric topology information
	GetTopology(ctx context.Context, in *GetTopologyRequest, opts ...grpc.CallOption) (*GetTopologyResponse, error)

	// ActivatePartition activates a fabric partition with the specified ID
	ActivatePartition(ctx context.Context, in *ActivatePartitionRequest, opts ...grpc.CallOption) (*ActivatePartitionResponse, error)

	// DeactivatePartition deactivates a fabric partition with the specified ID
	DeactivatePartition(ctx context.Context, in *DeactivatePartitionRequest, opts ...grpc.CallOption) (*DeactivatePartitionResponse, error)

	// GetPartitionStatus retrieves the status of a fabric partition
	GetPartitionStatus(ctx context.Context, in *GetPartitionStatusRequest, opts ...grpc.CallOption) (*GetPartitionStatusResponse, error)

	// Close closes the client connection
	Close() error
}

// FabricTopology represents the overall fabric topology.
type FabricTopology struct {
	Partitions    []FabricPartition  `json:"partitions"`
	Interconnects []Interconnect     `json:"interconnects"`
	Bandwidth     map[string]float64 `json:"bandwidth"` // GB/s
	Latency       map[string]float64 `json:"latency"`   // ms
	Timestamp     time.Time          `json:"timestamp"`
	Source        string             `json:"source"`
}

// FabricPartition represents a logical partition of GPUs with optimized interconnect.
type FabricPartition struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	GPUs       []string        `json:"gpus"`       // GPU UUIDs in this partition
	NVSwitches []string        `json:"nvswitches"` // NVSwitch IDs
	Topology   *NVLinkTopology `json:"topology"`
	Status     PartitionStatus `json:"status"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

// NVLinkTopology represents the NVLink connectivity within a partition.
type NVLinkTopology struct {
	ConnectedGPUs []string           `json:"connectedGpus"`
	Bandwidth     map[string]float64 `json:"bandwidth"` // GB/s between GPU pairs
	Latency       map[string]float64 `json:"latency"`   // ms between GPU pairs
	NVLinkVersion string             `json:"nvlinkVersion"`
	MaxBandwidth  float64            `json:"maxBandwidth"` // GB/s
	MinLatency    float64            `json:"minLatency"`   // ms
}

// Interconnect represents the interconnect configuration.
type Interconnect struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`        // "NVLink", "PCIe", etc.
	ConnectedTo []string `json:"connectedTo"` // Connected device IDs
	Bandwidth   float64  `json:"bandwidth"`   // GB/s
	Latency     float64  `json:"latency"`     // ms
	Status      string   `json:"status"`      // "active", "inactive", "error"
}

// PartitionStatus represents the status of a fabric partition.
type PartitionStatus struct {
	ID          string    `json:"id"`
	State       string    `json:"state"`      // "active", "inactive", "error"
	ActiveGPUs  []string  `json:"activeGpus"` // Currently active GPU UUIDs
	Error       string    `json:"error,omitempty"`
	LastUpdated time.Time `json:"lastUpdated"`
}

// FabricManagerConfig represents the configuration for FabricManager client.
type FabricManagerConfig struct {
	Endpoint   string        `json:"endpoint"`   // gRPC endpoint
	Timeout    time.Duration `json:"timeout"`    // Connection timeout
	RetryCount int           `json:"retryCount"` // Number of retries
	RetryDelay time.Duration `json:"retryDelay"` // Delay between retries
}

// DefaultFabricManagerConfig returns the default configuration.
func DefaultFabricManagerConfig() *FabricManagerConfig {
	return &FabricManagerConfig{
		Endpoint:   "localhost:50051", // Default FabricManager gRPC endpoint
		Timeout:    30 * time.Second,
		RetryCount: 3,
		RetryDelay: 1 * time.Second,
	}
}

// gRPC message types (temporary until protobuf generation is set up).
type GetTopologyRequest struct{}

type GetTopologyResponse struct {
	Partitions    []*FabricPartitionResponse `json:"partitions"`
	Interconnects []*InterconnectResponse    `json:"interconnects"`
	Bandwidth     map[string]float64         `json:"bandwidth"`
	Latency       map[string]float64         `json:"latency"`
	Timestamp     int64                      `json:"timestamp"`
	Source        string                     `json:"source"`
}

// gRPC response types with int64 timestamps.
type FabricPartitionResponse struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	GPUs       []string                 `json:"gpus"`
	NVSwitches []string                 `json:"nvswitches"`
	Topology   *NVLinkTopologyResponse  `json:"topology"`
	Status     *PartitionStatusResponse `json:"status"`
	CreatedAt  int64                    `json:"createdAt"`
	UpdatedAt  int64                    `json:"updatedAt"`
}

type NVLinkTopologyResponse struct {
	ConnectedGPUs []string           `json:"connectedGpus"`
	Bandwidth     map[string]float64 `json:"bandwidth"`
	Latency       map[string]float64 `json:"latency"`
	NVLinkVersion string             `json:"nvlinkVersion"`
	MaxBandwidth  float64            `json:"maxBandwidth"`
	MinLatency    float64            `json:"minLatency"`
}

type InterconnectResponse struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	ConnectedTo []string `json:"connectedTo"`
	Bandwidth   float64  `json:"bandwidth"`
	Latency     float64  `json:"latency"`
	Status      string   `json:"status"`
}

type PartitionStatusResponse struct {
	ID          string   `json:"id"`
	State       string   `json:"state"`
	ActiveGPUs  []string `json:"activeGpus"`
	Error       string   `json:"error"`
	LastUpdated int64    `json:"lastUpdated"`
}

type ActivatePartitionRequest struct {
	PartitionId string `json:"partitionId"`
}

type ActivatePartitionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

type DeactivatePartitionRequest struct {
	PartitionId string `json:"partitionId"`
}

type DeactivatePartitionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

type GetPartitionStatusRequest struct {
	PartitionId string `json:"partitionId"`
}

type GetPartitionStatusResponse struct {
	PartitionId string   `json:"partitionId"`
	State       string   `json:"state"`
	ActiveGpus  []string `json:"activeGpus"`
	Error       string   `json:"error"`
	LastUpdated int64    `json:"lastUpdated"`
}
