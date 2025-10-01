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

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configapi "github.com/NVIDIA/k8s-dra-driver-gpu/api/nvidia.com/resource/v1beta1"

	nvdev "github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
)

func TestSelectMigProfileDynamic(t *testing.T) {
	tests := []struct {
		name            string
		gpu             *GpuInfo
		config          *configapi.MigDeviceConfig
		expectedProfile string
		expectError     bool
	}{
		{
			name: "no profiles available",
			gpu: &GpuInfo{
				UUID:         "gpu-uuid",
				architecture: "Ampere",
				migEnabled:   true,
				migProfiles:  []*MigProfileInfo{},
			},
			config:          configapi.DefaultMigDeviceConfig(),
			expectedProfile: "",
			expectError:     false,
		},
		{
			name: "single profile available",
			gpu: &GpuInfo{
				UUID:         "gpu-uuid",
				architecture: "Ampere",
				migEnabled:   true,
				migProfiles: []*MigProfileInfo{
					createMockMigProfile("1g.5gb", 1, 5),
				},
			},
			config:          configapi.DefaultMigDeviceConfig(),
			expectedProfile: "1g.5gb",
			expectError:     false,
		},
		{
			name: "multiple profiles - should select smallest",
			gpu: &GpuInfo{
				UUID:         "gpu-uuid",
				architecture: "Ampere",
				migEnabled:   true,
				memoryBytes:  80 * 1024 * 1024 * 1024, // 80GB
				migProfiles: []*MigProfileInfo{
					createMockMigProfile("1g.5gb", 1, 5),
					createMockMigProfile("2g.10gb", 2, 10),
					createMockMigProfile("4g.20gb", 4, 20),
				},
			},
			config:          configapi.DefaultMigDeviceConfig(),
			expectedProfile: "1g.5gb", // Should select smallest profile
			expectError:     false,
		},
		{
			name: "Hopper GPU with multiple profiles",
			gpu: &GpuInfo{
				UUID:         "gpu-hopper-uuid",
				architecture: "Hopper",
				migEnabled:   true,
				memoryBytes:  80 * 1024 * 1024 * 1024, // 80GB
				migProfiles: []*MigProfileInfo{
					createMockMigProfile("1g.10gb", 1, 10),
					createMockMigProfile("2g.20gb", 2, 20),
					createMockMigProfile("4g.40gb", 4, 40),
				},
			},
			config:          configapi.DefaultMigDeviceConfig(),
			expectedProfile: "1g.10gb", // Should select smallest profile
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deviceState := &DeviceState{}

			profile := deviceState.selectMigProfile(tt.gpu, tt.config)

			if tt.expectError {
				assert.Empty(t, profile)
			} else {
				assert.Equal(t, tt.expectedProfile, profile)
			}
		})
	}
}

func TestCalculateProfileScoreDynamic(t *testing.T) {
	tests := []struct {
		name          string
		profile       *MigProfileInfo
		gpu           *GpuInfo
		config        *configapi.MigDeviceConfig
		expectedScore int
	}{
		{
			name:    "Ampere GPU - smaller profile gets higher score",
			profile: createMockMigProfile("1g.5gb", 1, 5),
			gpu: &GpuInfo{
				UUID:         "gpu-uuid",
				architecture: "Ampere",
				memoryBytes:  80 * 1024 * 1024 * 1024, // 80GB
			},
			config:        configapi.DefaultMigDeviceConfig(),
			expectedScore: 210, // (7-1)*10 + (80-5)*2 + 0*5 = 60 + 150 + 0 = 210
		},
		{
			name:    "Hopper GPU - smaller profile gets higher score",
			profile: createMockMigProfile("1g.10gb", 1, 10),
			gpu: &GpuInfo{
				UUID:         "gpu-hopper-uuid",
				architecture: "Hopper",
				memoryBytes:  80 * 1024 * 1024 * 1024, // 80GB
			},
			config:        configapi.DefaultMigDeviceConfig(),
			expectedScore: 210, // (8-1)*10 + (80-10)*2 + 0*5 = 70 + 140 + 0 = 210
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deviceState := &DeviceState{}

			score := deviceState.calculateProfileScore(tt.profile, tt.gpu, tt.config)

			// We expect smaller profiles to have higher scores
			assert.Greater(t, score, 0)
			// The exact score depends on the calculation logic
			assert.Equal(t, tt.expectedScore, score)
		})
	}
}

func TestCreateMigDevicesDynamic(t *testing.T) {
	tests := []struct {
		name            string
		gpu             *GpuInfo
		config          *configapi.MigDeviceConfig
		claim           *resourceapi.ResourceClaim
		expectedDevices int
		expectError     bool
		expectedProfile string
	}{
		{
			name: "valid MIG device creation",
			gpu: &GpuInfo{
				UUID:         "gpu-uuid",
				architecture: "Ampere",
				migEnabled:   true,
				memoryBytes:  80 * 1024 * 1024 * 1024, // 80GB
				migProfiles: []*MigProfileInfo{
					createMockMigProfile("1g.5gb", 1, 5),
				},
			},
			config: configapi.DefaultMigDeviceConfig(),
			claim: &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					UID:       "claim-uid",
					Name:      "test-claim",
					Namespace: "default",
				},
			},
			expectedDevices: 1,
			expectError:     false,
			expectedProfile: "1g.5gb",
		},
		{
			name: "GPU not MIG-enabled should fail",
			gpu: &GpuInfo{
				UUID:         "gpu-uuid",
				architecture: "Ampere",
				migEnabled:   false,
				memoryBytes:  80 * 1024 * 1024 * 1024, // 80GB
				migProfiles: []*MigProfileInfo{
					createMockMigProfile("1g.5gb", 1, 5),
				},
			},
			config: configapi.DefaultMigDeviceConfig(),
			claim: &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					UID:       "claim-uid",
					Name:      "test-claim",
					Namespace: "default",
				},
			},
			expectedDevices: 0,
			expectError:     true,
		},
		{
			name: "unsupported architecture should fail",
			gpu: &GpuInfo{
				UUID:         "gpu-uuid",
				architecture: "Volta",
				migEnabled:   true,
				memoryBytes:  80 * 1024 * 1024 * 1024, // 80GB
				migProfiles: []*MigProfileInfo{
					createMockMigProfile("1g.5gb", 1, 5),
				},
			},
			config: configapi.DefaultMigDeviceConfig(),
			claim: &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					UID:       "claim-uid",
					Name:      "test-claim",
					Namespace: "default",
				},
			},
			expectedDevices: 0,
			expectError:     true,
		},
		{
			name: "nil claim should fail",
			gpu: &GpuInfo{
				UUID:         "gpu-uuid",
				architecture: "Ampere",
				migEnabled:   true,
				memoryBytes:  80 * 1024 * 1024 * 1024, // 80GB
				migProfiles: []*MigProfileInfo{
					createMockMigProfile("1g.5gb", 1, 5),
				},
			},
			config:          configapi.DefaultMigDeviceConfig(),
			claim:           nil,
			expectedDevices: 0,
			expectError:     true,
		},
		{
			name: "Hopper GPU with MIG enabled",
			gpu: &GpuInfo{
				UUID:         "gpu-hopper-uuid",
				architecture: "Hopper",
				migEnabled:   true,
				memoryBytes:  80 * 1024 * 1024 * 1024, // 80GB
				migProfiles: []*MigProfileInfo{
					createMockMigProfile("1g.10gb", 1, 10),
				},
			},
			config: configapi.DefaultMigDeviceConfig(),
			claim: &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					UID:       "claim-uid",
					Name:      "test-claim",
					Namespace: "default",
				},
			},
			expectedDevices: 1,
			expectError:     false,
			expectedProfile: "1g.10gb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deviceState := &DeviceState{}

			devices, err := deviceState.createMigDevices(tt.gpu, tt.config, tt.claim)

			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, devices)
			} else {
				require.NoError(t, err)
				require.NotNil(t, devices)
				assert.Len(t, devices, tt.expectedDevices)

				if tt.expectedDevices > 0 {
					assert.Equal(t, tt.gpu.UUID, devices[0].ParentUUID)
					assert.Equal(t, tt.expectedProfile, devices[0].Profile)
					assert.Equal(t, 0, devices[0].Placement.Start)
					assert.Equal(t, 1, devices[0].Placement.Size)
				}
			}
		})
	}
}

// Helper function to create mock MIG profiles for testing.
func createMockMigProfile(name string, computeSlices int, memoryGB int) *MigProfileInfo {
	// For testing purposes, we'll create a simple mock that implements the required interface
	// This is a simplified version that focuses on testing the logic without full nvdev integration

	// Create a mock profile info that matches what the code expects
	profileInfo := nvml.GpuInstanceProfileInfo{
		Id:                  0, // Mock ID
		IsP2pSupported:      0,
		SliceCount:          uint32(computeSlices),
		InstanceCount:       0,
		MultiprocessorCount: 0,
		CopyEngineCount:     0,
		DecoderCount:        0,
		EncoderCount:        0,
		JpegCount:           0,
		OfaCount:            0,
		MemorySizeMB:        uint64(memoryGB * 1024), // Convert GB to MB
	}

	// Create a simple mock that implements the MigProfile interface
	mockProfile := &mockMigProfile{
		name: name,
		info: profileInfo,
	}

	return &MigProfileInfo{
		profile:    mockProfile,
		placements: []*MigDevicePlacement{}, // Empty placements for simplicity
	}
}

// mockMigProfile implements nvdev.MigProfile interface for testing.
type mockMigProfile struct {
	name string
	info nvml.GpuInstanceProfileInfo
}

func (m *mockMigProfile) String() string {
	return m.name
}

func (m *mockMigProfile) GetInfo() nvdev.MigProfileInfo {
	return nvdev.MigProfileInfo{
		GIProfileID:    0,
		CIProfileID:    0,
		CIEngProfileID: 0,
		C:              int(m.info.SliceCount),
		G:              int(m.info.SliceCount),          // Assume G == C for simplicity
		GB:             int(m.info.MemorySizeMB / 1024), // Convert MB to GB
	}
}

func (m *mockMigProfile) Matches(profile string) bool {
	return m.name == profile
}

func (m *mockMigProfile) Equals(other nvdev.MigProfile) bool {
	return m.String() == other.String()
}
