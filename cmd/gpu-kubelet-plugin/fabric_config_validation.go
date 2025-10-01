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
	"math"
)

// ScoringWeightsValidationError represents validation errors for scoring weights.
type ScoringWeightsValidationError struct {
	Field   string
	Value   float64
	Message string
}

func (e *ScoringWeightsValidationError) Error() string {
	return fmt.Sprintf("scoring weights validation failed for field '%s' (value: %.3f): %s", e.Field, e.Value, e.Message)
}

// ValidateScoringWeights validates scoring weights configuration.
func ValidateScoringWeights(weights *ScoringWeights) error {
	if weights == nil {
		return &ScoringWeightsValidationError{
			Field:   "weights",
			Value:   0,
			Message: "scoring weights cannot be nil",
		}
	}

	// Check individual weight values
	if err := validateWeightRange("Bandwidth", weights.Bandwidth); err != nil {
		return err
	}
	if err := validateWeightRange("Latency", weights.Latency); err != nil {
		return err
	}
	if err := validateWeightRange("DirectConn", weights.DirectConn); err != nil {
		return err
	}
	if err := validateWeightRange("HopCount", weights.HopCount); err != nil {
		return err
	}

	// Check that weights sum to approximately 1.0 (within tolerance)
	total := weights.Bandwidth + weights.Latency + weights.DirectConn + weights.HopCount
	tolerance := 0.01 // 1% tolerance
	if math.Abs(total-1.0) > tolerance {
		return &ScoringWeightsValidationError{
			Field:   "total",
			Value:   total,
			Message: fmt.Sprintf("weights must sum to 1.0 (within %.3f tolerance), got %.3f", tolerance, total),
		}
	}

	// Check for reasonable weight distribution
	if weights.Bandwidth < 0.1 {
		return &ScoringWeightsValidationError{
			Field:   "Bandwidth",
			Value:   weights.Bandwidth,
			Message: "bandwidth weight should be at least 0.1 (10%) for meaningful optimization",
		}
	}

	if weights.Latency < 0.1 {
		return &ScoringWeightsValidationError{
			Field:   "Latency",
			Value:   weights.Latency,
			Message: "latency weight should be at least 0.1 (10%) for meaningful optimization",
		}
	}

	// Check for extreme weight imbalances
	maxWeight := math.Max(math.Max(weights.Bandwidth, weights.Latency), math.Max(weights.DirectConn, weights.HopCount))
	if maxWeight > 0.8 {
		return &ScoringWeightsValidationError{
			Field:   "max_weight",
			Value:   maxWeight,
			Message: fmt.Sprintf("no single weight should exceed 0.8 (80%%), max weight is %.3f", maxWeight),
		}
	}

	return nil
}

// validateWeightRange validates that a weight is within valid range [0, 1].
func validateWeightRange(field string, weight float64) error {
	if weight < 0 {
		return &ScoringWeightsValidationError{
			Field:   field,
			Value:   weight,
			Message: "weight cannot be negative",
		}
	}
	if weight > 1 {
		return &ScoringWeightsValidationError{
			Field:   field,
			Value:   weight,
			Message: "weight cannot exceed 1.0",
		}
	}
	if math.IsNaN(weight) {
		return &ScoringWeightsValidationError{
			Field:   field,
			Value:   weight,
			Message: "weight cannot be NaN",
		}
	}
	if math.IsInf(weight, 0) {
		return &ScoringWeightsValidationError{
			Field:   field,
			Value:   weight,
			Message: "weight cannot be infinite",
		}
	}
	return nil
}

// NormalizeScoringWeights normalizes scoring weights to sum to 1.0.
func NormalizeScoringWeights(weights *ScoringWeights) *ScoringWeights {
	if weights == nil {
		return &DefaultScoringWeights
	}

	total := weights.Bandwidth + weights.Latency + weights.DirectConn + weights.HopCount
	if total == 0 {
		// If all weights are zero, use defaults
		return &DefaultScoringWeights
	}

	return &ScoringWeights{
		Bandwidth:  weights.Bandwidth / total,
		Latency:    weights.Latency / total,
		DirectConn: weights.DirectConn / total,
		HopCount:   weights.HopCount / total,
	}
}

// ValidateFabricOptimizationConfig validates fabric optimization configuration.
func ValidateFabricOptimizationConfig(config *FabricOptimizationConfig) error {
	if config == nil {
		return fmt.Errorf("fabric optimization config cannot be nil")
	}

	// Validate scoring weights
	if err := ValidateScoringWeights(&config.ScoringWeights); err != nil {
		return fmt.Errorf("invalid scoring weights: %w", err)
	}

	// Validate thresholds
	if config.MinBandwidthThreshold < 0 {
		return fmt.Errorf("min bandwidth threshold cannot be negative: %d", config.MinBandwidthThreshold)
	}

	if config.MaxLatencyThreshold < 0 {
		return fmt.Errorf("max latency threshold cannot be negative: %d", config.MaxLatencyThreshold)
	}

	// Validate reasonable threshold ranges
	if config.MinBandwidthThreshold > 10000 { // 10 TB/s seems excessive
		return fmt.Errorf("min bandwidth threshold seems too high: %d GB/s", config.MinBandwidthThreshold)
	}

	if config.MaxLatencyThreshold > 100000 { // 100 μs seems excessive
		return fmt.Errorf("max latency threshold seems too high: %d ns", config.MaxLatencyThreshold)
	}

	return nil
}

// NormalizeFabricOptimizationConfig normalizes and validates fabric optimization configuration.
func NormalizeFabricOptimizationConfig(config *FabricOptimizationConfig) (*FabricOptimizationConfig, error) {
	if config == nil {
		return &DefaultFabricOptimizationConfig, nil
	}

	// Create a copy to avoid modifying the original
	normalized := *config

	// Normalize scoring weights
	normalized.ScoringWeights = *NormalizeScoringWeights(&config.ScoringWeights)

	// Validate the normalized config
	if err := ValidateFabricOptimizationConfig(&normalized); err != nil {
		return nil, fmt.Errorf("normalized config validation failed: %w", err)
	}

	return &normalized, nil
}

// GetRecommendedScoringWeights returns recommended scoring weights for different use cases.
func GetRecommendedScoringWeights(useCase string) *ScoringWeights {
	switch useCase {
	case "high-bandwidth":
		return &ScoringWeights{
			Bandwidth:  0.6, // Prioritize bandwidth
			Latency:    0.2,
			DirectConn: 0.15,
			HopCount:   0.05,
		}
	case "low-latency":
		return &ScoringWeights{
			Bandwidth:  0.2,
			Latency:    0.6, // Prioritize low latency
			DirectConn: 0.15,
			HopCount:   0.05,
		}
	case "balanced":
		return &ScoringWeights{
			Bandwidth:  0.4,
			Latency:    0.3,
			DirectConn: 0.2,
			HopCount:   0.1,
		}
	case "direct-connections":
		return &ScoringWeights{
			Bandwidth:  0.3,
			Latency:    0.2,
			DirectConn: 0.4, // Prioritize direct connections
			HopCount:   0.1,
		}
	default:
		return &DefaultScoringWeights
	}
}
