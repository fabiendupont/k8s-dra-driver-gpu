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
	"sync"
	"time"

	"k8s.io/klog/v2"
)

// FabricOptimizationMetrics tracks performance metrics for fabric optimization operations.
type FabricOptimizationMetrics struct {
	// Operation counters
	TopologyDiscoveryCount        int64 `json:"topology_discovery_count"`
	TopologyDiscoveryErrors       int64 `json:"topology_discovery_errors"`
	GpuCombinationGenerationCount int64 `json:"gpu_combination_generation_count"`
	GpuScoringCount               int64 `json:"gpu_scoring_count"`
	OptimalSelectionCount         int64 `json:"optimal_selection_count"`
	OptimalSelectionErrors        int64 `json:"optimal_selection_errors"`

	// Performance metrics
	TopologyDiscoveryDuration time.Duration `json:"topology_discovery_duration"`
	GpuScoringDuration        time.Duration `json:"gpu_scoring_duration"`
	OptimalSelectionDuration  time.Duration `json:"optimal_selection_duration"`

	// Quality metrics
	AverageFabricScore    float64 `json:"average_fabric_score"`
	BestFabricScore       float64 `json:"best_fabric_score"`
	WorstFabricScore      float64 `json:"worst_fabric_score"`
	CombinationsEvaluated int64   `json:"combinations_evaluated"`
	CombinationsFiltered  int64   `json:"combinations_filtered"`

	// Resource utilization
	AverageGpuCount          float64 `json:"average_gpu_count"`
	AverageBandwidth         int64   `json:"average_bandwidth"`
	AverageLatency           int64   `json:"average_latency"`
	AverageDirectConnections float64 `json:"average_direct_connections"`

	// Timestamps
	LastTopologyDiscovery time.Time `json:"last_topology_discovery"`
	LastOptimalSelection  time.Time `json:"last_optimal_selection"`

	// Mutex for thread safety
	mu sync.RWMutex
}

// FabricOptimizationMetricsCollector manages metrics collection for fabric optimization.
type FabricOptimizationMetricsCollector struct {
	metrics *FabricOptimizationMetrics
}

// NewFabricOptimizationMetricsCollector creates a new metrics collector.
func NewFabricOptimizationMetricsCollector() *FabricOptimizationMetricsCollector {
	return &FabricOptimizationMetricsCollector{
		metrics: &FabricOptimizationMetrics{},
	}
}

// RecordTopologyDiscovery records metrics for fabric topology discovery.
func (c *FabricOptimizationMetricsCollector) RecordTopologyDiscovery(duration time.Duration, err error) {
	c.metrics.mu.Lock()
	defer c.metrics.mu.Unlock()

	c.metrics.TopologyDiscoveryCount++
	c.metrics.TopologyDiscoveryDuration += duration
	c.metrics.LastTopologyDiscovery = time.Now()

	if err != nil {
		c.metrics.TopologyDiscoveryErrors++
		klog.V(4).Infof("Fabric metrics: Topology discovery failed after %v: %v", duration, err)
	} else {
		klog.V(6).Infof("Fabric metrics: Topology discovery completed in %v", duration)
	}
}

// RecordGpuCombinationGeneration records metrics for GPU combination generation.
func (c *FabricOptimizationMetricsCollector) RecordGpuCombinationGeneration(count int) {
	c.metrics.mu.Lock()
	defer c.metrics.mu.Unlock()

	c.metrics.GpuCombinationGenerationCount++
	c.metrics.CombinationsEvaluated += int64(count)

	klog.V(6).Infof("Fabric metrics: Generated %d GPU combinations", count)
}

// RecordGpuScoring records metrics for GPU combination scoring.
func (c *FabricOptimizationMetricsCollector) RecordGpuScoring(duration time.Duration, score *FabricScore) {
	c.metrics.mu.Lock()
	defer c.metrics.mu.Unlock()

	c.metrics.GpuScoringCount++
	c.metrics.GpuScoringDuration += duration

	if score != nil {
		// Update quality metrics
		if c.metrics.BestFabricScore == 0 || score.Score > c.metrics.BestFabricScore {
			c.metrics.BestFabricScore = score.Score
		}
		if c.metrics.WorstFabricScore == 0 || score.Score < c.metrics.WorstFabricScore {
			c.metrics.WorstFabricScore = score.Score
		}

		// Update resource utilization metrics
		c.metrics.AverageBandwidth = (c.metrics.AverageBandwidth + score.TotalBandwidth) / 2
		c.metrics.AverageLatency = (c.metrics.AverageLatency + score.AverageLatency) / 2
		c.metrics.AverageDirectConnections = (c.metrics.AverageDirectConnections + float64(score.DirectConnections)) / 2
	}

	klog.V(7).Infof("Fabric metrics: GPU scoring completed in %v, score: %.3f", duration, score.Score)
}

// RecordOptimalSelection records metrics for optimal GPU selection.
func (c *FabricOptimizationMetricsCollector) RecordOptimalSelection(duration time.Duration, selectedGPUs []string, score *FabricScore, err error) {
	c.metrics.mu.Lock()
	defer c.metrics.mu.Unlock()

	c.metrics.OptimalSelectionCount++
	c.metrics.OptimalSelectionDuration += duration
	c.metrics.LastOptimalSelection = time.Now()

	if err != nil {
		c.metrics.OptimalSelectionErrors++
		klog.V(4).Infof("Fabric metrics: Optimal selection failed after %v: %v", duration, err)
	} else {
		c.metrics.AverageGpuCount = (c.metrics.AverageGpuCount + float64(len(selectedGPUs))) / 2
		if score != nil {
			c.metrics.AverageFabricScore = (c.metrics.AverageFabricScore + score.Score) / 2
		}
		klog.V(4).Infof("Fabric metrics: Optimal selection completed in %v, selected %d GPUs with score %.3f",
			duration, len(selectedGPUs), score.Score)
	}
}

// RecordCombinationFiltering records when combinations are filtered out.
func (c *FabricOptimizationMetricsCollector) RecordCombinationFiltering(reason string, count int) {
	c.metrics.mu.Lock()
	defer c.metrics.mu.Unlock()

	c.metrics.CombinationsFiltered += int64(count)
	klog.V(6).Infof("Fabric metrics: Filtered %d combinations due to: %s", count, reason)
}

// GetMetrics returns a copy of the current metrics.
func (c *FabricOptimizationMetricsCollector) GetMetrics() FabricOptimizationMetrics {
	c.metrics.mu.RLock()
	defer c.metrics.mu.RUnlock()

	// Return a copy without the mutex to avoid race conditions
	return FabricOptimizationMetrics{
		TopologyDiscoveryCount:        c.metrics.TopologyDiscoveryCount,
		TopologyDiscoveryErrors:       c.metrics.TopologyDiscoveryErrors,
		GpuCombinationGenerationCount: c.metrics.GpuCombinationGenerationCount,
		GpuScoringCount:               c.metrics.GpuScoringCount,
		OptimalSelectionCount:         c.metrics.OptimalSelectionCount,
		OptimalSelectionErrors:        c.metrics.OptimalSelectionErrors,
		TopologyDiscoveryDuration:     c.metrics.TopologyDiscoveryDuration,
		GpuScoringDuration:            c.metrics.GpuScoringDuration,
		OptimalSelectionDuration:      c.metrics.OptimalSelectionDuration,
		AverageFabricScore:            c.metrics.AverageFabricScore,
		BestFabricScore:               c.metrics.BestFabricScore,
		WorstFabricScore:              c.metrics.WorstFabricScore,
		CombinationsEvaluated:         c.metrics.CombinationsEvaluated,
		CombinationsFiltered:          c.metrics.CombinationsFiltered,
	}
}

// ResetMetrics resets all metrics to zero.
func (c *FabricOptimizationMetricsCollector) ResetMetrics() {
	c.metrics.mu.Lock()
	defer c.metrics.mu.Unlock()

	*c.metrics = FabricOptimizationMetrics{}
	klog.V(4).Infof("Fabric metrics: All metrics reset")
}

// LogMetricsSummary logs a summary of current metrics.
func (c *FabricOptimizationMetricsCollector) LogMetricsSummary() {
	metrics := c.GetMetrics()

	klog.V(4).Infof("Fabric Optimization Metrics Summary:")
	klog.V(4).Infof("  Topology Discovery: %d successful, %d errors, avg duration: %v",
		metrics.TopologyDiscoveryCount-metrics.TopologyDiscoveryErrors,
		metrics.TopologyDiscoveryErrors,
		metrics.TopologyDiscoveryDuration/time.Duration(metrics.TopologyDiscoveryCount))
	klog.V(4).Infof("  GPU Combinations: %d generated, %d evaluated, %d filtered",
		metrics.GpuCombinationGenerationCount,
		metrics.CombinationsEvaluated,
		metrics.CombinationsFiltered)
	klog.V(4).Infof("  GPU Scoring: %d operations, avg duration: %v",
		metrics.GpuScoringCount,
		metrics.GpuScoringDuration/time.Duration(metrics.GpuScoringCount))
	klog.V(4).Infof("  Optimal Selection: %d successful, %d errors, avg duration: %v",
		metrics.OptimalSelectionCount-metrics.OptimalSelectionErrors,
		metrics.OptimalSelectionErrors,
		metrics.OptimalSelectionDuration/time.Duration(metrics.OptimalSelectionCount))
	klog.V(4).Infof("  Quality Metrics: avg score %.3f, best %.3f, worst %.3f",
		metrics.AverageFabricScore, metrics.BestFabricScore, metrics.WorstFabricScore)
	klog.V(4).Infof("  Resource Utilization: avg %d GPUs, %d GB/s bandwidth, %d ns latency",
		int(metrics.AverageGpuCount), metrics.AverageBandwidth, metrics.AverageLatency)
}

// Global metrics collector instance.
var fabricMetricsCollector = NewFabricOptimizationMetricsCollector()

// GetFabricMetricsCollector returns the global metrics collector.
func GetFabricMetricsCollector() *FabricOptimizationMetricsCollector {
	return fabricMetricsCollector
}
