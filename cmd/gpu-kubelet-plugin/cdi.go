/*
 * Copyright (c) 2022-2023, NVIDIA CORPORATION.  All rights reserved.
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
	"fmt"
	"io"

	"github.com/sirupsen/logrus"

	nvdevice "github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi"
	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi/spec"
	transformroot "github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi/transform/root"
	"k8s.io/klog/v2"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdiparser "tags.cncf.io/container-device-interface/pkg/parser"
	cdispec "tags.cncf.io/container-device-interface/specs-go"

	configapi "github.com/NVIDIA/k8s-dra-driver-gpu/api/nvidia.com/resource/v1beta1"
)

const (
	cdiVendor = "k8s." + DriverName

	cdiDeviceClass = "device"
	cdiDeviceKind  = cdiVendor + "/" + cdiDeviceClass
	cdiClaimClass  = "claim"
	cdiClaimKind   = cdiVendor + "/" + cdiClaimClass

	cdiBaseSpecIdentifier = "base"

	defaultCDIRoot = "/var/run/cdi"
)

type CDIHandler struct {
	logger            *logrus.Logger
	nvml              nvml.Interface
	nvdevice          nvdevice.Interface
	nvcdiDevice       nvcdi.Interface
	nvcdiClaim        nvcdi.Interface
	cache             *cdiapi.Cache
	driverRoot        string
	devRoot           string
	targetDriverRoot  string
	nvidiaCDIHookPath string

	cdiRoot     string
	vendor      string
	deviceClass string
	claimClass  string
}

func NewCDIHandler(opts ...cdiOption) (*CDIHandler, error) {
	h := &CDIHandler{}
	for _, opt := range opts {
		opt(h)
	}

	if h.logger == nil {
		h.logger = logrus.New()
		h.logger.SetOutput(io.Discard)
	}
	if h.nvml == nil {
		h.nvml = nvml.New()
	}
	if h.cdiRoot == "" {
		h.cdiRoot = defaultCDIRoot
	}
	if h.nvdevice == nil {
		h.nvdevice = nvdevice.New(h.nvml)
	}
	if h.vendor == "" {
		h.vendor = cdiVendor
	}
	if h.deviceClass == "" {
		h.deviceClass = cdiDeviceClass
	}
	if h.claimClass == "" {
		h.claimClass = cdiClaimClass
	}
	if h.nvcdiDevice == nil {
		nvcdilib, err := nvcdi.New(
			nvcdi.WithDeviceLib(h.nvdevice),
			nvcdi.WithDriverRoot(h.driverRoot),
			nvcdi.WithDevRoot(h.devRoot),
			nvcdi.WithLogger(h.logger),
			nvcdi.WithNvmlLib(h.nvml),
			nvcdi.WithMode("nvml"),
			nvcdi.WithVendor(h.vendor),
			nvcdi.WithClass(h.deviceClass),
			nvcdi.WithNVIDIACDIHookPath(h.nvidiaCDIHookPath),
		)
		if err != nil {
			return nil, fmt.Errorf("unable to create CDI library for devices: %w", err)
		}
		h.nvcdiDevice = nvcdilib
	}
	if h.nvcdiClaim == nil {
		nvcdilib, err := nvcdi.New(
			nvcdi.WithDeviceLib(h.nvdevice),
			nvcdi.WithDriverRoot(h.driverRoot),
			nvcdi.WithDevRoot(h.devRoot),
			nvcdi.WithLogger(h.logger),
			nvcdi.WithNvmlLib(h.nvml),
			nvcdi.WithMode("nvml"),
			nvcdi.WithVendor(h.vendor),
			nvcdi.WithClass(h.claimClass),
			nvcdi.WithNVIDIACDIHookPath(h.nvidiaCDIHookPath),
		)
		if err != nil {
			return nil, fmt.Errorf("unable to create CDI library for claims: %w", err)
		}
		h.nvcdiClaim = nvcdilib
	}
	if h.cache == nil {
		cache, err := cdiapi.NewCache(
			cdiapi.WithSpecDirs(h.cdiRoot),
		)
		if err != nil {
			return nil, fmt.Errorf("unable to create a new CDI cache: %w", err)
		}
		h.cache = cache
	}

	return h, nil
}

func (cdi *CDIHandler) CreateStandardDeviceSpecFile(allocatable AllocatableDevices) error {
	// Initialize NVML in order to get the device edits.
	if r := cdi.nvml.Init(); r != nvml.SUCCESS {
		return fmt.Errorf("failed to initialize NVML: %v", r)
	}
	defer func() {
		if r := cdi.nvml.Shutdown(); r != nvml.SUCCESS {
			klog.Warningf("failed to shutdown NVML: %v", r)
		}
	}()

	// Generate the set of common edits.
	commonEdits, err := cdi.nvcdiDevice.GetCommonEdits()
	if err != nil {
		return fmt.Errorf("failed to get common CDI spec edits: %w", err)
	}

	// Make sure that NVIDIA_VISIBLE_DEVICES is set to void to avoid the
	// nvidia-container-runtime honoring it in addition to the underlying
	// runtime honoring CDI.
	commonEdits.Env = append(
		commonEdits.Env,
		"NVIDIA_VISIBLE_DEVICES=void")

	// Generate device specs for all full GPUs and MIG devices.
	var deviceSpecs []cdispec.Device
	for _, device := range allocatable {
		dspecs, err := cdi.nvcdiDevice.GetDeviceSpecsByID(device.CanonicalIndex())
		if err != nil {
			return fmt.Errorf("unable to get device spec for %s: %w", device.CanonicalName(), err)
		}
		dspecs[0].Name = device.CanonicalName()
		deviceSpecs = append(deviceSpecs, dspecs[0])
	}

	// Generate base spec from commonEdits and deviceEdits.
	spec, err := spec.New(
		spec.WithVendor(cdiVendor),
		spec.WithClass(cdiDeviceClass),
		spec.WithDeviceSpecs(deviceSpecs),
		spec.WithEdits(*commonEdits.ContainerEdits),
	)
	if err != nil {
		return fmt.Errorf("failed to creat CDI spec: %w", err)
	}

	// Transform the spec to make it aware that it is running inside a container.
	err = transformroot.New(
		transformroot.WithRoot(cdi.driverRoot),
		transformroot.WithTargetRoot(cdi.targetDriverRoot),
		transformroot.WithRelativeTo("host"),
	).Transform(spec.Raw())
	if err != nil {
		return fmt.Errorf("failed to transform driver root in CDI spec: %w", err)
	}

	// Update the spec to include only the minimum version necessary.
	minVersion, err := cdispec.MinimumRequiredVersion(spec.Raw())
	if err != nil {
		return fmt.Errorf("failed to get minimum required CDI spec version: %w", err)
	}
	spec.Raw().Version = minVersion

	// Write the spec out to disk.
	specName := cdiapi.GenerateTransientSpecName(cdiVendor, cdiDeviceClass, cdiBaseSpecIdentifier)
	return cdi.cache.WriteSpec(spec.Raw(), specName)
}

func (cdi *CDIHandler) CreateClaimSpecFile(claimUID string, preparedDevices PreparedDevices) error {
	// Generate claim specific specs for each device.
	var deviceSpecs []cdispec.Device
	for _, group := range preparedDevices {
		// If there are no edits passed back as part of the device config state, skip it
		if group.ConfigState.containerEdits == nil {
			continue
		}

		// Apply any edits passed back as part of the device config state to all devices
		for _, device := range group.Devices {
			deviceSpec := cdispec.Device{
				Name:           fmt.Sprintf("%s-%s", claimUID, device.CanonicalName()),
				ContainerEdits: *group.ConfigState.containerEdits.ContainerEdits,
			}

			// Add fabric topology information if available
			if group.ConfigState.FabricAllocation != nil {
				cdi.addFabricAllocationToDeviceSpec(&deviceSpec, group.ConfigState.FabricAllocation)
			} else if group.ConfigState.FabricPartitionID != "" {
				// Fallback to old approach for backward compatibility
				cdi.addFabricTopologyToDeviceSpec(&deviceSpec, group.ConfigState.FabricPartitionID)
			}

			deviceSpecs = append(deviceSpecs, deviceSpec)
		}
	}

	// If there are no claim specific deviceSpecs, just return without creating the spec file
	if len(deviceSpecs) == 0 {
		return nil
	}

	// Generate the claim specific device spec for this driver.
	spec, err := spec.New(
		spec.WithVendor(cdiVendor),
		spec.WithClass(cdiClaimClass),
		spec.WithDeviceSpecs(deviceSpecs),
	)
	if err != nil {
		return fmt.Errorf("failed to creat CDI spec: %w", err)
	}

	// Transform the spec to make it aware that it is running inside a container.
	err = transformroot.New(
		transformroot.WithRoot(cdi.driverRoot),
		transformroot.WithTargetRoot(cdi.targetDriverRoot),
		transformroot.WithRelativeTo("host"),
	).Transform(spec.Raw())
	if err != nil {
		return fmt.Errorf("failed to transform driver root in CDI spec: %w", err)
	}

	// Update the spec to include only the minimum version necessary.
	minVersion, err := cdispec.MinimumRequiredVersion(spec.Raw())
	if err != nil {
		return fmt.Errorf("failed to get minimum required CDI spec version: %w", err)
	}
	spec.Raw().Version = minVersion

	// Write the spec out to disk.
	specName := cdiapi.GenerateTransientSpecName(cdiVendor, cdiClaimClass, claimUID)
	return cdi.cache.WriteSpec(spec.Raw(), specName)
}

func (cdi *CDIHandler) DeleteClaimSpecFile(claimUID string) error {
	specName := cdiapi.GenerateTransientSpecName(cdiVendor, cdiClaimClass, claimUID)
	return cdi.cache.RemoveSpec(specName)
}

func (cdi *CDIHandler) GetStandardDevice(device *AllocatableDevice) string {
	return cdiparser.QualifiedName(cdiVendor, cdiDeviceClass, device.CanonicalName())
}

func (cdi *CDIHandler) GetClaimDevice(claimUID string, device *AllocatableDevice, containerEdits *cdiapi.ContainerEdits) string {
	if containerEdits == nil {
		return ""
	}
	return cdiparser.QualifiedName(cdiVendor, cdiClaimClass, fmt.Sprintf("%s-%s", claimUID, device.CanonicalName()))
}

// addFabricAllocationToDeviceSpec adds fabric allocation information to a CDI device specification.
func (cdi *CDIHandler) addFabricAllocationToDeviceSpec(deviceSpec *cdispec.Device, fabricAllocation *configapi.FabricResourceAllocation) {
	if fabricAllocation == nil {
		return
	}

	// Get the appropriate handler for the virtualization model
	handler := GetFabricHandler(fabricAllocation.VirtualizationModel)

	// Generate CDI spec using the handler
	fabricDeviceSpec := handler.GenerateCDISpec(fabricAllocation)
	if fabricDeviceSpec != nil {
		// Merge the fabric device spec into the main device spec
		deviceSpec.ContainerEdits.Env = append(deviceSpec.ContainerEdits.Env, fabricDeviceSpec.ContainerEdits.Env...)
		deviceSpec.ContainerEdits.Hooks = append(deviceSpec.ContainerEdits.Hooks, fabricDeviceSpec.ContainerEdits.Hooks...)
		deviceSpec.ContainerEdits.Mounts = append(deviceSpec.ContainerEdits.Mounts, fabricDeviceSpec.ContainerEdits.Mounts...)
	}
}

// addFabricTopologyToDeviceSpec adds fabric topology information to a CDI device specification.
func (cdi *CDIHandler) addFabricTopologyToDeviceSpec(deviceSpec *cdispec.Device, fabricPartitionID string) {
	// Add fabric topology environment variables
	fabricEnvVars := cdi.generateFabricTopologyEnvironmentVariables(fabricPartitionID)
	deviceSpec.ContainerEdits.Env = append(deviceSpec.ContainerEdits.Env, fabricEnvVars...)

	// Add fabric topology hooks
	fabricHooks := cdi.generateFabricTopologyHooks(fabricPartitionID)
	deviceSpec.ContainerEdits.Hooks = append(deviceSpec.ContainerEdits.Hooks, fabricHooks...)

	// Add fabric topology mounts
	fabricMounts := cdi.generateFabricTopologyMounts(fabricPartitionID)
	deviceSpec.ContainerEdits.Mounts = append(deviceSpec.ContainerEdits.Mounts, fabricMounts...)
}

// generateFabricTopologyEnvironmentVariables generates environment variables for fabric topology.
func (cdi *CDIHandler) generateFabricTopologyEnvironmentVariables(fabricPartitionID string) []string {
	return []string{
		fmt.Sprintf("NVIDIA_FABRIC_PARTITION_ID=%s", fabricPartitionID),
		"NVIDIA_FABRIC_TOPOLOGY_ENABLED=true",
		"NVIDIA_NVLINK_OPTIMIZATION=true",
		fmt.Sprintf("CUDA_VISIBLE_DEVICES_FABRIC_PARTITION=%s", fabricPartitionID),
	}
}

// generateFabricTopologyHooks generates CDI hooks for fabric topology.
func (cdi *CDIHandler) generateFabricTopologyHooks(fabricPartitionID string) []*cdispec.Hook {
	return []*cdispec.Hook{
		{
			HookName: "nvidia-fabric-topology-hook",
			Path:     cdi.nvidiaCDIHookPath,
			Args: []string{
				"--fabric-partition-id=" + fabricPartitionID,
				"--enable-nvlink-optimization",
			},
			Env: []string{
				"NVIDIA_FABRIC_PARTITION_ID=" + fabricPartitionID,
				"NVIDIA_FABRIC_TOPOLOGY_ENABLED=true",
			},
		},
	}
}

// generateFabricTopologyMounts generates CDI mounts for fabric topology.
func (cdi *CDIHandler) generateFabricTopologyMounts(fabricPartitionID string) []*cdispec.Mount {
	return []*cdispec.Mount{
		{
			HostPath:      "/sys/class/nvlink",
			ContainerPath: "/sys/class/nvlink",
			Options:       []string{"ro"},
		},
		{
			HostPath:      "/proc/driver/nvidia/gpus",
			ContainerPath: "/proc/driver/nvidia/gpus",
			Options:       []string{"ro"},
		},
		{
			HostPath:      "/var/run/nvidia-fabric",
			ContainerPath: "/var/run/nvidia-fabric",
			Options:       []string{"rw"},
		},
	}
}
