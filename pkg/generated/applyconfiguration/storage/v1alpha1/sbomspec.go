/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1alpha1

import (
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// SBOMSpecApplyConfiguration represents a declarative configuration of the SBOMSpec type for use
// with apply.
type SBOMSpecApplyConfiguration struct {
	ImageMetadata *ImageMetadataApplyConfiguration `json:"imageMetadata,omitempty"`
	Data          *runtime.RawExtension            `json:"data,omitempty"`
}

// SBOMSpecApplyConfiguration constructs a declarative configuration of the SBOMSpec type for use with
// apply.
func SBOMSpec() *SBOMSpecApplyConfiguration {
	return &SBOMSpecApplyConfiguration{}
}

// WithImageMetadata sets the ImageMetadata field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the ImageMetadata field is set to the value of the last call.
func (b *SBOMSpecApplyConfiguration) WithImageMetadata(value *ImageMetadataApplyConfiguration) *SBOMSpecApplyConfiguration {
	b.ImageMetadata = value
	return b
}

// WithData sets the Data field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Data field is set to the value of the last call.
func (b *SBOMSpecApplyConfiguration) WithData(value runtime.RawExtension) *SBOMSpecApplyConfiguration {
	b.Data = &value
	return b
}
