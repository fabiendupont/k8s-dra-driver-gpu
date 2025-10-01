/*
Copyright 2025 NVIDIA Corporation

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"math/rand"
	"time"

	"k8s.io/klog/v2"
)

// FallbackStrategy defines the strategy for graceful degradation.
type FallbackStrategy string

const (
	// FallbackStrategyRandom selects GPUs randomly when fabric optimization fails.
	FallbackStrategyRandom FallbackStrategy = "random"
	// FallbackStrategySequential selects GPUs in sequential order.
	FallbackStrategySequential FallbackStrategy = "sequential"
	// FallbackStrategyFail returns an error when fabric optimization fails.
	FallbackStrategyFail FallbackStrategy = "fail"
)

// FallbackConfig contains configuration for graceful degradation.
type FallbackConfig struct {
	Strategy         FallbackStrategy `json:"strategy"`
	EnableFallback   bool             `json:"enableFallback"`
	LogFallbackUsage bool             `json:"logFallbackUsage"`
	MaxRetries       int              `json:"maxRetries"`
	RetryDelay       time.Duration    `json:"retryDelay"`
}

// DefaultFallbackConfig provides sensible defaults for fallback behavior.
var DefaultFallbackConfig = FallbackConfig{
	Strategy:         FallbackStrategyRandom,
	EnableFallback:   true,
	LogFallbackUsage: true,
	MaxRetries:       3,
	RetryDelay:       100 * time.Millisecond,
}

// selectGpuFallback selects GPUs using fallback strategy when fabric optimization fails.
func (s *DeviceState) selectGpuFallback(requestedCount int, availableGPUs []string, config *FallbackConfig) ([]string, error) {
	if config == nil {
		config = &DefaultFallbackConfig
	}

	if !config.EnableFallback {
		return nil, fmt.Errorf("fabric optimization failed and fallback is disabled")
	}

	if len(availableGPUs) < requestedCount {
		return nil, fmt.Errorf("insufficient GPUs available for fallback: need %d, have %d", requestedCount, len(availableGPUs))
	}

	var selectedGPUs []string

	switch config.Strategy {
	case FallbackStrategyRandom:
		selectedGPUs = s.selectGpuRandom(availableGPUs, requestedCount)
	case FallbackStrategySequential:
		selectedGPUs = s.selectGpuSequential(availableGPUs, requestedCount)
	case FallbackStrategyFail:
		return nil, fmt.Errorf("fabric optimization failed and fallback strategy is set to fail")
	default:
		klog.Warningf("Unknown fallback strategy '%s', using random", config.Strategy)
		selectedGPUs = s.selectGpuRandom(availableGPUs, requestedCount)
	}

	if config.LogFallbackUsage {
		klog.Warningf("Using fallback GPU selection strategy '%s': selected %v from %d available GPUs",
			config.Strategy, selectedGPUs, len(availableGPUs))
	}

	return selectedGPUs, nil
}

// selectGpuRandom selects GPUs randomly.
func (s *DeviceState) selectGpuRandom(availableGPUs []string, requestedCount int) []string {
	if len(availableGPUs) <= requestedCount {
		return availableGPUs
	}

	// Create a copy to avoid modifying the original slice
	gpus := make([]string, len(availableGPUs))
	copy(gpus, availableGPUs)

	// Shuffle the slice
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := len(gpus) - 1; i > 0; i-- {
		j := rng.Intn(i + 1)
		gpus[i], gpus[j] = gpus[j], gpus[i]
	}

	return gpus[:requestedCount]
}

// selectGpuSequential selects GPUs in sequential order.
func (s *DeviceState) selectGpuSequential(availableGPUs []string, requestedCount int) []string {
	if len(availableGPUs) <= requestedCount {
		return availableGPUs
	}

	return availableGPUs[:requestedCount]
}

// discoverFabricTopologyWithFallback attempts fabric topology discovery with graceful degradation.
func (s *DeviceState) discoverFabricTopologyWithFallback(config *FallbackConfig) (*FabricTopology, error) {
	if config == nil {
		config = &DefaultFallbackConfig
	}

	var lastErr error
	for attempt := 1; attempt <= config.MaxRetries; attempt++ {
		topology, err := s.discoverFabricTopology()
		if err == nil {
			if attempt > 1 {
				klog.V(4).Infof("Fabric topology discovery succeeded on attempt %d", attempt)
			}
			return topology, nil
		}

		lastErr = err
		klog.V(4).Infof("Fabric topology discovery attempt %d failed: %v", attempt, err)

		if attempt < config.MaxRetries {
			klog.V(5).Infof("Retrying fabric topology discovery in %v", config.RetryDelay)
			time.Sleep(config.RetryDelay)
		}
	}

	klog.Warningf("Fabric topology discovery failed after %d attempts: %v", config.MaxRetries, lastErr)
	return nil, fmt.Errorf("fabric topology discovery failed after %d attempts: %w", config.MaxRetries, lastErr)
}

// selectOptimalGpuCombinationWithFallback selects optimal GPU combination with graceful degradation.
func (s *DeviceState) selectOptimalGpuCombinationWithFallback(requestedCount int, config *FabricOptimizationConfig, fallbackConfig *FallbackConfig) ([]string, error) {
	startTime := time.Now()
	defer func() {
		// Will record metrics at the end with actual results
	}()

	// Validate and normalize configuration
	var normalizedConfig *FabricOptimizationConfig
	if config != nil {
		var err error
		normalizedConfig, err = NormalizeFabricOptimizationConfig(config)
		if err != nil {
			GetFabricMetricsCollector().RecordOptimalSelection(time.Since(startTime), nil, nil, err)
			return nil, fmt.Errorf("invalid fabric optimization config: %w", err)
		}
		klog.V(5).Infof("Fabric optimization config validated and normalized: bandwidth=%.3f, latency=%.3f, direct=%.3f, hop=%.3f",
			normalizedConfig.ScoringWeights.Bandwidth, normalizedConfig.ScoringWeights.Latency,
			normalizedConfig.ScoringWeights.DirectConn, normalizedConfig.ScoringWeights.HopCount)
	} else {
		normalizedConfig = &DefaultFabricOptimizationConfig
		klog.V(5).Infof("Using default fabric optimization config")
	}

	// Try fabric topology discovery with fallback
	topology, err := s.discoverFabricTopologyWithFallback(fallbackConfig)
	if err != nil {
		// If fabric discovery fails, try fallback GPU selection
		klog.Warningf("Fabric topology discovery failed, attempting fallback GPU selection: %v", err)

		// Get available GPUs without fabric information
		availableGPUs := s.getAvailableGPUsWithoutFabric()

		if len(availableGPUs) < requestedCount {
			err := fmt.Errorf("insufficient GPUs available: need %d, have %d", requestedCount, len(availableGPUs))
			GetFabricMetricsCollector().RecordOptimalSelection(time.Since(startTime), nil, nil, err)
			return nil, err
		}

		// Use fallback strategy
		selectedGPUs, fallbackErr := s.selectGpuFallback(requestedCount, availableGPUs, fallbackConfig)
		if fallbackErr != nil {
			GetFabricMetricsCollector().RecordOptimalSelection(time.Since(startTime), nil, nil, fallbackErr)
			return nil, fmt.Errorf("fallback GPU selection failed: %w", fallbackErr)
		}

		klog.V(4).Infof("Fallback GPU selection completed: selected %v", selectedGPUs)
		GetFabricMetricsCollector().RecordOptimalSelection(time.Since(startTime), selectedGPUs, nil, nil)
		return selectedGPUs, nil
	}

	// Get all available GPUs with fabric information
	availableGPUs := make([]string, 0)
	for uuid, gpuInfo := range topology.GPUs {
		if s.isGpuAvailable(uuid) && s.isFabricManagerCapableFromFabricInfo(gpuInfo) {
			availableGPUs = append(availableGPUs, uuid)
		}
	}

	if len(availableGPUs) < requestedCount {
		err := fmt.Errorf("insufficient GPUs available: need %d, have %d", requestedCount, len(availableGPUs))
		GetFabricMetricsCollector().RecordOptimalSelection(time.Since(startTime), nil, nil, err)
		return nil, err
	}

	klog.V(4).Infof("Selecting optimal GPU combination from %d available GPUs for %d requested", len(availableGPUs), requestedCount)

	// Generate all possible combinations
	combinations := s.generateGpuCombinations(availableGPUs, requestedCount)

	if len(combinations) == 0 {
		err := fmt.Errorf("no valid GPU combinations found")
		GetFabricMetricsCollector().RecordOptimalSelection(time.Since(startTime), nil, nil, err)
		return nil, err
	}

	// Score each combination
	bestCombination := make([]string, 0)
	bestScore := float64(-1)
	var bestFabricScore *FabricScore
	filteredCount := 0

	for _, combination := range combinations {
		score := s.scoreGpuCombination(combination, topology)

		// Apply configuration thresholds
		if normalizedConfig != nil {
			if score.TotalBandwidth < normalizedConfig.MinBandwidthThreshold {
				klog.V(6).Infof("Skipping combination %v: bandwidth %d < threshold %d",
					combination, score.TotalBandwidth, normalizedConfig.MinBandwidthThreshold)
				filteredCount++
				continue
			}
			if score.AverageLatency > normalizedConfig.MaxLatencyThreshold {
				klog.V(6).Infof("Skipping combination %v: latency %d > threshold %d",
					combination, score.AverageLatency, normalizedConfig.MaxLatencyThreshold)
				filteredCount++
				continue
			}
		}

		if score.Score > bestScore {
			bestScore = score.Score
			bestCombination = combination
			bestFabricScore = score
		}
	}

	// Record filtering metrics
	if filteredCount > 0 {
		GetFabricMetricsCollector().RecordCombinationFiltering("performance thresholds", filteredCount)
	}

	if len(bestCombination) == 0 {
		// If no combination meets thresholds, try fallback
		klog.Warningf("No GPU combination meets performance thresholds, attempting fallback selection")

		selectedGPUs, fallbackErr := s.selectGpuFallback(requestedCount, availableGPUs, fallbackConfig)
		if fallbackErr != nil {
			err := fmt.Errorf("no GPU combination meets performance thresholds and fallback failed: %w", fallbackErr)
			GetFabricMetricsCollector().RecordOptimalSelection(time.Since(startTime), nil, nil, err)
			return nil, err
		}

		klog.V(4).Infof("Fallback GPU selection completed: selected %v", selectedGPUs)
		GetFabricMetricsCollector().RecordOptimalSelection(time.Since(startTime), selectedGPUs, nil, nil)
		return selectedGPUs, nil
	}

	klog.V(4).Infof("Selected optimal GPU combination %v with score %.3f", bestCombination, bestScore)

	// Record successful selection metrics
	GetFabricMetricsCollector().RecordOptimalSelection(time.Since(startTime), bestCombination, bestFabricScore, nil)
	return bestCombination, nil
}

// getAvailableGPUsWithoutFabric gets available GPUs without fabric information.
func (s *DeviceState) getAvailableGPUsWithoutFabric() []string {
	availableGPUs := make([]string, 0)
	for uuid, device := range s.allocatable {
		if device.Type() == GpuDeviceType && s.isGpuAvailable(uuid) {
			// Check if GPU is FabricManager capable without fabric topology
			if s.isFabricManagerCapable(device.Gpu) {
				availableGPUs = append(availableGPUs, uuid)
			}
		}
	}
	return availableGPUs
}
