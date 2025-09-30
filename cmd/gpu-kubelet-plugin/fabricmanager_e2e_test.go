/*
 * Copyright (c) 2025, NVIDIA CORPORATION. All rights reserved.
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
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configapi "github.com/NVIDIA/k8s-dra-driver-gpu/api/nvidia.com/resource/v1beta1"
)

func TestFabricManagerEndToEnd(t *testing.T) {
	tests := []struct {
		name               string
		gpu                *GpuInfo
		config             *configapi.FabricManagerConfig
		claim              *resourceapi.ResourceClaim
		expectedError      bool
		expectedPartitions int
	}{
		{
			name: "valid Ampere GPU with FabricManager config",
			gpu: &GpuInfo{
				UUID:         "gpu-a100-uuid",
				architecture: "Ampere",
				migEnabled:   false,
				memoryBytes:  80 * 1024 * 1024 * 1024, // 80GB
			},
			config: &configapi.FabricManagerConfig{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "nvidia.com/resource/v1beta1",
					Kind:       "FabricManagerConfig",
				},
				Spec: configapi.FabricManagerConfigSpec{
					PartitionName: "test-partition",
					CliqueID:      "0",
					Sharing: &configapi.FabricManagerSharing{
						Strategy: configapi.FabricManagerSharingStrategyExclusive,
					},
				},
			},
			claim: &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-claim",
					Namespace: "default",
					UID:       "test-claim-uid",
				},
			},
			expectedError:      false,
			expectedPartitions: 1,
		},
		{
			name: "valid Hopper GPU with FabricManager config",
			gpu: &GpuInfo{
				UUID:         "gpu-h100-uuid",
				architecture: "Hopper",
				migEnabled:   false,
				memoryBytes:  80 * 1024 * 1024 * 1024, // 80GB
			},
			config: &configapi.FabricManagerConfig{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "nvidia.com/resource/v1beta1",
					Kind:       "FabricManagerConfig",
				},
				Spec: configapi.FabricManagerConfigSpec{
					PartitionName: "hopper-partition",
					CliqueID:      "1",
					Sharing: &configapi.FabricManagerSharing{
						Strategy: configapi.FabricManagerSharingStrategyExclusive,
					},
				},
			},
			claim: &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "hopper-claim",
					Namespace: "default",
					UID:       "hopper-claim-uid",
				},
			},
			expectedError:      false,
			expectedPartitions: 1,
		},
		{
			name: "Ampere MIG-enabled GPU should fail",
			gpu: &GpuInfo{
				UUID:         "gpu-a100-mig-uuid",
				architecture: "Ampere",
				migEnabled:   true,
				memoryBytes:  80 * 1024 * 1024 * 1024, // 80GB
			},
			config: &configapi.FabricManagerConfig{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "nvidia.com/resource/v1beta1",
					Kind:       "FabricManagerConfig",
				},
				Spec: configapi.FabricManagerConfigSpec{
					PartitionName: "mig-partition",
					CliqueID:      "0",
				},
			},
			claim: &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mig-claim",
					Namespace: "default",
					UID:       "mig-claim-uid",
				},
			},
			expectedError:      true,
			expectedPartitions: 0,
		},
		{
			name: "Hopper MIG-enabled GPU should succeed",
			gpu: &GpuInfo{
				UUID:         "gpu-h100-mig-uuid",
				architecture: "Hopper",
				migEnabled:   true,
				memoryBytes:  80 * 1024 * 1024 * 1024, // 80GB
			},
			config: &configapi.FabricManagerConfig{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "nvidia.com/resource/v1beta1",
					Kind:       "FabricManagerConfig",
				},
				Spec: configapi.FabricManagerConfigSpec{
					PartitionName: "hopper-mig-partition",
					CliqueID:      "1",
				},
			},
			claim: &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "hopper-mig-claim",
					Namespace: "default",
					UID:       "hopper-mig-claim-uid",
				},
			},
			expectedError:      false,
			expectedPartitions: 1,
		},
		{
			name: "unsupported architecture should fail",
			gpu: &GpuInfo{
				UUID:         "gpu-volta-uuid",
				architecture: "Volta",
				migEnabled:   false,
				memoryBytes:  16 * 1024 * 1024 * 1024, // 16GB
			},
			config: &configapi.FabricManagerConfig{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "nvidia.com/resource/v1beta1",
					Kind:       "FabricManagerConfig",
				},
				Spec: configapi.FabricManagerConfigSpec{
					PartitionName: "volta-partition",
					CliqueID:      "0",
				},
			},
			claim: &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "volta-claim",
					Namespace: "default",
					UID:       "volta-claim-uid",
				},
			},
			expectedError:      true,
			expectedPartitions: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal DeviceState for testing
			deviceState := &DeviceState{
				allocatable: map[string]*AllocatableDevice{
					tt.gpu.UUID: {
						Gpu: tt.gpu,
					},
				},
			}

			// Test fabric partition creation (validation only, no actual creation)
			allocatedPartitions, err := deviceState.createFabricManagerDevices(tt.gpu, tt.config, tt.claim)
			if tt.expectedError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, allocatedPartitions, tt.expectedPartitions)

			// Test container edits generation (without actual preparation)
			if tt.expectedPartitions > 0 {
				// Create a mock prepared partitions structure
				preparedPartitions := &PreparedFabricPartitions{
					Partitions: []*FabricPartitionInfo{
						{
							PartitionID:   0,
							PartitionName: tt.config.Spec.PartitionName,
							ParentUUID:    tt.gpu.UUID,
							CliqueID:      tt.config.Spec.CliqueID,
							State:         "active",
						},
					},
				}

				// Test container edits generation
				containerEdits, err := deviceState.generateFabricPartitionContainerEdits(preparedPartitions)
				require.NoError(t, err)
				require.NotNil(t, containerEdits)
				assert.NotEmpty(t, containerEdits.ContainerEdits.Env)
				assert.NotEmpty(t, containerEdits.ContainerEdits.DeviceNodes)
			}
		})
	}
}

func TestFabricManagerConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *configapi.FabricManagerConfig
		expectError bool
	}{
		{
			name: "valid config with exclusive sharing",
			config: &configapi.FabricManagerConfig{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "nvidia.com/resource/v1beta1",
					Kind:       "FabricManagerConfig",
				},
				Spec: configapi.FabricManagerConfigSpec{
					PartitionName: "test-partition",
					CliqueID:      "0",
					Sharing: &configapi.FabricManagerSharing{
						Strategy: configapi.FabricManagerSharingStrategyExclusive,
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid config without sharing",
			config: &configapi.FabricManagerConfig{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "nvidia.com/resource/v1beta1",
					Kind:       "FabricManagerConfig",
				},
				Spec: configapi.FabricManagerConfigSpec{
					PartitionName: "test-partition",
					CliqueID:      "0",
				},
			},
			expectError: false,
		},
		{
			name: "config with invalid sharing strategy",
			config: &configapi.FabricManagerConfig{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "nvidia.com/resource/v1beta1",
					Kind:       "FabricManagerConfig",
				},
				Spec: configapi.FabricManagerConfigSpec{
					PartitionName: "test-partition",
					CliqueID:      "0",
					Sharing: &configapi.FabricManagerSharing{
						Strategy: "InvalidStrategy",
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test validation before normalization
			err := tt.config.Validate()
			if tt.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Test normalization
			err = tt.config.Normalize()
			require.NoError(t, err)

			// Test validation after normalization
			err = tt.config.Validate()
			require.NoError(t, err)
		})
	}
}

func TestFabricManagerConfigApplication(t *testing.T) {
	// Create a test config
	testConfig := &configapi.FabricManagerConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "nvidia.com/resource/v1beta1",
			Kind:       "FabricManagerConfig",
		},
		Spec: configapi.FabricManagerConfigSpec{
			PartitionName: "test-partition",
			CliqueID:      "0",
		},
	}

	// Test that the config can be validated and normalized
	err := testConfig.Normalize()
	require.NoError(t, err)

	err = testConfig.Validate()
	require.NoError(t, err)

	// Test that the config has the expected values after normalization
	assert.Equal(t, "test-partition", testConfig.Spec.PartitionName)
	assert.Equal(t, "0", testConfig.Spec.CliqueID)
	assert.NotNil(t, testConfig.Spec.Sharing)
	assert.Equal(t, configapi.FabricManagerSharingStrategyExclusive, testConfig.Spec.Sharing.Strategy)
}

func TestFabricManagerDefaultConfig(t *testing.T) {
	// Test default config creation
	defaultConfig := configapi.DefaultFabricManagerConfig()
	require.NotNil(t, defaultConfig)
	assert.Equal(t, "resource.nvidia.com/v1beta1", defaultConfig.APIVersion)
	assert.Equal(t, "FabricManagerConfig", defaultConfig.Kind)

	// Test normalization
	err := defaultConfig.Normalize()
	require.NoError(t, err)

	// Test validation
	err = defaultConfig.Validate()
	require.NoError(t, err)
}
