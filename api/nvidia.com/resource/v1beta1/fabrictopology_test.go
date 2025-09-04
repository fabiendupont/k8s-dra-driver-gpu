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
	"strings"
	"testing"

	"k8s.io/utils/ptr"
)

func TestFabricTopologyStrategy(t *testing.T) {
	tests := []struct {
		name     string
		strategy FabricTopologyStrategy
		wantErr  bool
	}{
		{
			name:     "Valid Auto strategy",
			strategy: FabricTopologyAuto,
			wantErr:  false,
		},
		{
			name:     "Valid Manual strategy",
			strategy: FabricTopologyManual,
			wantErr:  false,
		},
		{
			name:     "Valid Disabled strategy",
			strategy: FabricTopologyDisabled,
			wantErr:  false,
		},
		{
			name:     "Invalid strategy",
			strategy: "Invalid",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.strategy.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("FabricTopologyStrategy.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultFabricTopologyConfig(t *testing.T) {
	config := DefaultFabricTopologyConfig()

	if config == nil {
		t.Fatal("DefaultFabricTopologyConfig returned nil")
	}

	if config.Strategy != FabricTopologyAuto {
		t.Errorf("Expected strategy 'Auto', got '%s'", config.Strategy)
	}

	if config.PartitionSize == nil || *config.PartitionSize != FabricPartitionSize4 {
		t.Errorf("Expected partition size 4, got %v", config.PartitionSize)
	}

	if config.MinBandwidth == nil || *config.MinBandwidth != 600.0 {
		t.Errorf("Expected min bandwidth 600.0, got %v", config.MinBandwidth)
	}

	if config.MaxLatency == nil || *config.MaxLatency != 0.1 {
		t.Errorf("Expected max latency 0.1, got %v", config.MaxLatency)
	}

	if config.PreferNVLink == nil || *config.PreferNVLink != true {
		t.Errorf("Expected prefer NVLink true, got %v", config.PreferNVLink)
	}
}

func TestFabricTopologyConfigIsAuto(t *testing.T) {
	tests := []struct {
		name   string
		config *FabricTopologyConfig
		want   bool
	}{
		{
			name:   "Nil config",
			config: nil,
			want:   false,
		},
		{
			name:   "Auto strategy",
			config: &FabricTopologyConfig{Strategy: FabricTopologyAuto},
			want:   true,
		},
		{
			name:   "Manual strategy",
			config: &FabricTopologyConfig{Strategy: FabricTopologyManual},
			want:   false,
		},
		{
			name:   "Disabled strategy",
			config: &FabricTopologyConfig{Strategy: FabricTopologyDisabled},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.IsAuto(); got != tt.want {
				t.Errorf("FabricTopologyConfig.IsAuto() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFabricTopologyConfigIsManual(t *testing.T) {
	tests := []struct {
		name   string
		config *FabricTopologyConfig
		want   bool
	}{
		{
			name:   "Nil config",
			config: nil,
			want:   false,
		},
		{
			name:   "Auto strategy",
			config: &FabricTopologyConfig{Strategy: FabricTopologyAuto},
			want:   false,
		},
		{
			name:   "Manual strategy",
			config: &FabricTopologyConfig{Strategy: FabricTopologyManual},
			want:   true,
		},
		{
			name:   "Disabled strategy",
			config: &FabricTopologyConfig{Strategy: FabricTopologyDisabled},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.IsManual(); got != tt.want {
				t.Errorf("FabricTopologyConfig.IsManual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFabricTopologyConfigIsDisabled(t *testing.T) {
	tests := []struct {
		name   string
		config *FabricTopologyConfig
		want   bool
	}{
		{
			name:   "Nil config",
			config: nil,
			want:   false,
		},
		{
			name:   "Auto strategy",
			config: &FabricTopologyConfig{Strategy: FabricTopologyAuto},
			want:   false,
		},
		{
			name:   "Manual strategy",
			config: &FabricTopologyConfig{Strategy: FabricTopologyManual},
			want:   false,
		},
		{
			name:   "Disabled strategy",
			config: &FabricTopologyConfig{Strategy: FabricTopologyDisabled},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.IsDisabled(); got != tt.want {
				t.Errorf("FabricTopologyConfig.IsDisabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFabricTopologyConfigGetPartitionSize(t *testing.T) {
	tests := []struct {
		name    string
		config  *FabricTopologyConfig
		want    int
		wantErr bool
	}{
		{
			name:    "Nil config",
			config:  nil,
			want:    0,
			wantErr: true,
		},
		{
			name:    "Auto strategy with partition size",
			config:  &FabricTopologyConfig{Strategy: FabricTopologyAuto, PartitionSize: ptr.To(FabricPartitionSize8)},
			want:    FabricPartitionSize8,
			wantErr: false,
		},
		{
			name:    "Auto strategy without partition size",
			config:  &FabricTopologyConfig{Strategy: FabricTopologyAuto},
			want:    FabricPartitionSize4,
			wantErr: false,
		},
		{
			name:    "Manual strategy",
			config:  &FabricTopologyConfig{Strategy: FabricTopologyManual},
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.config.GetPartitionSize()
			if (err != nil) != tt.wantErr {
				t.Errorf("FabricTopologyConfig.GetPartitionSize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("FabricTopologyConfig.GetPartitionSize() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFabricTopologyConfigGetPartitionIDs(t *testing.T) {
	tests := []struct {
		name    string
		config  *FabricTopologyConfig
		want    []string
		wantErr bool
	}{
		{
			name:    "Nil config",
			config:  nil,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Manual strategy with partition IDs",
			config:  &FabricTopologyConfig{Strategy: FabricTopologyManual, PartitionIDs: []string{"partition-1", "partition-2"}},
			want:    []string{"partition-1", "partition-2"},
			wantErr: false,
		},
		{
			name:    "Manual strategy without partition IDs",
			config:  &FabricTopologyConfig{Strategy: FabricTopologyManual},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Auto strategy",
			config:  &FabricTopologyConfig{Strategy: FabricTopologyAuto},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.config.GetPartitionIDs()
			if (err != nil) != tt.wantErr {
				t.Errorf("FabricTopologyConfig.GetPartitionIDs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("FabricTopologyConfig.GetPartitionIDs() = %v, want %v", got, tt.want)
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("FabricTopologyConfig.GetPartitionIDs()[%d] = %v, want %v", i, v, tt.want[i])
				}
			}
		})
	}
}

func TestFabricTopologyConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *FabricTopologyConfig
		wantErr bool
	}{
		{
			name:    "Nil config",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "Valid auto config",
			config:  &FabricTopologyConfig{Strategy: FabricTopologyAuto, PartitionSize: ptr.To(FabricPartitionSize4)},
			wantErr: false,
		},
		{
			name:    "Valid manual config",
			config:  &FabricTopologyConfig{Strategy: FabricTopologyManual, PartitionIDs: []string{"partition-1"}},
			wantErr: false,
		},
		{
			name:    "Valid disabled config",
			config:  &FabricTopologyConfig{Strategy: FabricTopologyDisabled},
			wantErr: false,
		},
		{
			name:    "Invalid strategy",
			config:  &FabricTopologyConfig{Strategy: "Invalid"},
			wantErr: true,
		},
		{
			name:    "Auto with invalid partition size",
			config:  &FabricTopologyConfig{Strategy: FabricTopologyAuto, PartitionSize: ptr.To(3)},
			wantErr: true,
		},
		{
			name:    "Auto with partition IDs",
			config:  &FabricTopologyConfig{Strategy: FabricTopologyAuto, PartitionIDs: []string{"partition-1"}},
			wantErr: true,
		},
		{
			name:    "Manual without partition IDs",
			config:  &FabricTopologyConfig{Strategy: FabricTopologyManual},
			wantErr: true,
		},
		{
			name:    "Manual with empty partition ID",
			config:  &FabricTopologyConfig{Strategy: FabricTopologyManual, PartitionIDs: []string{"", "partition-2"}},
			wantErr: true,
		},
		{
			name:    "Manual with partition size",
			config:  &FabricTopologyConfig{Strategy: FabricTopologyManual, PartitionSize: ptr.To(FabricPartitionSize4)},
			wantErr: true,
		},
		{
			name:    "Disabled with partition size",
			config:  &FabricTopologyConfig{Strategy: FabricTopologyDisabled, PartitionSize: ptr.To(FabricPartitionSize4)},
			wantErr: true,
		},
		{
			name:    "Disabled with partition IDs",
			config:  &FabricTopologyConfig{Strategy: FabricTopologyDisabled, PartitionIDs: []string{"partition-1"}},
			wantErr: true,
		},
		{
			name:    "Negative min bandwidth",
			config:  &FabricTopologyConfig{Strategy: FabricTopologyAuto, MinBandwidth: ptr.To(-1.0)},
			wantErr: true,
		},
		{
			name:    "Negative max latency",
			config:  &FabricTopologyConfig{Strategy: FabricTopologyAuto, MaxLatency: ptr.To(-1.0)},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("FabricTopologyConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFabricTopologyConfigVirtualizationModelValidation(t *testing.T) {
	testCases := []struct {
		name                string
		virtualizationModel string
		wantErr             bool
		expectedErrorSubstr string
	}{
		{
			name:                "Valid BareMetal model",
			virtualizationModel: FabricVirtualizationBareMetal,
			wantErr:             false,
		},
		{
			name:                "Valid FullPassthrough model",
			virtualizationModel: FabricVirtualizationFullPassthrough,
			wantErr:             false,
		},
		{
			name:                "Valid SharedNVSwitch model",
			virtualizationModel: FabricVirtualizationSharedNVSwitch,
			wantErr:             false,
		},
		{
			name:                "Valid VGPU model",
			virtualizationModel: FabricVirtualizationVGPU,
			wantErr:             false,
		},
		{
			name:                "Invalid model",
			virtualizationModel: "InvalidModel",
			wantErr:             true,
			expectedErrorSubstr: "unknown virtualization model",
		},
		{
			name:                "Empty model",
			virtualizationModel: "",
			wantErr:             true,
			expectedErrorSubstr: "unknown virtualization model",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := &FabricTopologyConfig{
				Strategy:            FabricTopologyAuto,
				VirtualizationModel: &tc.virtualizationModel,
			}

			err := config.Validate()

			if tc.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tc.expectedErrorSubstr != "" && !strings.Contains(err.Error(), tc.expectedErrorSubstr) {
					t.Errorf("Expected error to contain '%s', got: %v", tc.expectedErrorSubstr, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestFabricTopologyConfigDefaultVirtualizationModel(t *testing.T) {
	config := DefaultFabricTopologyConfig()

	if config.VirtualizationModel == nil {
		t.Error("Expected default virtualization model to be set")
	}

	if *config.VirtualizationModel != FabricVirtualizationBareMetal {
		t.Errorf("Expected default virtualization model to be BareMetal, got: %s", *config.VirtualizationModel)
	}
}
