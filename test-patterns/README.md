# Test Patterns Reference

This document captures reusable test patterns developed during the dynamic MIG implementation.

## Mock Device Setup

### Basic GPU Mock
```go
func createTestDeviceLib(arch device.Architecture) *deviceLib {
    var mockNvmlInterface nvml.Interface
    switch arch {
    case device.ArchitectureA100:
        mockNvmlInterface = dgxa100.New()
    case device.ArchitectureH100:
        mockNvmlInterface = dgxh100.New()
    case device.ArchitectureH200:
        mockNvmlInterface = dgxh200.New()
    case device.ArchitectureB200:
        mockNvmlInterface = dgxb200.New()
    default:
        panic(fmt.Sprintf("unsupported architecture: %s", arch))
    }
    
    mockDeviceInterface := device.NewMockInterfaceWithNvml(mockNvmlInterface, arch)
    return &deviceLib{
        Interface: mockDeviceInterface,
        nvmllib:   mockNvmlInterface,
    }
}
```

### MIG Profile Testing
```go
func TestMigProfileDiscovery(t *testing.T) {
    server := dgxa100.New()
    device, ok := server.Devices[0].(*dgxa100.Device)
    require.True(t, ok)
    
    // Test profile enumeration
    for i := 0; i < nvml.GPU_INSTANCE_PROFILE_COUNT; i++ {
        giProfileInfo, ret := device.GetGpuInstanceProfileInfo(i)
        if ret == nvml.ERROR_NOT_SUPPORTED {
            continue
        }
        assert.Equal(t, nvml.SUCCESS, ret)
        assert.Greater(t, giProfileInfo.MemorySizeMB, uint64(0))
    }
}
```

## Architecture-Specific Testing

### Hopper+ Behavior
```go
func TestHopperMigFabricIntegration(t *testing.T) {
    h100Gpu := &GpuInfo{
        UUID:         "GPU-hopper-uuid",
        architecture: nvml.DEVICE_ARCH_HOPPER,
        migCapable:   true,
        migEnabled:   true, // Hopper+ supports MIG + Fabric
    }
    
    // Test that Hopper+ MIG devices can participate in fabric
    assert.True(t, deviceState.isHopperArchitecture(h100Gpu))
    assert.True(t, deviceState.isMigCapable(h100Gpu))
    assert.True(t, deviceState.isMigEnabled(h100Gpu))
}
```

### Ampere Behavior
```go
func TestAmpereMigFabricExclusion(t *testing.T) {
    a100Gpu := &GpuInfo{
        UUID:         "GPU-ampere-uuid", 
        architecture: nvml.DEVICE_ARCH_AMPERE,
        migCapable:   true,
        migEnabled:   true, // Ampere MIG excludes from fabric
    }
    
    // Test that Ampere MIG devices are excluded from fabric
    assert.False(t, deviceState.isHopperArchitecture(a100Gpu))
    assert.True(t, deviceState.isMigCapable(a100Gpu))
    assert.True(t, deviceState.isMigEnabled(a100Gpu))
}
```

## Validation Testing

### Input Validation
```go
func TestMigValidation(t *testing.T) {
    testCases := []struct {
        name    string
        gpu     *GpuInfo
        config  *configapi.MigDeviceConfig
        claim   *resourceapi.ResourceClaim
        wantErr bool
    }{
        {
            name:    "ValidInputs",
            gpu:     createValidGpu(),
            config:  &configapi.MigDeviceConfig{},
            claim:   createValidClaim(),
            wantErr: false,
        },
        {
            name:    "NilGPU",
            gpu:     nil,
            config:  &configapi.MigDeviceConfig{},
            claim:   createValidClaim(),
            wantErr: true,
        },
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            err := deviceState.validateMigCreationInputs(tc.gpu, tc.config, tc.claim)
            if tc.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

### GPU State Validation
```go
func TestGpuStateValidation(t *testing.T) {
    // Test MIG-enabled GPU
    migGpu := &GpuInfo{
        UUID:       "mig-gpu-uuid",
        migEnabled: true,
        migCapable: true,
    }
    
    err := deviceState.validateGpuForMigCreation(migGpu)
    assert.NoError(t, err)
    
    // Test non-MIG GPU
    nonMigGpu := &GpuInfo{
        UUID:       "non-mig-gpu-uuid",
        migEnabled: false,
        migCapable: false,
    }
    
    err = deviceState.validateGpuForMigCreation(nonMigGpu)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "MIG mode not enabled")
}
```

## Feature Gate Testing

### Gated vs Non-Gated
```go
func TestFeatureGateBehavior(t *testing.T) {
    // Test with feature gate enabled
    originalValue := featuregates.Enabled(featuregates.FabricTopologySupport)
    defer func() {
        // Restore original value
    }()
    
    // Test behavior when gate is enabled
    if featuregates.Enabled(featuregates.FabricTopologySupport) {
        assert.NotNil(t, deviceState.fmManager)
    } else {
        assert.Nil(t, deviceState.fmManager)
    }
}
```

## Error Handling Testing

### Graceful Degradation
```go
func TestErrorHandling(t *testing.T) {
    // Test invalid profile
    invalidProfile := "invalid-profile"
    profile, err := deviceState.selectMigProfile(gpu, &configapi.MigDeviceConfig{})
    if err != nil {
        assert.Empty(t, profile)
        assert.Error(t, err)
    }
    
    // Test device creation failure
    migDevices, err := deviceState.createMigDevices(gpu, config, claim)
    if err != nil {
        assert.Empty(t, migDevices)
        assert.Error(t, err)
    }
}
```

## Performance Testing

### Profile Selection Performance
```go
func TestProfileSelectionPerformance(t *testing.T) {
    gpu := createLargeProfileGpu() // GPU with many profiles
    
    start := time.Now()
    profile := deviceState.selectMigProfile(gpu, config)
    duration := time.Since(start)
    
    assert.NotEmpty(t, profile)
    assert.Less(t, duration, 100*time.Millisecond) // Should be fast
}
```

## Integration Testing

### End-to-End MIG Lifecycle
```go
func TestMigLifecycle(t *testing.T) {
    // 1. Discover GPU
    gpu := deviceState.allocatable["test-gpu-uuid"]
    assert.NotNil(t, gpu)
    
    // 2. Create MIG device
    migDevices, err := deviceState.createMigDevices(gpu.Gpu, config, claim)
    assert.NoError(t, err)
    assert.Len(t, migDevices, 1)
    
    // 3. Prepare MIG device
    prepared, err := deviceState.prepareMigDevices(claimUID, &AllocatedMigDevices{Devices: migDevices})
    assert.NoError(t, err)
    assert.Len(t, prepared.Devices, 1)
    
    // 4. Cleanup
    err = deviceState.unprepareMigDevices(claimUID, prepared)
    assert.NoError(t, err)
}
