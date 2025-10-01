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
	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

// FabricTopology represents the complete fabric topology of the system.
type FabricTopology struct {
	GPUs         map[string]*FabricGPUInfo
	Cliques      map[string]*CliqueInfo
	Connectivity map[string]map[string]*LinkInfo
	OptimalPaths map[string]map[string]*PathInfo
}

// FabricGPUInfo contains fabric-specific information about a GPU.
type FabricGPUInfo struct {
	UUID          string
	CliqueID      string
	NVLinkPorts   []NVLinkPort
	FabricState   nvml.GpuFabricState
	ConnectedGPUs []string // Direct NVLink connections
	Bandwidth     int64    // Total bandwidth in GB/s
}

// CliqueInfo represents a fabric clique and its properties.
type CliqueInfo struct {
	ID        string
	GPUs      []string
	Bandwidth int64 // Total bandwidth in GB/s
	Latency   int64 // Average latency in ns
	HopCount  int   // Maximum hops within clique
	IsOptimal bool  // Whether this clique provides optimal connectivity
}

// LinkInfo represents connectivity between two GPUs.
type LinkInfo struct {
	SourceGPU string
	TargetGPU string
	Bandwidth int64  // GB/s
	Latency   int64  // ns
	LinkType  string // NVLink4, NVLink5, etc.
	IsDirect  bool
	PortID    int
}

// PathInfo represents the optimal path between two GPUs.
type PathInfo struct {
	SourceGPU string
	TargetGPU string
	Latency   int64
	HopCount  int
	Path      []string // Intermediate GPUs in the path
	Bandwidth int64    // Effective bandwidth considering the path
}

// NVLinkPort represents an NVLink port on a GPU.
type NVLinkPort struct {
	PortID      int
	IsActive    bool
	LinkType    string
	Bandwidth   int64
	ConnectedTo string // UUID of connected GPU
}

// FabricScore represents the performance score of a GPU combination.
type FabricScore struct {
	TotalBandwidth    int64
	AverageLatency    int64
	MaxHopCount       int
	DirectConnections int
	Score             float64
}

// ScoringWeights defines the weights for different performance metrics.
type ScoringWeights struct {
	Bandwidth  float64 // Default: 0.4
	Latency    float64 // Default: 0.3
	DirectConn float64 // Default: 0.2
	HopCount   float64 // Default: 0.1
}

// DefaultScoringWeights provides sensible defaults for scoring.
var DefaultScoringWeights = ScoringWeights{
	Bandwidth:  0.4,
	Latency:    0.3,
	DirectConn: 0.2,
	HopCount:   0.1,
}

// FabricOptimizationConfig contains configuration for fabric optimization.
type FabricOptimizationConfig struct {
	EnableAutoDetection     bool           `json:"enableAutoDetection"`
	MinBandwidthThreshold   int64          `json:"minBandwidthThreshold"` // GB/s
	MaxLatencyThreshold     int64          `json:"maxLatencyThreshold"`   // ns
	PreferDirectConnections bool           `json:"preferDirectConnections"`
	ScoringWeights          ScoringWeights `json:"scoringWeights"`
}

// DefaultFabricOptimizationConfig provides sensible defaults.
var DefaultFabricOptimizationConfig = FabricOptimizationConfig{
	EnableAutoDetection:     true,
	MinBandwidthThreshold:   100,  // 100 GB/s minimum
	MaxLatencyThreshold:     1000, // 1000ns maximum latency
	PreferDirectConnections: true,
	ScoringWeights:          DefaultScoringWeights,
}
