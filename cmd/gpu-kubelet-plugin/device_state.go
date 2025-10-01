/*
 * Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
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
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/kubelet/checkpointmanager"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdispec "tags.cncf.io/container-device-interface/specs-go"

	configapi "github.com/NVIDIA/k8s-dra-driver-gpu/api/nvidia.com/resource/v1beta1"
	"github.com/NVIDIA/k8s-dra-driver-gpu/pkg/featuregates"
)

type OpaqueDeviceConfig struct {
	Requests []string
	Config   runtime.Object
}

type DeviceConfigState struct {
	MpsControlDaemonID       string `json:"mpsControlDaemonID"`
	containerEdits           *cdiapi.ContainerEdits
	PreparedMigDevices       *PreparedMigDevices       `json:"preparedMigDevices,omitempty"`
	PreparedFabricPartitions *PreparedFabricPartitions `json:"preparedFabricPartitions,omitempty"`
}

type DeviceState struct {
	sync.Mutex
	cdi         *CDIHandler
	tsManager   *TimeSlicingManager
	mpsManager  *MpsManager
	allocatable AllocatableDevices
	config      *Config

	nvdevlib          *deviceLib
	checkpointManager checkpointmanager.CheckpointManager
}

func NewDeviceState(ctx context.Context, config *Config) (*DeviceState, error) {
	containerDriverRoot := root(config.flags.containerDriverRoot)
	nvdevlib, err := newDeviceLib(containerDriverRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to create device library: %w", err)
	}

	allocatable, err := nvdevlib.enumerateAllPossibleDevices(config)
	if err != nil {
		return nil, fmt.Errorf("error enumerating all possible devices: %w", err)
	}

	devRoot := containerDriverRoot.getDevRoot()
	klog.Infof("using devRoot=%v", devRoot)

	hostDriverRoot := config.flags.hostDriverRoot
	cdi, err := NewCDIHandler(
		WithNvml(nvdevlib.nvmllib),
		WithDeviceLib(nvdevlib),
		WithDriverRoot(string(containerDriverRoot)),
		WithDevRoot(devRoot),
		WithTargetDriverRoot(hostDriverRoot),
		WithNVIDIACDIHookPath(config.flags.nvidiaCDIHookPath),
		WithCDIRoot(config.flags.cdiRoot),
		WithVendor(cdiVendor),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create CDI handler: %w", err)
	}

	var tsManager *TimeSlicingManager
	if featuregates.Enabled(featuregates.TimeSlicingSettings) {
		tsManager = NewTimeSlicingManager(nvdevlib)
	}

	var mpsManager *MpsManager
	if featuregates.Enabled(featuregates.MPSSupport) {
		mpsManager = NewMpsManager(config, nvdevlib, hostDriverRoot, MpsControlDaemonTemplatePath)
	}

	if err := cdi.CreateStandardDeviceSpecFile(allocatable); err != nil {
		return nil, fmt.Errorf("unable to create base CDI spec file: %v", err)
	}

	checkpointManager, err := checkpointmanager.NewCheckpointManager(config.DriverPluginPath())
	if err != nil {
		return nil, fmt.Errorf("unable to create checkpoint manager: %v", err)
	}

	state := &DeviceState{
		cdi:               cdi,
		tsManager:         tsManager,
		mpsManager:        mpsManager,
		allocatable:       allocatable,
		config:            config,
		nvdevlib:          nvdevlib,
		checkpointManager: checkpointManager,
	}

	checkpoints, err := state.checkpointManager.ListCheckpoints()
	if err != nil {
		return nil, fmt.Errorf("unable to list checkpoints: %v", err)
	}

	for _, c := range checkpoints {
		if c == DriverPluginCheckpointFileBasename {
			return state, nil
		}
	}

	if err := state.createCheckpoint(&Checkpoint{}); err != nil {
		return nil, fmt.Errorf("unable to create checkpoint: %v", err)
	}

	return state, nil
}

func (s *DeviceState) Prepare(ctx context.Context, claim *resourceapi.ResourceClaim) ([]kubeletplugin.Device, error) {
	s.Lock()
	defer s.Unlock()

	claimUID := string(claim.UID)

	checkpoint, err := s.getCheckpoint()
	if err != nil {
		return nil, fmt.Errorf("unable to get checkpoint: %v", err)
	}

	err = s.updateCheckpoint(func(checkpoint *Checkpoint) {
		checkpoint.V2.PreparedClaims[claimUID] = PreparedClaim{
			CheckpointState: ClaimCheckpointStatePrepareStarted,
			Status:          claim.Status,
		}
	})
	if err != nil {
		return nil, fmt.Errorf("unable to update checkpoint: %w", err)
	}
	klog.V(6).Infof("checkpoint updated for claim %v", claimUID)

	preparedClaim, exists := checkpoint.V2.PreparedClaims[claimUID]
	if exists && preparedClaim.CheckpointState == ClaimCheckpointStatePrepareCompleted {
		// Make this a noop. Associated device(s) has/ave been prepared by us.
		// Prepare() must be idempotent, as it may be invoked more than once per
		// claim (and actual device preparation must happen at most once).
		klog.V(6).Infof("skip prepare: claim %v found in checkpoint", claimUID)
		return preparedClaim.PreparedDevices.GetDevices(), nil
	}

	preparedDevices, err := s.prepareDevices(ctx, claim)
	if err != nil {
		return nil, fmt.Errorf("prepare devices failed: %w", err)
	}

	if err := s.cdi.CreateClaimSpecFile(claimUID, preparedDevices); err != nil {
		return nil, fmt.Errorf("unable to create CDI spec file for claim: %w", err)
	}

	err = s.updateCheckpoint(func(checkpoint *Checkpoint) {
		checkpoint.V2.PreparedClaims[claimUID] = PreparedClaim{
			CheckpointState: ClaimCheckpointStatePrepareCompleted,
			Status:          claim.Status,
			PreparedDevices: preparedDevices,
		}
	})
	if err != nil {
		return nil, fmt.Errorf("unable to update checkpoint: %w", err)
	}
	klog.V(6).Infof("checkpoint updated for claim %v", claimUID)

	return preparedDevices.GetDevices(), nil
}

func (s *DeviceState) Unprepare(ctx context.Context, claimUID string) error {
	s.Lock()
	defer s.Unlock()

	checkpoint, err := s.getCheckpoint()
	if err != nil {
		return fmt.Errorf("unable to get checkpoint: %v", err)
	}

	pc, exists := checkpoint.V2.PreparedClaims[claimUID]
	if !exists {
		// Not an error: if this claim UID is not in the checkpoint then this
		// device was never prepared or has already been unprepared (assume that
		// Prepare+Checkpoint are done transactionally). Note that
		// claimRef.String() contains namespace, name, UID.
		klog.Infof("unprepare noop: claim not found in checkpoint data: %v", claimUID)
		return nil
	}

	switch pc.CheckpointState {
	case ClaimCheckpointStatePrepareStarted:
		klog.Infof("unprepare noop: claim preparation started but not completed: %v", claimUID)
		return nil
	case ClaimCheckpointStatePrepareCompleted:
		if err := s.unprepareDevices(ctx, claimUID, pc.PreparedDevices); err != nil {
			return fmt.Errorf("unprepare devices failed: %w", err)
		}
	default:
		return fmt.Errorf("unsupported ClaimCheckpointState: %v", pc.CheckpointState)
	}

	if err := s.cdi.DeleteClaimSpecFile(claimUID); err != nil {
		return fmt.Errorf("unable to delete CDI spec file for claim: %w", err)
	}

	// Unprepare succeeded; reflect that in the node-local checkpoint data.
	delete(checkpoint.V2.PreparedClaims, claimUID)
	if err := s.checkpointManager.CreateCheckpoint(DriverPluginCheckpointFileBasename, checkpoint); err != nil {
		return fmt.Errorf("unable to sync to checkpoint: %v", err)
	}

	return nil
}

func (s *DeviceState) createCheckpoint(cp *Checkpoint) error {
	return s.checkpointManager.CreateCheckpoint(DriverPluginCheckpointFileBasename, cp)
}

func (s *DeviceState) getCheckpoint() (*Checkpoint, error) {
	checkpoint := &Checkpoint{}
	if err := s.checkpointManager.GetCheckpoint(DriverPluginCheckpointFileBasename, checkpoint); err != nil {
		return nil, err
	}
	return checkpoint.ToLatestVersion(), nil
}

func (s *DeviceState) updateCheckpoint(f func(*Checkpoint)) error {
	checkpoint, err := s.getCheckpoint()
	if err != nil {
		return fmt.Errorf("unable to get checkpoint: %w", err)
	}

	f(checkpoint)

	if err := s.checkpointManager.CreateCheckpoint(DriverPluginCheckpointFileBasename, checkpoint); err != nil {
		return fmt.Errorf("unable to create checkpoint: %w", err)
	}

	return nil
}

func (s *DeviceState) prepareDevices(ctx context.Context, claim *resourceapi.ResourceClaim) (PreparedDevices, error) {
	if claim.Status.Allocation == nil {
		return nil, fmt.Errorf("claim not yet allocated")
	}

	// Retrieve the full set of device configs for the driver.
	configs, err := GetOpaqueDeviceConfigs(
		configapi.StrictDecoder,
		DriverName,
		claim.Status.Allocation.Devices.Config,
	)
	if err != nil {
		return nil, fmt.Errorf("error getting opaque device configs: %v", err)
	}

	// Add the default GPU, MIG device, and FabricManager Configs to the front of the config
	// list with the lowest precedence. This guarantees there will be at least
	// one of each config in the list with len(Requests) == 0 for the lookup below.
	configs = slices.Insert(configs, 0, &OpaqueDeviceConfig{
		Requests: []string{},
		Config:   configapi.DefaultGpuConfig(),
	})
	configs = slices.Insert(configs, 0, &OpaqueDeviceConfig{
		Requests: []string{},
		Config:   configapi.DefaultMigDeviceConfig(),
	})
	// Check if we should auto-create FabricManager config for multi-GPU scenarios
	autoFabricConfig, err := s.autoCreateFabricManagerConfig(claim.Status.Allocation.Devices.Results, claim)
	if err != nil {
		return nil, fmt.Errorf("failed to auto-create FabricManager config: %w", err)
	}

	// Add auto-created config if needed
	if autoFabricConfig != nil {
		configs = append(configs, &OpaqueDeviceConfig{
			Requests: []string{},
			Config:   autoFabricConfig,
		})
		klog.V(4).Infof("Auto-created FabricManager config for multi-GPU claim %s", claim.UID)
	}

	// Look through the configs and figure out which one will be applied to
	// each device allocation result based on their order of precedence and type.
	configResultsMap := make(map[runtime.Object][]*resourceapi.DeviceRequestAllocationResult)
	for _, result := range claim.Status.Allocation.Devices.Results {
		if result.Driver != DriverName {
			continue
		}
		device, exists := s.allocatable[result.Device]
		if !exists {
			return nil, fmt.Errorf("requested device is not allocatable: %v", result.Device)
		}
		for _, c := range slices.Backward(configs) {
			if slices.Contains(c.Requests, result.Request) {
				if _, ok := c.Config.(*configapi.GpuConfig); ok && device.Type() != GpuDeviceType {
					return nil, fmt.Errorf("cannot apply GPU config to request: %v", result.Request)
				}
				if _, ok := c.Config.(*configapi.MigDeviceConfig); ok && device.Type() != MigDeviceType {
					return nil, fmt.Errorf("cannot apply MIG device config to request: %v", result.Request)
				}
				if _, ok := c.Config.(*configapi.FabricManagerConfig); ok && device.Type() != FabricPartitionType {
					return nil, fmt.Errorf("cannot apply FabricManager config to request: %v", result.Request)
				}
				configResultsMap[c.Config] = append(configResultsMap[c.Config], &result)
				break
			}
			if len(c.Requests) == 0 {
				if _, ok := c.Config.(*configapi.GpuConfig); ok && device.Type() != GpuDeviceType {
					continue
				}
				if _, ok := c.Config.(*configapi.MigDeviceConfig); ok && device.Type() != MigDeviceType {
					continue
				}
				if _, ok := c.Config.(*configapi.FabricManagerConfig); ok && device.Type() != FabricPartitionType {
					continue
				}
				configResultsMap[c.Config] = append(configResultsMap[c.Config], &result)
				break
			}
		}
	}

	// Normalize, validate, and apply all configs associated with devices that
	// need to be prepared. Track device group configs generated from applying the
	// config to the set of device allocation results.
	preparedDeviceGroupConfigState := make(map[runtime.Object]*DeviceConfigState)
	for c, results := range configResultsMap {
		// Cast the opaque config to a configapi.Interface type
		var config configapi.Interface
		switch castConfig := c.(type) {
		case *configapi.GpuConfig:
			config = castConfig
		case *configapi.MigDeviceConfig:
			config = castConfig
		case *configapi.FabricManagerConfig:
			config = castConfig
		default:
			return nil, fmt.Errorf("runtime object is not a recognized configuration")
		}

		// Normalize the config to set any implied defaults.
		if err := config.Normalize(); err != nil {
			return nil, fmt.Errorf("error normalizing GPU config: %w", err)
		}

		// Validate the config to ensure its integrity.
		if err := config.Validate(); err != nil {
			return nil, fmt.Errorf("error validating GPU config: %w", err)
		}

		// Apply the config to the list of results associated with it.
		configState, err := s.applyConfig(ctx, config, claim, results)
		if err != nil {
			return nil, fmt.Errorf("error applying GPU config: %w", err)
		}

		// Capture the prepared device group config in the map.
		preparedDeviceGroupConfigState[c] = configState
	}

	// Walk through each config and its associated device allocation results
	// and construct the list of prepared devices to return.
	var preparedDevices PreparedDevices
	for c, results := range configResultsMap {
		preparedDeviceGroup := PreparedDeviceGroup{
			ConfigState: *preparedDeviceGroupConfigState[c],
		}

		for _, result := range results {
			cdiDevices := []string{}
			if d := s.cdi.GetStandardDevice(s.allocatable[result.Device]); d != "" {
				cdiDevices = append(cdiDevices, d)
			}
			if d := s.cdi.GetClaimDevice(string(claim.UID), s.allocatable[result.Device], preparedDeviceGroupConfigState[c].containerEdits); d != "" {
				cdiDevices = append(cdiDevices, d)
			}

			device := &kubeletplugin.Device{
				Requests:     []string{result.Request},
				PoolName:     result.Pool,
				DeviceName:   result.Device,
				CDIDeviceIDs: cdiDevices,
			}

			var preparedDevice PreparedDevice
			switch s.allocatable[result.Device].Type() {
			case GpuDeviceType:
				preparedDevice.Gpu = &PreparedGpu{
					Info:   s.allocatable[result.Device].Gpu,
					Device: device,
				}
			case MigDeviceType:
				preparedDevice.Mig = &PreparedMigDevice{
					Info:   s.allocatable[result.Device].Mig,
					Device: device,
				}
			}

			preparedDeviceGroup.Devices = append(preparedDeviceGroup.Devices, preparedDevice)
		}

		preparedDevices = append(preparedDevices, &preparedDeviceGroup)
	}
	return preparedDevices, nil
}

func (s *DeviceState) unprepareDevices(ctx context.Context, claimUID string, devices PreparedDevices) error {
	for _, group := range devices {
		// Stop any MPS control daemons started for each group of prepared devices.
		if featuregates.Enabled(featuregates.MPSSupport) {
			mpsControlDaemon := s.mpsManager.NewMpsControlDaemon(claimUID, group)
			if err := mpsControlDaemon.Stop(ctx); err != nil {
				return fmt.Errorf("error stopping MPS control daemon: %w", err)
			}
		}

		// Go back to default time-slicing for all full GPUs.
		if featuregates.Enabled(featuregates.TimeSlicingSettings) {
			tsc := configapi.DefaultGpuConfig().Sharing.TimeSlicingConfig
			if err := s.tsManager.SetTimeSlice(group.Devices.Gpus(), tsc); err != nil {
				return fmt.Errorf("error setting timeslice for devices: %w", err)
			}
		}

		// Clean up any prepared MIG devices
		if group.ConfigState.PreparedMigDevices != nil {
			if err := s.unprepareMigDevices(claimUID, group.ConfigState.PreparedMigDevices); err != nil {
				return fmt.Errorf("error unpreparing MIG devices: %w", err)
			}
		}

		// Clean up any prepared fabric partitions
		if group.ConfigState.PreparedFabricPartitions != nil {
			if err := s.unprepareFabricPartitions(claimUID, group.ConfigState.PreparedFabricPartitions); err != nil {
				return fmt.Errorf("error unpreparing fabric partitions: %w", err)
			}
		}
	}
	return nil
}

func (s *DeviceState) applyConfig(ctx context.Context, config configapi.Interface, claim *resourceapi.ResourceClaim, results []*resourceapi.DeviceRequestAllocationResult) (*DeviceConfigState, error) {
	switch castConfig := config.(type) {
	case *configapi.GpuConfig:
		return s.applySharingConfig(ctx, castConfig.Sharing, claim, results)
	case *configapi.MigDeviceConfig:
		return s.applyMigDeviceConfig(ctx, castConfig, claim, results)
	case *configapi.FabricManagerConfig:
		return s.applyFabricManagerConfig(ctx, castConfig, claim, results)
	default:
		return nil, fmt.Errorf("unknown config type: %T", castConfig)
	}
}

// applyMigDeviceConfig handles MIG device configuration and creation.
func (s *DeviceState) applyMigDeviceConfig(ctx context.Context, config *configapi.MigDeviceConfig, claim *resourceapi.ResourceClaim, results []*resourceapi.DeviceRequestAllocationResult) (*DeviceConfigState, error) {
	// For MIG device configs, we need to create MIG devices from the allocated GPUs
	var allocatedMigDevices AllocatedMigDevices

	for _, result := range results {
		device := s.allocatable[result.Device]
		if device.Type() != GpuDeviceType {
			return nil, fmt.Errorf("MIG device config can only be applied to GPU devices, got %v", device.Type())
		}

		// Create MIG devices for this GPU
		migDevices, err := s.createMigDevices(device.Gpu, config, claim)
		if err != nil {
			return nil, fmt.Errorf("error creating MIG devices for GPU %s: %w", device.Gpu.UUID, err)
		}

		allocatedMigDevices.Devices = append(allocatedMigDevices.Devices, migDevices...)
	}

	// Prepare the MIG devices
	preparedMigDevices, err := s.prepareMigDevices(string(claim.UID), &allocatedMigDevices)
	if err != nil {
		return nil, fmt.Errorf("error preparing MIG devices: %w", err)
	}

	// Create device config state with prepared MIG devices
	configState := &DeviceConfigState{
		PreparedMigDevices: preparedMigDevices,
	}

	return configState, nil
}

// applyFabricManagerConfig handles FabricManager device configuration and creation.
func (s *DeviceState) applyFabricManagerConfig(ctx context.Context, config *configapi.FabricManagerConfig, claim *resourceapi.ResourceClaim, results []*resourceapi.DeviceRequestAllocationResult) (*DeviceConfigState, error) {
	// Optimize GPU selection if fabric optimization is enabled
	if s.shouldOptimizeGpuSelection(results) {
		optimalGPUs, err := s.selectOptimalGpuCombination(len(results), &DefaultFabricOptimizationConfig)
		if err != nil {
			klog.Warningf("Failed to select optimal GPU combination, using original allocation: %v", err)
		} else {
			// Update results with optimal GPUs
			results = s.updateResultsWithOptimalGPUs(results, optimalGPUs)
			klog.V(4).Infof("Optimized GPU selection for FabricManager: %v", optimalGPUs)
		}
	}

	// For FabricManager device configs, we need to create fabric partitions from the allocated GPUs
	var allocatedFabricPartitions []AllocatedFabricPartition

	for _, result := range results {
		device := s.allocatable[result.Device]
		if device.Type() != GpuDeviceType {
			return nil, fmt.Errorf("FabricManager device config can only be applied to GPU devices, got %v", device.Type())
		}

		// Create fabric partitions for this GPU
		fabricPartitions, err := s.createFabricManagerDevices(device.Gpu, config, claim)
		if err != nil {
			return nil, fmt.Errorf("error creating fabric partitions for GPU %s: %w", device.Gpu.UUID, err)
		}

		allocatedFabricPartitions = append(allocatedFabricPartitions, fabricPartitions...)
	}

	// Prepare the fabric partitions
	preparedFabricPartitions, err := s.prepareFabricPartitions(string(claim.UID), allocatedFabricPartitions)
	if err != nil {
		return nil, fmt.Errorf("error preparing fabric partitions: %w", err)
	}

	// Generate container edits for fabric partitions
	containerEdits, err := s.generateFabricPartitionContainerEdits(preparedFabricPartitions)
	if err != nil {
		return nil, fmt.Errorf("error generating container edits for fabric partitions: %w", err)
	}

	// Create device config state with prepared fabric partitions and container edits
	configState := &DeviceConfigState{
		PreparedFabricPartitions: preparedFabricPartitions,
		containerEdits:           containerEdits,
	}

	return configState, nil
}

// generateFabricPartitionContainerEdits generates container edits for fabric partitions.
func (s *DeviceState) generateFabricPartitionContainerEdits(partitions *PreparedFabricPartitions) (*cdiapi.ContainerEdits, error) {
	if partitions == nil || len(partitions.Partitions) == 0 {
		return nil, nil
	}

	// For fabric partitions, we need to generate environment variables
	// that expose the fabric partition information to containers
	var envVars []string
	var deviceNodes []*cdispec.DeviceNode

	for _, partition := range partitions.Partitions {
		// Add fabric partition environment variables
		envVars = append(envVars, fmt.Sprintf("NVIDIA_FABRIC_PARTITION_%s_ID=%d", partition.PartitionName, partition.PartitionID))
		envVars = append(envVars, fmt.Sprintf("NVIDIA_FABRIC_PARTITION_%s_CLIQUE_ID=%s", partition.PartitionName, partition.CliqueID))
		envVars = append(envVars, fmt.Sprintf("NVIDIA_FABRIC_PARTITION_%s_PARENT_UUID=%s", partition.PartitionName, partition.ParentUUID))

		// Add device node access (fabric partitions use the parent GPU device)
		deviceNodes = append(deviceNodes, &cdispec.DeviceNode{
			Path: fmt.Sprintf("/dev/nvidia%s", partition.ParentUUID[:8]),
		})
	}

	// Create container edits
	containerEdits := &cdiapi.ContainerEdits{
		ContainerEdits: &cdispec.ContainerEdits{
			Env:         envVars,
			DeviceNodes: deviceNodes,
		},
	}

	return containerEdits, nil
}

func (s *DeviceState) applySharingConfig(ctx context.Context, config configapi.Sharing, claim *resourceapi.ResourceClaim, results []*resourceapi.DeviceRequestAllocationResult) (*DeviceConfigState, error) {
	// Get the list of claim requests this config is being applied over.
	var requests []string
	for _, r := range results {
		requests = append(requests, r.Request)
	}

	// Get the list of allocatable devices this config is being applied over.
	allocatableDevices := make(AllocatableDevices)
	for _, r := range results {
		allocatableDevices[r.Device] = s.allocatable[r.Device]
	}

	// Declare a device group state object to populate.
	var configState DeviceConfigState

	// Apply time-slicing settings (if available and feature gate enabled).
	if featuregates.Enabled(featuregates.TimeSlicingSettings) && config.IsTimeSlicing() {
		tsc, err := config.GetTimeSlicingConfig()
		if err != nil {
			return nil, fmt.Errorf("error getting timeslice config for requests '%v' in claim '%v': %w", requests, claim.UID, err)
		}
		if tsc != nil {
			err = s.tsManager.SetTimeSlice(allocatableDevices, tsc)
			if err != nil {
				return nil, fmt.Errorf("error setting timeslice config for requests '%v' in claim '%v': %w", requests, claim.UID, err)
			}
		}
	}

	// Apply MPS settings (if available and feature gate enabled).
	if featuregates.Enabled(featuregates.MPSSupport) && config.IsMps() {
		mpsc, err := config.GetMpsConfig()
		if err != nil {
			return nil, fmt.Errorf("error getting MPS configuration: %w", err)
		}
		mpsControlDaemon := s.mpsManager.NewMpsControlDaemon(string(claim.UID), allocatableDevices)
		if err := mpsControlDaemon.Start(ctx, mpsc); err != nil {
			return nil, fmt.Errorf("error starting MPS control daemon: %w", err)
		}
		if err := mpsControlDaemon.AssertReady(ctx); err != nil {
			return nil, fmt.Errorf("MPS control daemon is not yet ready: %w", err)
		}
		configState.MpsControlDaemonID = mpsControlDaemon.GetID()
		configState.containerEdits = mpsControlDaemon.GetCDIContainerEdits()
	}

	return &configState, nil
}

// GetOpaqueDeviceConfigs returns an ordered list of the configs contained in possibleConfigs for this driver.
//
// Configs can either come from the resource claim itself or from the device
// class associated with the request. Configs coming directly from the resource
// claim take precedence over configs coming from the device class. Moreover,
// configs found later in the list of configs attached to its source take
// precedence over configs found earlier in the list for that source.
//
// All of the configs relevant to the driver from the list of possibleConfigs
// will be returned in order of precedence (from lowest to highest). If no
// configs are found, nil is returned.
func GetOpaqueDeviceConfigs(
	decoder runtime.Decoder,
	driverName string,
	possibleConfigs []resourceapi.DeviceAllocationConfiguration,
) ([]*OpaqueDeviceConfig, error) {
	// Collect all configs in order of reverse precedence.
	var classConfigs []resourceapi.DeviceAllocationConfiguration
	var claimConfigs []resourceapi.DeviceAllocationConfiguration
	var candidateConfigs []resourceapi.DeviceAllocationConfiguration
	for _, config := range possibleConfigs {
		switch config.Source {
		case resourceapi.AllocationConfigSourceClass:
			classConfigs = append(classConfigs, config)
		case resourceapi.AllocationConfigSourceClaim:
			claimConfigs = append(claimConfigs, config)
		default:
			return nil, fmt.Errorf("invalid config source: %v", config.Source)
		}
	}
	candidateConfigs = append(candidateConfigs, classConfigs...)
	candidateConfigs = append(candidateConfigs, claimConfigs...)

	// Decode all configs that are relevant for the driver.
	var resultConfigs []*OpaqueDeviceConfig
	for _, config := range candidateConfigs {
		// If this is nil, the driver doesn't support some future API extension
		// and needs to be updated.
		if config.Opaque == nil {
			return nil, fmt.Errorf("only opaque parameters are supported by this driver")
		}

		// Configs for different drivers may have been specified because a
		// single request can be satisfied by different drivers. This is not
		// an error -- drivers must skip over other driver's configs in order
		// to support this.
		if config.Opaque.Driver != driverName {
			continue
		}

		decodedConfig, err := runtime.Decode(decoder, config.Opaque.Parameters.Raw)
		if err != nil {
			return nil, fmt.Errorf("error decoding config parameters: %w", err)
		}

		resultConfig := &OpaqueDeviceConfig{
			Requests: config.Requests,
			Config:   decodedConfig,
		}

		resultConfigs = append(resultConfigs, resultConfig)
	}

	return resultConfigs, nil
}

// MIG-related types and structures

// AllocatedMigDevice represents a MIG device that has been allocated for a claim.
type AllocatedMigDevice struct {
	ParentUUID string `json:"parentUUID"`
	Profile    string `json:"profile"`
	Placement  struct {
		Start int `json:"start"`
		Size  int `json:"size"`
	} `json:"placement"`
}

// AllocatedMigDevices represents a collection of allocated MIG devices.
type AllocatedMigDevices struct {
	Devices []AllocatedMigDevice `json:"devices"`
}

// PreparedMigDevices represents a collection of prepared MIG devices.
type PreparedMigDevices struct {
	Devices []*MigDeviceInfo `json:"devices"`
}

// prepareMigDevices prepares MIG devices for use by containers.
func (s *DeviceState) prepareMigDevices(claimUID string, allocated *AllocatedMigDevices) (*PreparedMigDevices, error) {
	// Validate input parameters
	if claimUID == "" {
		return nil, fmt.Errorf("claim UID cannot be empty")
	}

	if allocated == nil {
		return nil, fmt.Errorf("allocated MIG devices cannot be nil")
	}

	if len(allocated.Devices) == 0 {
		return nil, fmt.Errorf("no MIG devices to prepare")
	}

	prepared := &PreparedMigDevices{}

	for i, device := range allocated.Devices {
		// Validate each device before processing
		if err := s.validateAllocatedMigDevice(device, i); err != nil {
			return nil, fmt.Errorf("allocated MIG device %d validation failed: %w", i, err)
		}

		if _, exists := s.allocatable[device.ParentUUID]; !exists {
			return nil, fmt.Errorf("allocated GPU does not exist: %v", device.ParentUUID)
		}

		parent := s.allocatable[device.ParentUUID]

		// Validate GPU for MIG creation
		if err := s.validateGpuForMigCreation(parent.Gpu); err != nil {
			return nil, fmt.Errorf("GPU validation failed: %w", err)
		}

		// Find the MIG profile by string representation
		var migProfile *MigProfileInfo
		for _, profile := range parent.Gpu.migProfiles {
			if profile.String() == device.Profile {
				migProfile = profile
				break
			}
		}
		if migProfile == nil {
			return nil, fmt.Errorf("MIG profile %v does not exist on GPU: %v", device.Profile, device.ParentUUID)
		}

		// Validate the MIG profile
		if err := s.validateMigProfile(migProfile, parent.Gpu); err != nil {
			return nil, fmt.Errorf("MIG profile validation failed: %w", err)
		}

		// Get the GpuInstanceProfileInfo from the device
		profileInfo := migProfile.profile.GetInfo()
		deviceHandle, ret := s.nvdevlib.nvmllib.DeviceGetHandleByUUID(parent.Gpu.UUID)
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error getting GPU device handle: %v", ret)
		}

		giProfileInfo, ret := deviceHandle.GetGpuInstanceProfileInfo(profileInfo.GIProfileID)
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("error getting GPU instance profile info for profile %v: %v", device.Profile, ret)
		}

		placement := nvml.GpuInstancePlacement{
			Start: uint32(device.Placement.Start),
			Size:  uint32(device.Placement.Size),
		}

		// Validate placement
		if err := s.validateMigPlacement(&placement, parent.Gpu); err != nil {
			return nil, fmt.Errorf("MIG placement validation failed: %w", err)
		}

		migInfo, err := s.nvdevlib.createMigDevice(parent.Gpu, giProfileInfo, &placement)
		if err != nil {
			return nil, fmt.Errorf("error creating MIG device: %w", err)
		}

		prepared.Devices = append(prepared.Devices, migInfo)
	}

	return prepared, nil
}

// unprepareMigDevices cleans up MIG devices after container use.
func (s *DeviceState) unprepareMigDevices(claimUID string, devices *PreparedMigDevices) error {
	if devices == nil {
		return nil
	}

	for _, device := range devices.Devices {
		err := s.nvdevlib.deleteMigDevice(device)
		if err != nil {
			return fmt.Errorf("error deleting MIG device for %v: %w", device.UUID, err)
		}
	}

	return nil
}

// validateAllocatedMigDevice validates a single allocated MIG device.
func (s *DeviceState) validateAllocatedMigDevice(device AllocatedMigDevice, index int) error {
	if device.ParentUUID == "" {
		return fmt.Errorf("parent UUID cannot be empty")
	}

	if device.Profile == "" {
		return fmt.Errorf("profile cannot be empty")
	}

	if device.Placement.Start < 0 {
		return fmt.Errorf("placement start must be non-negative")
	}

	if device.Placement.Size <= 0 {
		return fmt.Errorf("placement size must be positive")
	}

	return nil
}

// validateMigDeviceConfig validates the MIG device configuration.
func (s *DeviceState) validateMigDeviceConfig(config *configapi.MigDeviceConfig) error {
	if config == nil {
		return fmt.Errorf("MIG device config cannot be nil")
	}

	// Validate the config using the built-in validation
	if err := config.Validate(); err != nil {
		return fmt.Errorf("MIG device config validation failed: %w", err)
	}

	// Additional validation for sharing configuration
	if config.Sharing != nil {
		if err := s.validateMigDeviceSharing(config.Sharing); err != nil {
			return fmt.Errorf("MIG device sharing validation failed: %w", err)
		}
	}

	return nil
}

// validateMigDeviceSharing validates the MIG device sharing configuration.
func (s *DeviceState) validateMigDeviceSharing(sharing *configapi.MigDeviceSharing) error {
	if sharing == nil {
		return nil
	}

	// Validate strategy
	switch sharing.Strategy {
	case configapi.TimeSlicingStrategy, configapi.MpsStrategy:
		// Valid strategies
	default:
		return fmt.Errorf("invalid sharing strategy: %s", sharing.Strategy)
	}

	// Validate MPS configuration if MPS strategy is used
	if sharing.Strategy == configapi.MpsStrategy {
		if err := s.validateMpsConfig(sharing.MpsConfig); err != nil {
			return fmt.Errorf("MPS config validation failed: %w", err)
		}
	}

	return nil
}

// validateMpsConfig validates the MPS configuration.
func (s *DeviceState) validateMpsConfig(config *configapi.MpsConfig) error {
	if config == nil {
		return nil
	}

	// Validate active thread percentage
	if config.DefaultActiveThreadPercentage != nil {
		percentage := *config.DefaultActiveThreadPercentage
		if percentage < 1 || percentage > 100 {
			return fmt.Errorf("active thread percentage must be between 1 and 100, got %d", percentage)
		}
	}

	// Validate pinned memory limits
	if config.DefaultPinnedDeviceMemoryLimit != nil {
		if config.DefaultPinnedDeviceMemoryLimit.Value() <= 0 {
			return fmt.Errorf("default pinned device memory limit must be positive")
		}
	}

	// Validate per-device pinned memory limits
	for deviceKey, limit := range config.DefaultPerDevicePinnedMemoryLimit {
		if limit.Value() <= 0 {
			return fmt.Errorf("pinned memory limit for device %s must be positive", deviceKey)
		}
	}

	return nil
}

// FabricManager-related validation functions

// validateGpuForFabricManagerCreation validates a GPU for FabricManager device creation.
func (s *DeviceState) validateGpuForFabricManagerCreation(gpu *GpuInfo) error {
	if gpu == nil {
		return fmt.Errorf("GPU cannot be nil")
	}

	// Check if GPU supports FabricManager
	if !s.isFabricManagerCapable(gpu) {
		return fmt.Errorf("GPU %s does not support FabricManager", gpu.UUID)
	}

	// Check if GPU is fabric-attached (skip if nvdevlib is not available for testing)
	if s.nvdevlib != nil && s.nvdevlib.nvmllib != nil {
		if !s.isFabricAttached(gpu) {
			return fmt.Errorf("GPU %s is not fabric-attached", gpu.UUID)
		}
	}

	// For Ampere: MIG-enabled GPUs cannot participate in fabric
	if s.isAmpereArchitecture(gpu) && gpu.migEnabled {
		return fmt.Errorf("GPU %s is MIG-enabled and cannot participate in fabric on Ampere architecture", gpu.UUID)
	}

	return nil
}

// isFabricManagerCapable checks if a GPU supports FabricManager.
func (s *DeviceState) isFabricManagerCapable(gpu *GpuInfo) bool {
	// Check if architecture supports FabricManager
	supportedArchitectures := []string{"Ampere", "Hopper", "Blackwell"}
	for _, arch := range supportedArchitectures {
		if gpu.architecture == arch {
			return true
		}
	}
	return false
}

// isFabricAttached checks if a GPU is fabric-attached.
func (s *DeviceState) isFabricAttached(gpu *GpuInfo) bool {
	deviceHandle, ret := s.nvdevlib.nvmllib.DeviceGetHandleByUUID(gpu.UUID)
	if ret != nvml.SUCCESS {
		klog.Warningf("Failed to get device handle for GPU %s: %v", gpu.UUID, ret)
		return false
	}

	// Use go-nvlib's IsFabricAttached method
	device, err := s.nvdevlib.NewDevice(deviceHandle)
	if err != nil {
		klog.Warningf("Failed to create device for GPU %s: %v", gpu.UUID, err)
		return false
	}

	fabricAttached, err := device.IsFabricAttached()
	if err != nil {
		klog.Warningf("Failed to check fabric attachment for GPU %s: %v", gpu.UUID, err)
		return false
	}

	return fabricAttached
}

// isAmpereArchitecture checks if a GPU is Ampere architecture.
func (s *DeviceState) isAmpereArchitecture(gpu *GpuInfo) bool {
	return gpu.architecture == "Ampere"
}

// isHopperArchitecture checks if a GPU is Hopper+ architecture (Hopper, Blackwell, etc.)
func (s *DeviceState) isHopperArchitecture(gpu *GpuInfo) bool {
	return gpu.architecture == "Hopper" || gpu.architecture == "Blackwell"
}

// validateGpuForMigCreation validates a GPU for MIG device creation.
func (s *DeviceState) validateGpuForMigCreation(gpu *GpuInfo) error {
	if gpu == nil {
		return fmt.Errorf("GPU cannot be nil")
	}

	// Check if GPU is MIG-enabled
	if !gpu.migEnabled {
		return fmt.Errorf("GPU %s does not have MIG mode enabled", gpu.UUID)
	}

	// Check if GPU has available MIG profiles
	if len(gpu.migProfiles) == 0 {
		return fmt.Errorf("GPU %s has no available MIG profiles", gpu.UUID)
	}

	// Validate GPU architecture compatibility
	if err := s.validateGpuArchitectureForMig(gpu); err != nil {
		return fmt.Errorf("GPU architecture validation failed: %w", err)
	}

	// Validate GPU memory requirements
	if err := s.validateGpuMemoryForMig(gpu); err != nil {
		return fmt.Errorf("GPU memory validation failed: %w", err)
	}

	return nil
}

// validateGpuArchitectureForMig validates GPU architecture for MIG compatibility.
func (s *DeviceState) validateGpuArchitectureForMig(gpu *GpuInfo) error {
	if gpu.architecture == "" {
		return fmt.Errorf("GPU architecture is not specified")
	}

	// Check if architecture supports MIG
	supportedArchitectures := []string{"Ampere", "Hopper", "Blackwell"}
	isSupported := false
	for _, arch := range supportedArchitectures {
		if gpu.architecture == arch {
			isSupported = true
			break
		}
	}

	if !isSupported {
		return fmt.Errorf("GPU architecture %s does not support MIG", gpu.architecture)
	}

	return nil
}

// validateGpuMemoryForMig validates GPU memory requirements for MIG.
func (s *DeviceState) validateGpuMemoryForMig(gpu *GpuInfo) error {
	if gpu.memoryBytes == 0 {
		return fmt.Errorf("GPU memory size is not specified")
	}

	// Check minimum memory requirements for MIG
	minMemoryBytes := s.getMinMemoryForMig(gpu.architecture)
	if gpu.memoryBytes < minMemoryBytes {
		return fmt.Errorf("GPU memory %d bytes is below minimum required %d bytes for MIG on %s architecture",
			gpu.memoryBytes, minMemoryBytes, gpu.architecture)
	}

	return nil
}

// getMinMemoryForMig returns the minimum memory required for MIG on a given architecture.
func (s *DeviceState) getMinMemoryForMig(architecture string) uint64 {
	switch architecture {
	case "Ampere":
		return 40 * 1024 * 1024 * 1024 // 40GB minimum for A100
	case "Hopper", "Blackwell":
		return 80 * 1024 * 1024 * 1024 // 80GB minimum for H100+
	default:
		return 40 * 1024 * 1024 * 1024 // Default to A100 minimum
	}
}

// validateMigProfile validates a MIG profile for a specific GPU.
func (s *DeviceState) validateMigProfile(profile *MigProfileInfo, gpu *GpuInfo) error {
	if profile == nil {
		return fmt.Errorf("MIG profile cannot be nil")
	}

	if gpu == nil {
		return fmt.Errorf("GPU cannot be nil")
	}

	// Validate profile string format
	profileStr := profile.String()
	if profileStr == "" {
		return fmt.Errorf("MIG profile string cannot be empty")
	}

	// Validate profile is available on the GPU
	found := false
	for _, gpuProfile := range gpu.migProfiles {
		if gpuProfile.String() == profileStr {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("MIG profile %s is not available on GPU %s", profileStr, gpu.UUID)
	}

	// Validate profile memory requirements
	if err := s.validateProfileMemoryRequirements(profile, gpu); err != nil {
		return fmt.Errorf("profile memory validation failed: %w", err)
	}

	return nil
}

// validateProfileMemoryRequirements validates memory requirements for a MIG profile.
func (s *DeviceState) validateProfileMemoryRequirements(profile *MigProfileInfo, gpu *GpuInfo) error {
	profileInfo := profile.profile.GetInfo()

	// Check if profile memory is within GPU memory limits
	profileMemoryBytes := uint64(profileInfo.GB) * 1024 * 1024 * 1024
	if profileMemoryBytes > gpu.memoryBytes {
		return fmt.Errorf("profile memory %d bytes exceeds GPU memory %d bytes",
			profileMemoryBytes, gpu.memoryBytes)
	}

	// Check minimum profile memory
	minProfileMemoryBytes := uint64(1 * 1024 * 1024 * 1024) // 1GB minimum
	if profileMemoryBytes < minProfileMemoryBytes {
		return fmt.Errorf("profile memory %d bytes is below minimum 1GB", profileMemoryBytes)
	}

	return nil
}

// validateMigPlacement validates MIG device placement.
func (s *DeviceState) validateMigPlacement(placement *nvml.GpuInstancePlacement, gpu *GpuInfo) error {
	if placement == nil {
		return fmt.Errorf("placement cannot be nil")
	}

	if gpu == nil {
		return fmt.Errorf("GPU cannot be nil")
	}

	// Validate placement bounds (Start is uint32, so it's always >= 0)

	if placement.Size <= 0 {
		return fmt.Errorf("placement size %d must be positive", placement.Size)
	}

	// Validate placement doesn't exceed GPU limits
	maxSlices := s.getMaxComputeSlices(gpu)
	if int(placement.Start+placement.Size) > maxSlices {
		return fmt.Errorf("placement range [%d, %d) exceeds maximum slices %d",
			placement.Start, placement.Start+placement.Size, maxSlices)
	}

	return nil
}

// validateResourceClaimForMig validates a resource claim for MIG device allocation.
func (s *DeviceState) validateResourceClaimForMig(claim *resourceapi.ResourceClaim) error {
	if claim == nil {
		return fmt.Errorf("resource claim cannot be nil")
	}

	// Validate claim UID
	if claim.UID == "" {
		return fmt.Errorf("resource claim UID cannot be empty")
	}

	// Validate claim name
	if claim.Name == "" {
		return fmt.Errorf("resource claim name cannot be empty")
	}

	// Validate claim namespace
	if claim.Namespace == "" {
		return fmt.Errorf("resource claim namespace cannot be empty")
	}

	return nil
}

// selectMigProfile selects an appropriate MIG profile based on GPU capabilities and requirements.
func (s *DeviceState) selectMigProfile(gpu *GpuInfo, config *configapi.MigDeviceConfig) string {
	if len(gpu.migProfiles) == 0 {
		return ""
	}

	// Simple selection strategy: prefer smaller profiles for better resource utilization
	var bestProfile *MigProfileInfo
	var bestScore = -1

	for _, profile := range gpu.migProfiles {
		score := s.calculateProfileScore(profile, gpu, config)
		if score > bestScore {
			bestScore = score
			bestProfile = profile
		}
	}

	if bestProfile != nil {
		return bestProfile.String()
	}

	// Fallback to first available profile
	return gpu.migProfiles[0].String()
}

// calculateProfileScore calculates a score for a MIG profile based on various factors.
func (s *DeviceState) calculateProfileScore(profile *MigProfileInfo, gpu *GpuInfo, config *configapi.MigDeviceConfig) int {
	score := 0

	// Get profile information
	profileInfo := profile.profile.GetInfo()

	// Prefer profiles with fewer compute slices (more efficient resource usage)
	// Lower slice count = higher score
	maxSlices := s.getMaxComputeSlices(gpu)
	if maxSlices > 0 {
		sliceScore := maxSlices - profileInfo.C
		score += sliceScore * 10 // Weight compute slices heavily
	}

	// Prefer profiles with smaller memory footprint
	// Lower memory = higher score (allows more MIG instances)
	if gpu.memoryBytes > 0 {
		memoryScore := int(gpu.memoryBytes/1024/1024/1024) - profileInfo.GB
		score += memoryScore * 2 // Weight memory moderately
	}

	// Prefer profiles with more placement options
	placementScore := len(profile.placements)
	score += placementScore * 5 // Weight placement options moderately

	return score
}

// getMaxComputeSlices returns the maximum number of compute slices for the GPU architecture.
func (s *DeviceState) getMaxComputeSlices(gpu *GpuInfo) int {
	switch gpu.architecture {
	case "Hopper", "Blackwell":
		return 8 // Hopper+ GPUs have up to 8 compute slices
	case "Ampere":
		return 7 // A100 GPUs have up to 7 compute slices
	default:
		return 7 // Default to A100-like behavior
	}
}

// createMigDevices creates MIG devices based on GPU capabilities and requirements.
func (s *DeviceState) createMigDevices(gpu *GpuInfo, config *configapi.MigDeviceConfig, claim *resourceapi.ResourceClaim) ([]AllocatedMigDevice, error) {
	// Comprehensive validation of input parameters
	if err := s.validateResourceClaimForMig(claim); err != nil {
		return nil, fmt.Errorf("resource claim validation failed: %w", err)
	}

	if err := s.validateMigDeviceConfig(config); err != nil {
		return nil, fmt.Errorf("MIG device config validation failed: %w", err)
	}

	if err := s.validateGpuForMigCreation(gpu); err != nil {
		return nil, fmt.Errorf("GPU validation failed: %w", err)
	}

	// Select optimal profile
	profile := s.selectMigProfile(gpu, config)
	if profile == "" {
		return nil, fmt.Errorf("failed to select MIG profile for GPU %s", gpu.UUID)
	}

	// Find the selected profile
	var selectedProfile *MigProfileInfo
	for _, p := range gpu.migProfiles {
		if p.String() == profile {
			selectedProfile = p
			break
		}
	}
	if selectedProfile == nil {
		return nil, fmt.Errorf("selected MIG profile %s not found on GPU %s", profile, gpu.UUID)
	}

	// Validate the selected profile
	if err := s.validateMigProfile(selectedProfile, gpu); err != nil {
		return nil, fmt.Errorf("MIG profile validation failed: %w", err)
	}

	// Create placement and validate it
	placement := nvml.GpuInstancePlacement{
		Start: 0,
		Size:  1,
	}
	if err := s.validateMigPlacement(&placement, gpu); err != nil {
		return nil, fmt.Errorf("MIG placement validation failed: %w", err)
	}

	// Create MIG device with validated placement
	migDevice := AllocatedMigDevice{
		ParentUUID: gpu.UUID,
		Profile:    profile,
		Placement: struct {
			Start int `json:"start"`
			Size  int `json:"size"`
		}{
			Start: int(placement.Start),
			Size:  int(placement.Size),
		},
	}

	// Validate the created device
	if err := s.validateAllocatedMigDevice(migDevice, 0); err != nil {
		return nil, fmt.Errorf("allocated MIG device validation failed: %w", err)
	}

	return []AllocatedMigDevice{migDevice}, nil
}

// FabricManager device creation functions

// createFabricManagerDevices creates fabric partitions based on GPU capabilities and requirements.
func (s *DeviceState) createFabricManagerDevices(gpu *GpuInfo, config *configapi.FabricManagerConfig, claim *resourceapi.ResourceClaim) ([]AllocatedFabricPartition, error) {
	// Validate input parameters
	if err := s.validateResourceClaimForFabricManager(claim); err != nil {
		return nil, fmt.Errorf("resource claim validation failed: %w", err)
	}

	if err := s.validateFabricManagerConfig(config); err != nil {
		return nil, fmt.Errorf("FabricManager config validation failed: %w", err)
	}

	if err := s.validateGpuForFabricManagerCreation(gpu); err != nil {
		return nil, fmt.Errorf("GPU validation failed: %w", err)
	}

	// Generate partition name if not specified
	partitionName := config.Spec.PartitionName
	if partitionName == "" {
		partitionName = fmt.Sprintf("partition-%s", claim.UID)
	}

	// Get clique ID
	cliqueID := config.Spec.CliqueID
	if cliqueID == "" {
		cliqueID = s.getCliqueID(gpu)
	}

	// Create fabric partition
	fabricPartition := AllocatedFabricPartition{
		ParentUUID:    gpu.UUID,
		PartitionID:   0, // Will be assigned during preparation
		PartitionName: partitionName,
		CliqueID:      cliqueID,
	}

	return []AllocatedFabricPartition{fabricPartition}, nil
}

// prepareFabricPartitions prepares fabric partitions for use by containers.
func (s *DeviceState) prepareFabricPartitions(claimUID string, allocated []AllocatedFabricPartition) (*PreparedFabricPartitions, error) {
	// Validate input parameters
	if claimUID == "" {
		return nil, fmt.Errorf("claim UID cannot be empty")
	}

	if len(allocated) == 0 {
		return nil, fmt.Errorf("no fabric partitions to prepare")
	}

	prepared := &PreparedFabricPartitions{}

	for i, partition := range allocated {
		// Validate each partition before processing
		if err := s.validateAllocatedFabricPartition(partition, i); err != nil {
			return nil, fmt.Errorf("allocated fabric partition %d validation failed: %w", i, err)
		}

		if _, exists := s.allocatable[partition.ParentUUID]; !exists {
			return nil, fmt.Errorf("allocated GPU does not exist: %v", partition.ParentUUID)
		}

		parent := s.allocatable[partition.ParentUUID]

		// Validate GPU for FabricManager creation
		if err := s.validateGpuForFabricManagerCreation(parent.Gpu); err != nil {
			return nil, fmt.Errorf("GPU validation failed: %w", err)
		}

		// Create actual fabric partition using nvlib
		fabricInfo, err := s.nvdevlib.createFabricPartition(parent.Gpu, partition.PartitionName, partition.CliqueID)
		if err != nil {
			return nil, fmt.Errorf("failed to create fabric partition %s: %w", partition.PartitionName, err)
		}

		prepared.Partitions = append(prepared.Partitions, fabricInfo)
	}

	return prepared, nil
}

// unprepareFabricPartitions cleans up fabric partitions after container use.
func (s *DeviceState) unprepareFabricPartitions(claimUID string, partitions *PreparedFabricPartitions) error {
	if partitions == nil {
		return nil
	}

	for _, partition := range partitions.Partitions {
		// Delete the fabric partition using nvlib
		if err := s.nvdevlib.deleteFabricPartition(partition); err != nil {
			klog.Errorf("Failed to delete fabric partition %s: %v", partition.PartitionName, err)
			// Continue with other partitions even if one fails
		} else {
			klog.Infof("Successfully cleaned up fabric partition %s for claim %s", partition.PartitionName, claimUID)
		}
	}

	return nil
}

// validateAllocatedFabricPartition validates a single allocated fabric partition.
func (s *DeviceState) validateAllocatedFabricPartition(partition AllocatedFabricPartition, index int) error {
	if partition.ParentUUID == "" {
		return fmt.Errorf("parent UUID cannot be empty")
	}

	if partition.PartitionName == "" {
		return fmt.Errorf("partition name cannot be empty")
	}

	if partition.CliqueID == "" {
		return fmt.Errorf("clique ID cannot be empty")
	}

	return nil
}

// validateFabricManagerConfig validates the FabricManager configuration.
func (s *DeviceState) validateFabricManagerConfig(config *configapi.FabricManagerConfig) error {
	if config == nil {
		return fmt.Errorf("FabricManager config cannot be nil")
	}

	// Validate sharing strategy
	if config.Spec.Sharing != nil {
		if config.Spec.Sharing.Strategy != configapi.FabricManagerSharingStrategyExclusive {
			return fmt.Errorf("invalid sharing strategy: %s", config.Spec.Sharing.Strategy)
		}
	}

	return nil
}

// validateResourceClaimForFabricManager validates a resource claim for FabricManager allocation.
func (s *DeviceState) validateResourceClaimForFabricManager(claim *resourceapi.ResourceClaim) error {
	if claim == nil {
		return fmt.Errorf("resource claim cannot be nil")
	}

	if claim.UID == "" {
		return fmt.Errorf("resource claim UID cannot be empty")
	}

	if claim.Name == "" {
		return fmt.Errorf("resource claim name cannot be empty")
	}

	if claim.Namespace == "" {
		return fmt.Errorf("resource claim namespace cannot be empty")
	}

	return nil
}

// getCliqueID gets the clique ID for a GPU.
func (s *DeviceState) getCliqueID(gpu *GpuInfo) string {
	deviceHandle, ret := s.nvdevlib.nvmllib.DeviceGetHandleByUUID(gpu.UUID)
	if ret != nvml.SUCCESS {
		klog.Warningf("Failed to get device handle for GPU %s: %v", gpu.UUID, ret)
		return ""
	}

	// Get fabric info to extract clique ID
	fabricInfo, ret := deviceHandle.GetGpuFabricInfo()
	if ret != nvml.SUCCESS {
		klog.Warningf("Failed to get fabric info for GPU %s: %v", gpu.UUID, ret)
		return ""
	}

	return fmt.Sprintf("%d", fabricInfo.CliqueId)
}
