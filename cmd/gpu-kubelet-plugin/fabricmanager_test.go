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

func TestValidateGpuForFabricManagerCreation(t *testing.T) {
	tests := []struct {
		name    string
		gpu     *GpuInfo
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil GPU",
			gpu:     nil,
			wantErr: true,
			errMsg:  "GPU cannot be nil",
		},
		{
			name: "unsupported architecture",
			gpu: &GpuInfo{
				UUID:         "gpu-volta-uuid",
				architecture: "Volta",
			},
			wantErr: true,
			errMsg:  "GPU gpu-volta-uuid does not support FabricManager",
		},
		{
			name: "Ampere MIG-enabled GPU",
			gpu: &GpuInfo{
				UUID:         "gpu-a100-uuid",
				architecture: "Ampere",
				migEnabled:   true,
			},
			wantErr: true,
			errMsg:  "GPU gpu-a100-uuid is MIG-enabled and cannot participate in fabric on Ampere architecture",
		},
		{
			name: "valid Ampere GPU",
			gpu: &GpuInfo{
				UUID:         "gpu-a100-uuid",
				architecture: "Ampere",
				migEnabled:   false,
			},
			wantErr: false,
		},
		{
			name: "valid Hopper GPU",
			gpu: &GpuInfo{
				UUID:         "gpu-h100-uuid",
				architecture: "Hopper",
				migEnabled:   false,
			},
			wantErr: false,
		},
		{
			name: "valid Hopper MIG-enabled GPU",
			gpu: &GpuInfo{
				UUID:         "gpu-h100-uuid",
				architecture: "Hopper",
				migEnabled:   true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal DeviceState for testing
			deviceState := &DeviceState{}

			err := deviceState.validateGpuForFabricManagerCreation(tt.gpu)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestIsFabricManagerCapable(t *testing.T) {
	tests := []struct {
		name         string
		architecture string
		expected     bool
	}{
		{
			name:         "Ampere",
			architecture: "Ampere",
			expected:     true,
		},
		{
			name:         "Hopper",
			architecture: "Hopper",
			expected:     true,
		},
		{
			name:         "Blackwell",
			architecture: "Blackwell",
			expected:     true,
		},
		{
			name:         "Volta",
			architecture: "Volta",
			expected:     false,
		},
		{
			name:         "Unknown",
			architecture: "Unknown",
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gpu := &GpuInfo{architecture: tt.architecture}

			// Create a minimal DeviceState for testing
			deviceState := &DeviceState{}

			result := deviceState.isFabricManagerCapable(gpu)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsAmpereArchitecture(t *testing.T) {
	tests := []struct {
		name         string
		architecture string
		expected     bool
	}{
		{
			name:         "Ampere",
			architecture: "Ampere",
			expected:     true,
		},
		{
			name:         "Hopper",
			architecture: "Hopper",
			expected:     false,
		},
		{
			name:         "Blackwell",
			architecture: "Blackwell",
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gpu := &GpuInfo{architecture: tt.architecture}

			// Create a minimal DeviceState for testing
			deviceState := &DeviceState{}

			result := deviceState.isAmpereArchitecture(gpu)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsHopperArchitecture(t *testing.T) {
	tests := []struct {
		name         string
		architecture string
		expected     bool
	}{
		{
			name:         "Ampere",
			architecture: "Ampere",
			expected:     false,
		},
		{
			name:         "Hopper",
			architecture: "Hopper",
			expected:     true,
		},
		{
			name:         "Blackwell",
			architecture: "Blackwell",
			expected:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gpu := &GpuInfo{architecture: tt.architecture}

			// Create a minimal DeviceState for testing
			deviceState := &DeviceState{}

			result := deviceState.isHopperArchitecture(gpu)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateFabricManagerConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *configapi.FabricManagerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
			errMsg:  "FabricManager config cannot be nil",
		},
		{
			name: "valid config with no sharing",
			config: &configapi.FabricManagerConfig{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "nvidia.com/resource/v1beta1",
					Kind:       "FabricManagerConfig",
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with exclusive sharing",
			config: &configapi.FabricManagerConfig{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "nvidia.com/resource/v1beta1",
					Kind:       "FabricManagerConfig",
				},
				Spec: configapi.FabricManagerConfigSpec{
					Sharing: &configapi.FabricManagerSharing{
						Strategy: configapi.FabricManagerSharingStrategyExclusive,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid sharing strategy",
			config: &configapi.FabricManagerConfig{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "nvidia.com/resource/v1beta1",
					Kind:       "FabricManagerConfig",
				},
				Spec: configapi.FabricManagerConfigSpec{
					Sharing: &configapi.FabricManagerSharing{
						Strategy: "InvalidStrategy",
					},
				},
			},
			wantErr: true,
			errMsg:  "invalid sharing strategy: InvalidStrategy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal DeviceState for testing
			deviceState := &DeviceState{}

			err := deviceState.validateFabricManagerConfig(tt.config)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateResourceClaimForFabricManager(t *testing.T) {
	tests := []struct {
		name    string
		claim   *resourceapi.ResourceClaim
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil claim",
			claim:   nil,
			wantErr: true,
			errMsg:  "resource claim cannot be nil",
		},
		{
			name: "empty UID",
			claim: &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-claim",
					Namespace: "default",
					UID:       "",
				},
			},
			wantErr: true,
			errMsg:  "resource claim UID cannot be empty",
		},
		{
			name: "empty name",
			claim: &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "",
					Namespace: "default",
					UID:       "test-uid",
				},
			},
			wantErr: true,
			errMsg:  "resource claim name cannot be empty",
		},
		{
			name: "empty namespace",
			claim: &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-claim",
					Namespace: "",
					UID:       "test-uid",
				},
			},
			wantErr: true,
			errMsg:  "resource claim namespace cannot be empty",
		},
		{
			name: "valid claim",
			claim: &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-claim",
					Namespace: "default",
					UID:       "test-uid",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal DeviceState for testing
			deviceState := &DeviceState{}

			err := deviceState.validateResourceClaimForFabricManager(tt.claim)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateAllocatedFabricPartition(t *testing.T) {
	tests := []struct {
		name      string
		partition AllocatedFabricPartition
		index     int
		wantErr   bool
		errMsg    string
	}{
		{
			name: "empty parent UUID",
			partition: AllocatedFabricPartition{
				ParentUUID:    "",
				PartitionName: "test-partition",
				CliqueID:      "0",
			},
			index:   0,
			wantErr: true,
			errMsg:  "parent UUID cannot be empty",
		},
		{
			name: "empty partition name",
			partition: AllocatedFabricPartition{
				ParentUUID:    "gpu-uuid",
				PartitionName: "",
				CliqueID:      "0",
			},
			index:   0,
			wantErr: true,
			errMsg:  "partition name cannot be empty",
		},
		{
			name: "empty clique ID",
			partition: AllocatedFabricPartition{
				ParentUUID:    "gpu-uuid",
				PartitionName: "test-partition",
				CliqueID:      "",
			},
			index:   0,
			wantErr: true,
			errMsg:  "clique ID cannot be empty",
		},
		{
			name: "valid partition",
			partition: AllocatedFabricPartition{
				ParentUUID:    "gpu-uuid",
				PartitionName: "test-partition",
				CliqueID:      "0",
			},
			index:   0,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal DeviceState for testing
			deviceState := &DeviceState{}

			err := deviceState.validateAllocatedFabricPartition(tt.partition, tt.index)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
