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

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ScanJobSpec defines the desired state of ScanJob.
type ScanJobSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of ScanJob. Edit scanjob_types.go to remove/update
	Foo string `json:"foo,omitempty"`
}

const (
	ConditionTypeComplete = "Complete"
	ConditionTypeFailed   = "Failed"

	PhaseQueued     = "Queued"
	PhaseInProgress = "InProgress"
	PhaseCompleted  = "Completed"
	PhaseFailed     = "Failed"

	ReasonAwaitingTurn  = "AwaitingTurn"
	ReasonProcessing    = "Processing"
	ReasonCompleted     = "Completed"
	ReasonFailed        = "Failed"
	ReasonPublishFailed = "PublishFailed"
)

// ScanJobStatus defines the observed state of ScanJob.
type ScanJobStatus struct {
	// Phase represents the current phase of the scan job: Queued, InProgress, Completed, Failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions represent the latest available observations of ScanJob state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// StartTime is when the job began processing (not just queued)
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the job completed or failed
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ScanJob is the Schema for the scanjobs API.
type ScanJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ScanJobSpec   `json:"spec,omitempty"`
	Status ScanJobStatus `json:"status,omitempty"`
}

// InitializeConditions initializes status fields and conditions
func (s *ScanJob) InitializeConditions() {
	s.Status.Phase = PhaseQueued

	s.Status.Conditions = []metav1.Condition{}
	meta.SetStatusCondition(&s.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeComplete,
		Status:             metav1.ConditionFalse,
		Reason:             "Initializing",
		Message:            "Scan job created",
		ObservedGeneration: s.Generation,
	})
	meta.SetStatusCondition(&s.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeFailed,
		Status:             metav1.ConditionFalse,
		Reason:             "Initializing",
		Message:            "Scan job created",
		ObservedGeneration: s.Generation,
	})
}

// MarkQueued marks the job as queued
func (s *ScanJob) MarkQueued(reason, message string) {
	s.Status.Phase = PhaseQueued

	// Clear conditions
	meta.SetStatusCondition(&s.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeComplete,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            "Job queued",
		ObservedGeneration: s.Generation,
	})

	meta.SetStatusCondition(&s.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeFailed,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: s.Generation,
	})
}

// MarkInProgress marks the job as in progress
func (s *ScanJob) MarkInProgress(reason, message string) {
	s.Status.Phase = PhaseInProgress

	now := metav1.Now()
	s.Status.StartTime = &now

	meta.SetStatusCondition(&s.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeComplete,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            "Job in progress",
		ObservedGeneration: s.Generation,
	})

	meta.SetStatusCondition(&s.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeFailed,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: s.Generation,
	})
}

// MarkComplete marks the job as complete
func (s *ScanJob) MarkComplete(reason, message string) {
	s.Status.Phase = PhaseCompleted

	now := metav1.Now()
	s.Status.CompletionTime = &now

	meta.SetStatusCondition(&s.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeComplete,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: s.Generation,
	})

	meta.SetStatusCondition(&s.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeFailed,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            "Job completed successfully",
		ObservedGeneration: s.Generation,
	})
}

// MarkFailed marks the job as failed
func (s *ScanJob) MarkFailed(reason, message string) {
	s.Status.Phase = PhaseFailed

	now := metav1.Now()
	s.Status.CompletionTime = &now

	meta.SetStatusCondition(&s.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeFailed,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: s.Generation,
	})

	meta.SetStatusCondition(&s.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeComplete,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            "Job failed",
		ObservedGeneration: s.Generation,
	})
}

// +kubebuilder:object:root=true

// ScanJobList contains a list of ScanJob.
type ScanJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ScanJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ScanJob{}, &ScanJobList{})
}
