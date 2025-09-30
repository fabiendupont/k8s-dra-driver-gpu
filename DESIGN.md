# Dynamic MIG Implementation Design

## Overview
This document captures the design decisions and architectural patterns developed during the dynamic MIG implementation work.

## Key Architectural Decisions

### 1. Architecture-Aware MIG Behavior
- **Hopper+ GPUs (H100, H200, B200, etc.)**: Support MIG + FabricManager simultaneously
- **Ampere GPUs (A100)**: MIG-enabled GPUs are excluded from fabric partitions
- **Rationale**: Hardware capabilities differ between architectures

### 2. Capability-Based Design
- Functions are named based on capabilities rather than specific architectures
- Examples: `createMigDevices()` instead of `createHopperMigDevices()`
- Uses NVML constants (`DEVICE_ARCH_*`) instead of string literals

### 3. FabricManager Integration Patterns
- **Bare Metal**: Direct partition allocation via FabricManager
- **Full Passthrough**: Topology information passed to guest VMs
- **SR-IOV**: Partitioned resources with QoS guarantees

### 4. MIG Profile Selection Strategy
- **Intelligent scoring**: Based on compute slices, memory footprint, placement options
- **Architecture-specific optimizations**: 
  - Hopper+: Prefer single-slice profiles for FabricManager compatibility
  - Ampere: Prefer 2-4 slice profiles for balanced performance
- **Strategy-aware**: Time-slicing prefers smaller profiles

### 5. Placement Strategies
- **Balanced**: Optimal resource utilization
- **Fabric-Aware**: Optimize for FabricManager integration
- **Best-Fit**: Minimize fragmentation

## Implementation Patterns

### Device State Management
```go
type DeviceState struct {
    // Core managers
    cdi         *CDIHandler
    tsManager   *TimeSlicingManager
    mpsManager  *MpsManager
    fmManager   *FabricManagerManager
    
    // Device tracking
    allocatable AllocatableDevices
    nvdevlib    *deviceLib
}
```

### MIG Device Lifecycle
1. **Discovery**: Enumerate GPUs and MIG profiles
2. **Validation**: Check capabilities and constraints
3. **Selection**: Choose optimal profile and placement
4. **Creation**: Allocate MIG device with placement
5. **Preparation**: Make device available to containers
6. **Cleanup**: Unprepare and delete MIG device

### Validation Hierarchy
- **Input Validation**: Check parameters and constraints
- **GPU State Validation**: Verify device capabilities
- **MIG Device Validation**: Ensure profile compatibility
- **Placement Validation**: Confirm placement integrity

## Test Patterns

### Mock Infrastructure
- **go-nvml mocks**: Comprehensive GPU simulation (A100, H100, H200, B200)
- **go-nvlib mocks**: MIG profile parsing and validation
- **Architecture-specific**: Different behaviors per GPU type

### Test Categories
- **Unit Tests**: Individual function behavior
- **Integration Tests**: End-to-end MIG lifecycle
- **Architecture Tests**: Hopper+ vs Ampere behavior
- **Feature Gate Tests**: Gated vs non-gated paths

## Future Considerations

### Extensibility
- **New Architectures**: Easy addition of new GPU types
- **New Strategies**: Pluggable placement algorithms
- **New Features**: Feature-gated functionality

### Performance
- **Caching**: Profile and placement caching
- **Parallelization**: Concurrent device operations
- **Optimization**: Profile selection algorithms

## Dependencies

### External Libraries
- **go-nvml**: Raw NVML API bindings
- **go-nvlib**: Higher-level MIG abstractions
- **Kubernetes DRA**: Dynamic Resource Allocation

### Internal Components
- **FabricManager**: Fabric topology management
- **CDI**: Container Device Interface
- **Feature Gates**: Conditional functionality
