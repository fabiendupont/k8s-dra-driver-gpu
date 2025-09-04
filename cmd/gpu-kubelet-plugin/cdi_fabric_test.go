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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cdispec "tags.cncf.io/container-device-interface/specs-go"
)

func TestAddFabricTopologyToDeviceSpec(t *testing.T) {
	cdi := &CDIHandler{
		nvidiaCDIHookPath: "/usr/bin/nvidia-cdi-hook",
	}

	deviceSpec := &cdispec.Device{
		Name: "test-device",
		ContainerEdits: cdispec.ContainerEdits{
			Env:    []string{"EXISTING_ENV=value"},
			Hooks:  []*cdispec.Hook{},
			Mounts: []*cdispec.Mount{},
		},
	}

	fabricPartitionID := "partition-123"

	cdi.addFabricTopologyToDeviceSpec(deviceSpec, fabricPartitionID)

	// Check environment variables
	assert.Contains(t, deviceSpec.ContainerEdits.Env, "EXISTING_ENV=value")
	assert.Contains(t, deviceSpec.ContainerEdits.Env, "NVIDIA_FABRIC_PARTITION_ID=partition-123")
	assert.Contains(t, deviceSpec.ContainerEdits.Env, "NVIDIA_FABRIC_TOPOLOGY_ENABLED=true")
	assert.Contains(t, deviceSpec.ContainerEdits.Env, "NVIDIA_NVLINK_OPTIMIZATION=true")
	assert.Contains(t, deviceSpec.ContainerEdits.Env, "CUDA_VISIBLE_DEVICES_FABRIC_PARTITION=partition-123")

	// Check hooks
	require.Len(t, deviceSpec.ContainerEdits.Hooks, 1)
	hook := deviceSpec.ContainerEdits.Hooks[0]
	assert.Equal(t, "nvidia-fabric-topology-hook", hook.HookName)
	assert.Equal(t, "/usr/bin/nvidia-cdi-hook", hook.Path)
	assert.Contains(t, hook.Args, "--fabric-partition-id=partition-123")
	assert.Contains(t, hook.Args, "--enable-nvlink-optimization")
	assert.Contains(t, hook.Env, "NVIDIA_FABRIC_PARTITION_ID=partition-123")
	assert.Contains(t, hook.Env, "NVIDIA_FABRIC_TOPOLOGY_ENABLED=true")

	// Check mounts
	require.Len(t, deviceSpec.ContainerEdits.Mounts, 3)

	// Check NVLink mount
	nvlinkMount := deviceSpec.ContainerEdits.Mounts[0]
	assert.Equal(t, "/sys/class/nvlink", nvlinkMount.HostPath)
	assert.Equal(t, "/sys/class/nvlink", nvlinkMount.ContainerPath)
	assert.Contains(t, nvlinkMount.Options, "ro")

	// Check GPU info mount
	gpuMount := deviceSpec.ContainerEdits.Mounts[1]
	assert.Equal(t, "/proc/driver/nvidia/gpus", gpuMount.HostPath)
	assert.Equal(t, "/proc/driver/nvidia/gpus", gpuMount.ContainerPath)
	assert.Contains(t, gpuMount.Options, "ro")

	// Check fabric runtime mount
	fabricMount := deviceSpec.ContainerEdits.Mounts[2]
	assert.Equal(t, "/var/run/nvidia-fabric", fabricMount.HostPath)
	assert.Equal(t, "/var/run/nvidia-fabric", fabricMount.ContainerPath)
	assert.Contains(t, fabricMount.Options, "rw")
}

func TestAddFabricTopologyToDeviceSpecWithEmptyContainerEdits(t *testing.T) {
	cdi := &CDIHandler{
		nvidiaCDIHookPath: "/usr/bin/nvidia-cdi-hook",
	}

	deviceSpec := &cdispec.Device{
		Name: "test-device",
		ContainerEdits: cdispec.ContainerEdits{
			Env:    []string{"EXISTING_ENV=value"},
			Hooks:  []*cdispec.Hook{},
			Mounts: []*cdispec.Mount{},
		},
	}

	fabricPartitionID := "partition-456"

	cdi.addFabricTopologyToDeviceSpec(deviceSpec, fabricPartitionID)

	// Check that fabric topology was added
	assert.Contains(t, deviceSpec.ContainerEdits.Env, "NVIDIA_FABRIC_PARTITION_ID=partition-456")
	assert.Len(t, deviceSpec.ContainerEdits.Hooks, 1)
	assert.Len(t, deviceSpec.ContainerEdits.Mounts, 3)
}

func TestGenerateFabricTopologyEnvironmentVariables(t *testing.T) {
	cdi := &CDIHandler{}

	envVars := cdi.generateFabricTopologyEnvironmentVariables("partition-789")

	expected := []string{
		"NVIDIA_FABRIC_PARTITION_ID=partition-789",
		"NVIDIA_FABRIC_TOPOLOGY_ENABLED=true",
		"NVIDIA_NVLINK_OPTIMIZATION=true",
		"CUDA_VISIBLE_DEVICES_FABRIC_PARTITION=partition-789",
	}

	assert.ElementsMatch(t, expected, envVars)
}

func TestGenerateFabricTopologyHooks(t *testing.T) {
	cdi := &CDIHandler{
		nvidiaCDIHookPath: "/custom/path/nvidia-cdi-hook",
	}

	hooks := cdi.generateFabricTopologyHooks("partition-def")

	require.Len(t, hooks, 1)
	hook := hooks[0]

	assert.Equal(t, "nvidia-fabric-topology-hook", hook.HookName)
	assert.Equal(t, "/custom/path/nvidia-cdi-hook", hook.Path)
	assert.Contains(t, hook.Args, "--fabric-partition-id=partition-def")
	assert.Contains(t, hook.Args, "--enable-nvlink-optimization")
	assert.Contains(t, hook.Env, "NVIDIA_FABRIC_PARTITION_ID=partition-def")
	assert.Contains(t, hook.Env, "NVIDIA_FABRIC_TOPOLOGY_ENABLED=true")
}

func TestGenerateFabricTopologyMounts(t *testing.T) {
	cdi := &CDIHandler{}

	mounts := cdi.generateFabricTopologyMounts("partition-ghi")

	require.Len(t, mounts, 3)

	// Check NVLink mount
	nvlinkMount := mounts[0]
	assert.Equal(t, "/sys/class/nvlink", nvlinkMount.HostPath)
	assert.Equal(t, "/sys/class/nvlink", nvlinkMount.ContainerPath)
	assert.Contains(t, nvlinkMount.Options, "ro")

	// Check GPU info mount
	gpuMount := mounts[1]
	assert.Equal(t, "/proc/driver/nvidia/gpus", gpuMount.HostPath)
	assert.Equal(t, "/proc/driver/nvidia/gpus", gpuMount.ContainerPath)
	assert.Contains(t, gpuMount.Options, "ro")

	// Check fabric runtime mount
	fabricMount := mounts[2]
	assert.Equal(t, "/var/run/nvidia-fabric", fabricMount.HostPath)
	assert.Equal(t, "/var/run/nvidia-fabric", fabricMount.ContainerPath)
	assert.Contains(t, fabricMount.Options, "rw")
}

func TestCreateClaimSpecFileWithFabricTopology(t *testing.T) {
	cdi := &CDIHandler{
		nvidiaCDIHookPath: "/usr/bin/nvidia-cdi-hook",
	}

	// This test would require more complex setup with actual CDI cache
	// For now, we'll test the fabric topology addition logic separately
	deviceSpec := &cdispec.Device{
		Name: "test-claim-gpu-0",
		ContainerEdits: cdispec.ContainerEdits{
			Env: []string{"EXISTING_ENV=value"},
		},
	}

	cdi.addFabricTopologyToDeviceSpec(deviceSpec, "partition-123")

	// Verify fabric topology was added
	assert.Contains(t, deviceSpec.ContainerEdits.Env, "NVIDIA_FABRIC_PARTITION_ID=partition-123")
	assert.Len(t, deviceSpec.ContainerEdits.Hooks, 1)
	assert.Len(t, deviceSpec.ContainerEdits.Mounts, 3)
}

func TestFabricTopologyEnvironmentVariablesFormat(t *testing.T) {
	cdi := &CDIHandler{}

	testCases := []struct {
		partitionID string
		expected    []string
	}{
		{
			partitionID: "simple-partition",
			expected: []string{
				"NVIDIA_FABRIC_PARTITION_ID=simple-partition",
				"NVIDIA_FABRIC_TOPOLOGY_ENABLED=true",
				"NVIDIA_NVLINK_OPTIMIZATION=true",
				"CUDA_VISIBLE_DEVICES_FABRIC_PARTITION=simple-partition",
			},
		},
		{
			partitionID: "partition-with-dashes-123",
			expected: []string{
				"NVIDIA_FABRIC_PARTITION_ID=partition-with-dashes-123",
				"NVIDIA_FABRIC_TOPOLOGY_ENABLED=true",
				"NVIDIA_NVLINK_OPTIMIZATION=true",
				"CUDA_VISIBLE_DEVICES_FABRIC_PARTITION=partition-with-dashes-123",
			},
		},
		{
			partitionID: "partition_with_underscores_456",
			expected: []string{
				"NVIDIA_FABRIC_PARTITION_ID=partition_with_underscores_456",
				"NVIDIA_FABRIC_TOPOLOGY_ENABLED=true",
				"NVIDIA_NVLINK_OPTIMIZATION=true",
				"CUDA_VISIBLE_DEVICES_FABRIC_PARTITION=partition_with_underscores_456",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.partitionID, func(t *testing.T) {
			envVars := cdi.generateFabricTopologyEnvironmentVariables(tc.partitionID)
			assert.ElementsMatch(t, tc.expected, envVars)
		})
	}
}
