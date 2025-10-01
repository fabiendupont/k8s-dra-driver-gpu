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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFabricTopologyDiscovery(t *testing.T) {
	tests := []struct {
		name            string
		allocatable     AllocatableDevices
		expectedGPUs    int
		expectedCliques int
		expectError     bool
	}{
		{
			name: "single GPU",
			allocatable: AllocatableDevices{
				"gpu-1": {
					Gpu: &GpuInfo{
						UUID:         "gpu-1",
						architecture: "Hopper",
					},
				},
			},
			expectedGPUs:    1,
			expectedCliques: 1,
			expectError:     false,
		},
		{
			name: "multiple GPUs same clique",
			allocatable: AllocatableDevices{
				"gpu-1": {
					Gpu: &GpuInfo{
						UUID:         "gpu-1",
						architecture: "Hopper",
					},
				},
				"gpu-2": {
					Gpu: &GpuInfo{
						UUID:         "gpu-2",
						architecture: "Hopper",
					},
				},
			},
			expectedGPUs:    2,
			expectedCliques: 1,
			expectError:     false,
		},
		{
			name:            "no GPUs",
			allocatable:     AllocatableDevices{},
			expectedGPUs:    0,
			expectedCliques: 0,
			expectError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deviceState := &DeviceState{
				allocatable: tt.allocatable,
			}

			topology, err := deviceState.discoverFabricTopology()

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedGPUs, len(topology.GPUs))
			assert.Equal(t, tt.expectedCliques, len(topology.Cliques))
		})
	}
}

func TestScoreGpuCombination(t *testing.T) {
	tests := []struct {
		name              string
		gpus              []string
		topology          *FabricTopology
		expectedScore     float64
		expectedBandwidth int64
	}{
		{
			name: "single GPU",
			gpus: []string{"gpu-1"},
			topology: &FabricTopology{
				GPUs: map[string]*FabricGPUInfo{
					"gpu-1": {
						UUID:      "gpu-1",
						CliqueID:  "0",
						Bandwidth: 0,
					},
				},
				Connectivity: map[string]map[string]*LinkInfo{},
				OptimalPaths: map[string]map[string]*PathInfo{},
			},
			expectedScore:     0.0, // Single GPU has no fabric performance
			expectedBandwidth: 0,
		},
		{
			name: "two GPUs with direct connection",
			gpus: []string{"gpu-1", "gpu-2"},
			topology: &FabricTopology{
				GPUs: map[string]*FabricGPUInfo{
					"gpu-1": {
						UUID:      "gpu-1",
						CliqueID:  "0",
						Bandwidth: 200,
					},
					"gpu-2": {
						UUID:      "gpu-2",
						CliqueID:  "0",
						Bandwidth: 200,
					},
				},
				Connectivity: map[string]map[string]*LinkInfo{
					"gpu-1": {
						"gpu-2": {
							SourceGPU: "gpu-1",
							TargetGPU: "gpu-2",
							Bandwidth: 200,
							Latency:   100,
							IsDirect:  true,
						},
					},
					"gpu-2": {
						"gpu-1": {
							SourceGPU: "gpu-2",
							TargetGPU: "gpu-1",
							Bandwidth: 200,
							Latency:   100,
							IsDirect:  true,
						},
					},
				},
				OptimalPaths: map[string]map[string]*PathInfo{
					"gpu-1": {
						"gpu-2": {
							SourceGPU: "gpu-1",
							TargetGPU: "gpu-2",
							Latency:   100,
							HopCount:  0,
							Bandwidth: 200,
						},
					},
					"gpu-2": {
						"gpu-1": {
							SourceGPU: "gpu-2",
							TargetGPU: "gpu-1",
							Latency:   100,
							HopCount:  0,
							Bandwidth: 200,
						},
					},
				},
			},
			expectedScore:     0.48, // Bandwidth (0.2*0.4) + Latency (0.95*0.3) + Direct (0.1*0.2) + Hop (0.95*0.1) = 0.48
			expectedBandwidth: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deviceState := &DeviceState{}

			score := deviceState.scoreGpuCombination(tt.gpus, tt.topology)

			assert.Equal(t, tt.expectedBandwidth, score.TotalBandwidth)
			assert.InDelta(t, tt.expectedScore, score.Score, 0.01) // Allow small floating point differences
		})
	}
}

func TestGenerateGpuCombinations(t *testing.T) {
	tests := []struct {
		name          string
		gpus          []string
		count         int
		expectedCount int
	}{
		{
			name:          "2 GPUs, select 1",
			gpus:          []string{"gpu-1", "gpu-2"},
			count:         1,
			expectedCount: 2,
		},
		{
			name:          "3 GPUs, select 2",
			gpus:          []string{"gpu-1", "gpu-2", "gpu-3"},
			count:         2,
			expectedCount: 3, // C(3,2) = 3
		},
		{
			name:          "4 GPUs, select 2",
			gpus:          []string{"gpu-1", "gpu-2", "gpu-3", "gpu-4"},
			count:         2,
			expectedCount: 6, // C(4,2) = 6
		},
		{
			name:          "invalid count",
			gpus:          []string{"gpu-1", "gpu-2"},
			count:         3,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deviceState := &DeviceState{}

			combinations := deviceState.generateGpuCombinations(tt.gpus, tt.count)

			assert.Equal(t, tt.expectedCount, len(combinations))
		})
	}
}

func TestCalculateCompositeScore(t *testing.T) {
	tests := []struct {
		name     string
		score    *FabricScore
		weights  ScoringWeights
		expected float64
	}{
		{
			name: "perfect score",
			score: &FabricScore{
				TotalBandwidth:    1000, // 1 TB/s
				AverageLatency:    100,  // 100 ns
				DirectConnections: 10,   // Max connections
				MaxHopCount:       0,    // No hops
			},
			weights:  DefaultScoringWeights,
			expected: 0.985, // Bandwidth (1.0*0.4) + Latency (0.95*0.3) + Direct (1.0*0.2) + Hop (1.0*0.1) = 0.985
		},
		{
			name: "zero score",
			score: &FabricScore{
				TotalBandwidth:    0,
				AverageLatency:    1000,
				DirectConnections: 0,
				MaxHopCount:       10,
			},
			weights:  DefaultScoringWeights,
			expected: 0.2, // Bandwidth (0.0*0.4) + Latency (0.5*0.3) + Direct (0.0*0.2) + Hop (0.5*0.1) = 0.2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deviceState := &DeviceState{}

			compositeScore := deviceState.calculateCompositeScore(tt.score, &tt.weights)

			assert.InDelta(t, tt.expected, compositeScore, 0.01) // Allow small floating point differences
		})
	}
}
