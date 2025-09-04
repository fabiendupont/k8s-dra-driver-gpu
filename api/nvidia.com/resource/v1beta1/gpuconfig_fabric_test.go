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

package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestGpuConfigWithFabricTopology(t *testing.T) {
	config := &GpuConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: GroupName + "/" + Version,
			Kind:       GpuConfigKind,
		},
		FabricTopologyConfig: &FabricTopologyConfig{
			Strategy:      FabricTopologyAuto,
			PartitionSize: ptr.To(FabricPartitionSize4),
		},
	}

	if config.FabricTopologyConfig == nil {
		t.Fatal("Expected FabricTopologyConfig to be set")
	}

	if config.FabricTopologyConfig.Strategy != FabricTopologyAuto {
		t.Errorf("Expected strategy 'Auto', got '%s'", config.FabricTopologyConfig.Strategy)
	}

	if config.FabricTopologyConfig.PartitionSize == nil || *config.FabricTopologyConfig.PartitionSize != FabricPartitionSize4 {
		t.Errorf("Expected partition size 4, got %v", config.FabricTopologyConfig.PartitionSize)
	}
}

func TestGpuConfigValidateWithFabricTopology(t *testing.T) {
	tests := []struct {
		name    string
		config  *GpuConfig
		wantErr bool
	}{
		{
			name: "Valid config with fabric topology",
			config: &GpuConfig{
				FabricTopologyConfig: &FabricTopologyConfig{
					Strategy:      FabricTopologyAuto,
					PartitionSize: ptr.To(FabricPartitionSize4),
				},
			},
			wantErr: false,
		},
		{
			name: "Valid config with sharing and fabric topology",
			config: &GpuConfig{
				Sharing: &GpuSharing{
					Strategy: TimeSlicingStrategy,
					TimeSlicingConfig: &TimeSlicingConfig{
						Interval: ptr.To(DefaultTimeSlice),
					},
				},
				FabricTopologyConfig: &FabricTopologyConfig{
					Strategy:      FabricTopologyAuto,
					PartitionSize: ptr.To(FabricPartitionSize4),
				},
			},
			wantErr: true, // TimeSlicing feature gate is not enabled by default
		},
		{
			name: "Invalid fabric topology config",
			config: &GpuConfig{
				FabricTopologyConfig: &FabricTopologyConfig{
					Strategy:      "Invalid",
					PartitionSize: ptr.To(FabricPartitionSize4),
				},
			},
			wantErr: true,
		},
		{
			name: "Invalid sharing config",
			config: &GpuConfig{
				Sharing: &GpuSharing{
					Strategy: "Invalid",
				},
				FabricTopologyConfig: &FabricTopologyConfig{
					Strategy:      FabricTopologyAuto,
					PartitionSize: ptr.To(FabricPartitionSize4),
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("GpuConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGpuConfigNormalizeWithFabricTopology(t *testing.T) {
	// This test would require mocking the feature gate system
	// For now, we'll test the basic structure
	config := &GpuConfig{
		FabricTopologyConfig: &FabricTopologyConfig{
			Strategy: FabricTopologyAuto,
		},
	}

	err := config.Normalize()
	if err != nil {
		t.Errorf("GpuConfig.Normalize() error = %v", err)
	}

	if config.FabricTopologyConfig == nil {
		t.Fatal("Expected FabricTopologyConfig to be preserved after normalization")
	}

	if config.FabricTopologyConfig.Strategy != FabricTopologyAuto {
		t.Errorf("Expected strategy 'Auto', got '%s'", config.FabricTopologyConfig.Strategy)
	}
}

func TestGpuConfigValidateSharingFabricCompatibility(t *testing.T) {
	// Test the compatibility validation method directly
	testCases := []struct {
		name                string
		sharingStrategy     string
		fabricStrategy      string
		expectError         bool
		expectedErrorSubstr string
	}{
		{
			name:                "MPS with Auto fabric topology - incompatible",
			sharingStrategy:     MpsStrategy,
			fabricStrategy:      FabricTopologyAuto,
			expectError:         true,
			expectedErrorSubstr: "MPS sharing strategy is not compatible with fabric topology optimization",
		},
		{
			name:                "MPS with Manual fabric topology - incompatible",
			sharingStrategy:     MpsStrategy,
			fabricStrategy:      FabricTopologyManual,
			expectError:         true,
			expectedErrorSubstr: "MPS sharing strategy is not compatible with fabric topology optimization",
		},
		{
			name:            "MPS with Disabled fabric topology - compatible",
			sharingStrategy: MpsStrategy,
			fabricStrategy:  FabricTopologyDisabled,
			expectError:     false,
		},
		{
			name:            "TimeSlicing with Auto fabric topology - compatible",
			sharingStrategy: TimeSlicingStrategy,
			fabricStrategy:  FabricTopologyAuto,
			expectError:     false,
		},
		{
			name:            "TimeSlicing with Manual fabric topology - compatible",
			sharingStrategy: TimeSlicingStrategy,
			fabricStrategy:  FabricTopologyManual,
			expectError:     false,
		},
		{
			name:            "TimeSlicing with Disabled fabric topology - compatible",
			sharingStrategy: TimeSlicingStrategy,
			fabricStrategy:  FabricTopologyDisabled,
			expectError:     false,
		},
		{
			name:            "No sharing with Auto fabric topology - compatible",
			sharingStrategy: "",
			fabricStrategy:  FabricTopologyAuto,
			expectError:     false,
		},
		{
			name:            "No sharing with Disabled fabric topology - compatible",
			sharingStrategy: "",
			fabricStrategy:  FabricTopologyDisabled,
			expectError:     false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := &GpuConfig{}

			// Set sharing strategy if specified
			if tc.sharingStrategy != "" {
				config.Sharing = &GpuSharing{
					Strategy: GpuSharingStrategy(tc.sharingStrategy),
				}
			}

			// Set fabric topology strategy
			config.FabricTopologyConfig = &FabricTopologyConfig{
				Strategy: FabricTopologyStrategy(tc.fabricStrategy),
			}

			// Test the compatibility validation directly
			err := config.validateSharingFabricCompatibility()

			if tc.expectError {
				assert.Error(t, err)
				if tc.expectedErrorSubstr != "" {
					assert.Contains(t, err.Error(), tc.expectedErrorSubstr)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGpuConfigValidateSharingFabricCompatibilityEdgeCases(t *testing.T) {
	t.Run("Nil sharing config with fabric topology", func(t *testing.T) {
		config := &GpuConfig{
			FabricTopologyConfig: &FabricTopologyConfig{
				Strategy: FabricTopologyAuto,
			},
		}

		err := config.validateSharingFabricCompatibility()
		assert.NoError(t, err)
	})

	t.Run("Nil fabric topology config with sharing", func(t *testing.T) {
		config := &GpuConfig{
			Sharing: &GpuSharing{
				Strategy: GpuSharingStrategy(MpsStrategy),
			},
		}

		err := config.validateSharingFabricCompatibility()
		assert.NoError(t, err)
	})

	t.Run("Both configs nil", func(t *testing.T) {
		config := &GpuConfig{}

		err := config.validateSharingFabricCompatibility()
		assert.NoError(t, err)
	})

	t.Run("Fabric topology disabled with MPS - should be compatible", func(t *testing.T) {
		config := &GpuConfig{
			Sharing: &GpuSharing{
				Strategy: GpuSharingStrategy(MpsStrategy),
			},
			FabricTopologyConfig: &FabricTopologyConfig{
				Strategy: FabricTopologyDisabled,
			},
		}

		err := config.validateSharingFabricCompatibility()
		assert.NoError(t, err)
	})
}
