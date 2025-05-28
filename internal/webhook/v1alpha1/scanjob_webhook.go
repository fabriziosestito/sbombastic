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
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	sbombasticv1alpha1 "github.com/rancher/sbombastic/api/v1alpha1"
)

// log is for logging in this package.
var scanJobLog = logf.Log.WithName("scanjob-resource")

// SetupScanJobWebhookWithManager registers the webhook for ScanJob in the manager.
func SetupScanJobWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&sbombasticv1alpha1.ScanJob{}).
		WithValidator(&ScanJobCustomValidator{}).
		Complete()
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-sbombastic-sbombastic-rancher-io-v1alpha1-scanjob,mutating=false,failurePolicy=fail,sideEffects=None,groups=sbombastic.sbombastic.rancher.io,resources=scanjobs,verbs=create;update,versions=v1alpha1,name=vscanjob-v1alpha1.kb.io,admissionReviewVersions=v1

// ScanJobCustomValidator struct is responsible for validating the ScanJob resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type ScanJobCustomValidator struct {
	client client.Client
}

var _ webhook.CustomValidator = &ScanJobCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type ScanJob.
func (v *ScanJobCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	scanJob, ok := obj.(*sbombasticv1alpha1.ScanJob)
	if !ok {
		return nil, fmt.Errorf("expected a ScanJob object but got %T", obj)
	}
	scanJobLog.Info("Validation for ScanJob upon creation", "name", scanJob.GetName())

	existingJobs := &sbombasticv1alpha1.ScanJobList{}
	if err := v.client.List(ctx, existingJobs, client.InNamespace(scanJob.GetNamespace())); err != nil {
		return nil, fmt.Errorf("failed to list ScanJobs in namespace %s: %w", scanJob.GetNamespace(), err)
	}

	for _, existingJob := range existingJobs.Items {
		if existingJob.Spec.Registry == scanJob.Spec.Registry && existingJob.Status.Phase != sbombasticv1alpha1.PhaseCompleted {
			return nil, fmt.Errorf("a ScanJob with the same registry %s is already in progress", scanJob.Spec.Registry)
		}
	}

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type ScanJob.
func (v *ScanJobCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldScanJob, ok := oldObj.(*sbombasticv1alpha1.ScanJob)
	if !ok {
		return nil, fmt.Errorf("expected a ScanJob object for the oldObj but got %T", oldObj)
	}
	scanJob, ok := newObj.(*sbombasticv1alpha1.ScanJob)
	if !ok {
		return nil, fmt.Errorf("expected a ScanJob object for the newObj but got %T", newObj)
	}
	scanJobLog.Info("Validation for ScanJob upon update", "name", scanJob.GetName())

	if oldScanJob.Spec.Registry != scanJob.Spec.Registry {
		return nil, errors.New("field .spec.registry is immutable and cannot be updated")
	}

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type ScanJob.
func (v *ScanJobCustomValidator) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	scanJob, ok := obj.(*sbombasticv1alpha1.ScanJob)
	if !ok {
		return nil, fmt.Errorf("expected a ScanJob object but got %T", obj)
	}
	scanJobLog.Info("Validation for ScanJob upon deletion", "name", scanJob.GetName())

	return nil, nil
}
