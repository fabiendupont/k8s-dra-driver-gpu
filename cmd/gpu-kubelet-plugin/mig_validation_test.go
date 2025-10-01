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

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateGpuArchitectureForMig(t *testing.T) {
	tests := []struct {
		name         string
		architecture string
		wantErr      bool
		errMsg       string
	}{
		{
			name:         "empty architecture",
			architecture: "",
			wantErr:      true,
			errMsg:       "GPU architecture is not specified",
		},
		{
			name:         "unsupported architecture",
			architecture: "Volta",
			wantErr:      true,
			errMsg:       "GPU architecture Volta does not support MIG",
		},
		{
			name:         "supported Ampere",
			architecture: "Ampere",
			wantErr:      false,
		},
		{
			name:         "supported Hopper",
			architecture: "Hopper",
			wantErr:      false,
		},
		{
			name:         "supported Blackwell",
			architecture: "Blackwell",
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gpu := &GpuInfo{architecture: tt.architecture}

			// Create a minimal DeviceState for testing
			deviceState := &DeviceState{}

			err := deviceState.validateGpuArchitectureForMig(gpu)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateGpuMemoryForMig(t *testing.T) {
	tests := []struct {
		name         string
		architecture string
		memoryBytes  uint64
		wantErr      bool
		errMsg       string
	}{
		{
			name:         "zero memory",
			architecture: "Ampere",
			memoryBytes:  0,
			wantErr:      true,
			errMsg:       "GPU memory size is not specified",
		},
		{
			name:         "insufficient memory for Ampere",
			architecture: "Ampere",
			memoryBytes:  10 * 1024 * 1024 * 1024, // 10GB
			wantErr:      true,
			errMsg:       "GPU memory 10737418240 bytes is below minimum required 42949672960 bytes for MIG on Ampere architecture",
		},
		{
			name:         "sufficient memory for Ampere",
			architecture: "Ampere",
			memoryBytes:  40 * 1024 * 1024 * 1024, // 40GB
			wantErr:      false,
		},
		{
			name:         "insufficient memory for Hopper",
			architecture: "Hopper",
			memoryBytes:  10 * 1024 * 1024 * 1024, // 10GB
			wantErr:      true,
			errMsg:       "GPU memory 10737418240 bytes is below minimum required 85899345920 bytes for MIG on Hopper architecture",
		},
		{
			name:         "sufficient memory for Hopper",
			architecture: "Hopper",
			memoryBytes:  80 * 1024 * 1024 * 1024, // 80GB
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gpu := &GpuInfo{
				architecture: tt.architecture,
				memoryBytes:  tt.memoryBytes,
			}

			// Create a minimal DeviceState for testing
			deviceState := &DeviceState{}

			err := deviceState.validateGpuMemoryForMig(gpu)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateMigPlacement(t *testing.T) {
	tests := []struct {
		name         string
		placement    *nvml.GpuInstancePlacement
		architecture string
		wantErr      bool
		errMsg       string
	}{
		{
			name:         "nil placement",
			placement:    nil,
			architecture: "Ampere",
			wantErr:      true,
			errMsg:       "placement cannot be nil",
		},
		{
			name:         "zero size",
			placement:    &nvml.GpuInstancePlacement{Start: 0, Size: 0},
			architecture: "Ampere",
			wantErr:      true,
			errMsg:       "placement size 0 must be positive",
		},
		{
			name:         "placement exceeds maximum slices for Ampere",
			placement:    &nvml.GpuInstancePlacement{Start: 0, Size: 8},
			architecture: "Ampere",
			wantErr:      true,
			errMsg:       "placement range [0, 8) exceeds maximum slices 7",
		},
		{
			name:         "valid placement for Ampere",
			placement:    &nvml.GpuInstancePlacement{Start: 0, Size: 1},
			architecture: "Ampere",
			wantErr:      false,
		},
		{
			name:         "valid placement for Hopper",
			placement:    &nvml.GpuInstancePlacement{Start: 0, Size: 8},
			architecture: "Hopper",
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gpu := &GpuInfo{architecture: tt.architecture}

			// Create a minimal DeviceState for testing
			deviceState := &DeviceState{}

			err := deviceState.validateMigPlacement(tt.placement, gpu)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateAllocatedMigDevice(t *testing.T) {
	tests := []struct {
		name    string
		device  AllocatedMigDevice
		index   int
		wantErr bool
		errMsg  string
	}{
		{
			name: "empty parent UUID",
			device: AllocatedMigDevice{
				ParentUUID: "",
				Profile:    "1g.5gb",
				Placement: struct {
					Start int `json:"start"`
					Size  int `json:"size"`
				}{Start: 0, Size: 1},
			},
			index:   0,
			wantErr: true,
			errMsg:  "parent UUID cannot be empty",
		},
		{
			name: "empty profile",
			device: AllocatedMigDevice{
				ParentUUID: "test-gpu-uuid",
				Profile:    "",
				Placement: struct {
					Start int `json:"start"`
					Size  int `json:"size"`
				}{Start: 0, Size: 1},
			},
			index:   0,
			wantErr: true,
			errMsg:  "profile cannot be empty",
		},
		{
			name: "negative placement start",
			device: AllocatedMigDevice{
				ParentUUID: "test-gpu-uuid",
				Profile:    "1g.5gb",
				Placement: struct {
					Start int `json:"start"`
					Size  int `json:"size"`
				}{Start: -1, Size: 1},
			},
			index:   0,
			wantErr: true,
			errMsg:  "placement start must be non-negative",
		},
		{
			name: "zero placement size",
			device: AllocatedMigDevice{
				ParentUUID: "test-gpu-uuid",
				Profile:    "1g.5gb",
				Placement: struct {
					Start int `json:"start"`
					Size  int `json:"size"`
				}{Start: 0, Size: 0},
			},
			index:   0,
			wantErr: true,
			errMsg:  "placement size must be positive",
		},
		{
			name: "valid device",
			device: AllocatedMigDevice{
				ParentUUID: "test-gpu-uuid",
				Profile:    "1g.5gb",
				Placement: struct {
					Start int `json:"start"`
					Size  int `json:"size"`
				}{Start: 0, Size: 1},
			},
			index:   0,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal DeviceState for testing
			deviceState := &DeviceState{}

			err := deviceState.validateAllocatedMigDevice(tt.device, tt.index)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetMinMemoryForMig(t *testing.T) {
	tests := []struct {
		name         string
		architecture string
		expected     uint64
	}{
		{
			name:         "Ampere",
			architecture: "Ampere",
			expected:     40 * 1024 * 1024 * 1024, // 40GB
		},
		{
			name:         "Hopper",
			architecture: "Hopper",
			expected:     80 * 1024 * 1024 * 1024, // 80GB
		},
		{
			name:         "Blackwell",
			architecture: "Blackwell",
			expected:     80 * 1024 * 1024 * 1024, // 80GB
		},
		{
			name:         "Unknown architecture",
			architecture: "Unknown",
			expected:     40 * 1024 * 1024 * 1024, // Default to 40GB
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal DeviceState for testing
			deviceState := &DeviceState{}

			result := deviceState.getMinMemoryForMig(tt.architecture)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetMaxComputeSlices(t *testing.T) {
	tests := []struct {
		name         string
		architecture string
		expected     int
	}{
		{
			name:         "Ampere",
			architecture: "Ampere",
			expected:     7,
		},
		{
			name:         "Hopper",
			architecture: "Hopper",
			expected:     8,
		},
		{
			name:         "Blackwell",
			architecture: "Blackwell",
			expected:     8,
		},
		{
			name:         "Unknown architecture",
			architecture: "Unknown",
			expected:     7, // Default to A100-like behavior
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gpu := &GpuInfo{architecture: tt.architecture}

			// Create a minimal DeviceState for testing
			deviceState := &DeviceState{}

			result := deviceState.getMaxComputeSlices(gpu)
			assert.Equal(t, tt.expected, result)
		})
	}
}
