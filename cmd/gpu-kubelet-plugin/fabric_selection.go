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

// scoreGpuCombination scores a GPU combination based on fabric performance.
func (s *DeviceState) scoreGpuCombination(gpus []string, topology *FabricTopology) *FabricScore {
	if len(gpus) == 0 {
		return &FabricScore{}
	}

	if len(gpus) == 1 {
		// Single GPU - no fabric performance to measure
		return &FabricScore{
			TotalBandwidth:    0,
			AverageLatency:    0,
			DirectConnections: 0,
			MaxHopCount:       0,
		}
	}

	totalBandwidth := int64(0)
	totalLatency := int64(0)
	directConnections := 0
	maxHopCount := 0

	// Calculate pairwise connectivity
	for i := 0; i < len(gpus); i++ {
		for j := i + 1; j < len(gpus); j++ {
			gpu1 := gpus[i]
			gpu2 := gpus[j]

			// Get connectivity info
			linkInfo := topology.Connectivity[gpu1][gpu2]
			if linkInfo != nil {
				totalBandwidth += linkInfo.Bandwidth
				totalLatency += linkInfo.Latency
				if linkInfo.IsDirect {
					directConnections++
				}
				// For now, assume direct connections have hop count 1, indirect have 2
				if linkInfo.IsDirect {
					maxHopCount = 1
				} else {
					maxHopCount = 2
				}
			}
		}
	}

	// Calculate averages
	pairs := len(gpus) * (len(gpus) - 1) / 2
	averageLatency := int64(0)
	if pairs > 0 {
		averageLatency = totalLatency / int64(pairs)
	}

	return &FabricScore{
		TotalBandwidth:    totalBandwidth,
		AverageLatency:    averageLatency,
		DirectConnections: directConnections,
		MaxHopCount:       maxHopCount,
		Score: s.calculateCompositeScore(&FabricScore{
			TotalBandwidth:    totalBandwidth,
			AverageLatency:    averageLatency,
			DirectConnections: directConnections,
			MaxHopCount:       maxHopCount,
		}, &DefaultScoringWeights),
	}
}

// generateGpuCombinations generates all possible combinations of GPUs.
func (s *DeviceState) generateGpuCombinations(gpus []string, count int) [][]string {
	if count <= 0 || count > len(gpus) {
		return [][]string{}
	}

	var combinations [][]string
	s.generateCombinationsRecursive(gpus, count, 0, []string{}, &combinations)
	return combinations
}

// calculateCompositeScore calculates a composite score based on performance metrics and weights.
func (s *DeviceState) calculateCompositeScore(score *FabricScore, weights *ScoringWeights) float64 {
	if score == nil || weights == nil {
		return 0.0
	}

	// Normalize scores (simplified normalization)
	bandwidthScore := float64(score.TotalBandwidth) / 1000.0       // Normalize to 0-1 range
	latencyScore := 1.0 - (float64(score.AverageLatency) / 2000.0) // Invert latency (lower is better)
	if latencyScore < 0 {
		latencyScore = 0
	}
	directScore := float64(score.DirectConnections) / 10.0 // Normalize to 0-1 range
	if directScore > 1.0 {
		directScore = 1.0
	}
	hopScore := 1.0 - (float64(score.MaxHopCount) / 20.0) // Invert hop count (lower is better)
	if hopScore < 0 {
		hopScore = 0
	}

	// Calculate weighted composite score
	compositeScore := bandwidthScore*weights.Bandwidth +
		latencyScore*weights.Latency +
		directScore*weights.DirectConn +
		hopScore*weights.HopCount

	return compositeScore
}

func (s *DeviceState) generateCombinationsRecursive(gpus []string, count int, start int, current []string, combinations *[][]string) {
	if len(current) == count {
		// Make a copy of current combination
		combination := make([]string, len(current))
		copy(combination, current)
		*combinations = append(*combinations, combination)
		return
	}

	for i := start; i < len(gpus); i++ {
		current = append(current, gpus[i])
		s.generateCombinationsRecursive(gpus, count, i+1, current, combinations)
		current = current[:len(current)-1] // Backtrack
	}
}
