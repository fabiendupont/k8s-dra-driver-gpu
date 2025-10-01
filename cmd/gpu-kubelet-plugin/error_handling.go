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
	"strings"
)

// ErrorCategory represents the category of an error for better classification.
type ErrorCategory string

const (
	ErrorCategoryValidation    ErrorCategory = "validation"
	ErrorCategoryConfiguration ErrorCategory = "configuration"
	ErrorCategoryResource      ErrorCategory = "resource"
	ErrorCategoryFabric        ErrorCategory = "fabric"
	ErrorCategoryMIG           ErrorCategory = "mig"
	ErrorCategorySystem        ErrorCategory = "system"
	ErrorCategoryPermission    ErrorCategory = "permission"
)

// ActionableError represents an error with actionable suggestions.
type ActionableError struct {
	Category    ErrorCategory
	Message     string
	Suggestions []string
	Context     map[string]interface{}
}

func (e *ActionableError) Error() string {
	if len(e.Suggestions) == 0 {
		return e.Message
	}

	suggestions := strings.Join(e.Suggestions, "; ")
	return fmt.Sprintf("%s. Suggestions: %s", e.Message, suggestions)
}

// NewActionableError creates a new actionable error.
func NewActionableError(category ErrorCategory, message string, suggestions []string, context map[string]interface{}) *ActionableError {
	return &ActionableError{
		Category:    category,
		Message:     message,
		Suggestions: suggestions,
		Context:     context,
	}
}

// Error builders for common scenarios

// NewValidationError creates a validation error with suggestions.
func NewValidationError(message string, field string, value interface{}, suggestions []string) *ActionableError {
	context := map[string]interface{}{
		"field": field,
		"value": value,
	}
	return NewActionableError(ErrorCategoryValidation, message, suggestions, context)
}

// NewConfigurationError creates a configuration error with suggestions.
func NewConfigurationError(message string, configType string, suggestions []string) *ActionableError {
	context := map[string]interface{}{
		"config_type": configType,
	}
	return NewActionableError(ErrorCategoryConfiguration, message, suggestions, context)
}

// NewResourceError creates a resource error with suggestions.
func NewResourceError(message string, resourceType string, resourceID string, suggestions []string) *ActionableError {
	context := map[string]interface{}{
		"resource_type": resourceType,
		"resource_id":   resourceID,
	}
	return NewActionableError(ErrorCategoryResource, message, suggestions, context)
}

// NewFabricError creates a fabric-related error with suggestions.
func NewFabricError(message string, operation string, suggestions []string) *ActionableError {
	context := map[string]interface{}{
		"operation": operation,
	}
	return NewActionableError(ErrorCategoryFabric, message, suggestions, context)
}

// NewMIGError creates a MIG-related error with suggestions.
func NewMIGError(message string, operation string, suggestions []string) *ActionableError {
	context := map[string]interface{}{
		"operation": operation,
	}
	return NewActionableError(ErrorCategoryMIG, message, suggestions, context)
}

// Predefined error messages and suggestions

// MIG-related errors.
func NewMIGProfileNotFoundError(profile string, gpuUUID string) *ActionableError {
	return NewMIGError(
		fmt.Sprintf("MIG profile '%s' not found on GPU %s", profile, gpuUUID),
		"profile_selection",
		[]string{
			"Check available MIG profiles using 'nvidia-smi -L'",
			"Verify GPU supports MIG with 'nvidia-smi -q -d MIG'",
			"Ensure MIG is enabled on the GPU",
			"Consider using a different MIG profile or GPU",
		},
	)
}

func NewMIGInsufficientMemoryError(gpuUUID string, requiredGB int, availableGB int) *ActionableError {
	return NewMIGError(
		fmt.Sprintf("Insufficient GPU memory for MIG on GPU %s: need %dGB, have %dGB", gpuUUID, requiredGB, availableGB),
		"memory_validation",
		[]string{
			"Use a smaller MIG profile that requires less memory",
			"Check if other MIG instances are consuming GPU memory",
			"Consider using a GPU with more memory",
			"Verify GPU memory is not fragmented",
		},
	)
}

func NewMIGNotEnabledError(gpuUUID string) *ActionableError {
	return NewMIGError(
		fmt.Sprintf("MIG is not enabled on GPU %s", gpuUUID),
		"mig_enablement",
		[]string{
			"Enable MIG on the GPU using 'nvidia-smi -mig 1'",
			"Restart the GPU driver after enabling MIG",
			"Verify GPU architecture supports MIG (Ampere, Hopper, Blackwell)",
			"Check if GPU is in exclusive mode",
		},
	)
}

// Fabric-related errors.
func NewFabricTopologyDiscoveryError(reason string) *ActionableError {
	return NewFabricError(
		fmt.Sprintf("Fabric topology discovery failed: %s", reason),
		"topology_discovery",
		[]string{
			"Check NVLink connections between GPUs",
			"Verify GPU drivers are up to date",
			"Ensure GPUs are properly connected to the fabric",
			"Try enabling fallback GPU selection strategy",
			"Check system logs for hardware issues",
		},
	)
}

func NewFabricInsufficientBandwidthError(requiredGBps int, availableGBps int) *ActionableError {
	return NewFabricError(
		fmt.Sprintf("Insufficient fabric bandwidth: need %d GB/s, have %d GB/s", requiredGBps, availableGBps),
		"bandwidth_validation",
		[]string{
			"Use GPUs with higher bandwidth connections",
			"Reduce bandwidth requirements in configuration",
			"Check NVLink cable connections",
			"Consider using fewer GPUs with better connectivity",
		},
	)
}

func NewFabricHighLatencyError(maxLatencyNs int, actualLatencyNs int) *ActionableError {
	return NewFabricError(
		fmt.Sprintf("Fabric latency too high: max %d ns, actual %d ns", maxLatencyNs, actualLatencyNs),
		"latency_validation",
		[]string{
			"Use GPUs with direct NVLink connections",
			"Increase maximum latency threshold in configuration",
			"Check for fabric congestion or hardware issues",
			"Consider using fewer GPUs to reduce hop count",
		},
	)
}

// Configuration errors.
func NewScoringWeightsError(weights string, reason string) *ActionableError {
	return NewConfigurationError(
		fmt.Sprintf("Invalid scoring weights '%s': %s", weights, reason),
		"scoring_weights",
		[]string{
			"Ensure weights sum to 1.0",
			"Use values between 0.0 and 1.0",
			"Consider using predefined weight sets (balanced, high-bandwidth, low-latency)",
			"Check for typos in weight configuration",
		},
	)
}

func NewFeatureGateError(feature string, required bool) *ActionableError {
	action := "enable"
	if !required {
		action = "disable"
	}
	return NewConfigurationError(
		fmt.Sprintf("Feature gate '%s' must be %sd", feature, action),
		"feature_gate",
		[]string{
			fmt.Sprintf("Set --feature-gates=%s=%t in the driver configuration", feature, required),
			"Restart the driver after changing feature gates",
			"Check Kubernetes version compatibility",
			"Verify feature gate is available in current version",
		},
	)
}

// Resource errors.
func NewInsufficientGPUsError(required int, available int) *ActionableError {
	return NewResourceError(
		fmt.Sprintf("Insufficient GPUs: need %d, have %d", required, available),
		"gpu",
		"",
		[]string{
			"Add more GPUs to the cluster",
			"Reduce the number of GPUs requested",
			"Check if GPUs are available and not allocated to other workloads",
			"Verify GPU drivers are properly installed",
		},
	)
}

func NewGPUNotAvailableError(gpuUUID string, reason string) *ActionableError {
	return NewResourceError(
		fmt.Sprintf("GPU %s is not available: %s", gpuUUID, reason),
		"gpu",
		gpuUUID,
		[]string{
			"Check if GPU is allocated to another workload",
			"Verify GPU is not in error state",
			"Check GPU driver and firmware status",
			"Consider using a different GPU",
		},
	)
}

// System errors.
func NewSystemError(operation string, reason string) *ActionableError {
	return NewActionableError(
		ErrorCategorySystem,
		fmt.Sprintf("System error during %s: %s", operation, reason),
		[]string{
			"Check system logs for detailed error information",
			"Verify system resources (memory, disk space)",
			"Restart the driver if the issue persists",
			"Contact system administrator for hardware issues",
		},
		map[string]interface{}{
			"operation": operation,
			"reason":    reason,
		},
	)
}

// Permission errors.
func NewPermissionError(operation string, resource string) *ActionableError {
	return NewActionableError(
		ErrorCategoryPermission,
		fmt.Sprintf("Permission denied for %s on %s", operation, resource),
		[]string{
			"Check if the driver has necessary permissions",
			"Verify the driver is running with appropriate privileges",
			"Check SELinux or AppArmor policies",
			"Ensure the driver has access to GPU devices",
		},
		map[string]interface{}{
			"operation": operation,
			"resource":  resource,
		},
	)
}

// Helper functions for common error scenarios

// WrapError wraps an existing error with actionable suggestions.
func WrapError(err error, category ErrorCategory, suggestions []string) *ActionableError {
	if actionableErr, ok := err.(*ActionableError); ok {
		// If it's already an actionable error, add suggestions
		actionableErr.Suggestions = append(actionableErr.Suggestions, suggestions...)
		return actionableErr
	}

	return NewActionableError(
		category,
		err.Error(),
		suggestions,
		map[string]interface{}{
			"original_error": err.Error(),
		},
	)
}

// GetErrorSuggestions returns suggestions for a given error category.
func GetErrorSuggestions(category ErrorCategory) []string {
	switch category {
	case ErrorCategoryValidation:
		return []string{
			"Check input parameters for correctness",
			"Verify data types and formats",
			"Ensure required fields are provided",
		}
	case ErrorCategoryConfiguration:
		return []string{
			"Review configuration file syntax",
			"Check for typos in configuration values",
			"Verify configuration is compatible with current version",
		}
	case ErrorCategoryResource:
		return []string{
			"Check resource availability",
			"Verify resource permissions",
			"Consider alternative resources",
		}
	case ErrorCategoryFabric:
		return []string{
			"Check NVLink connections",
			"Verify fabric topology",
			"Consider fallback strategies",
		}
	case ErrorCategoryMIG:
		return []string{
			"Check MIG configuration",
			"Verify GPU MIG capabilities",
			"Review MIG profile settings",
		}
	case ErrorCategorySystem:
		return []string{
			"Check system logs",
			"Verify system resources",
			"Contact system administrator",
		}
	case ErrorCategoryPermission:
		return []string{
			"Check user permissions",
			"Verify driver privileges",
			"Review security policies",
		}
	default:
		return []string{
			"Check logs for more details",
			"Verify system configuration",
			"Contact support if issue persists",
		}
	}
}
