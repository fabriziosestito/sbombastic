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

package controller

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	sbombasticv1alpha1 "github.com/rancher/sbombastic/api/v1alpha1"
	"github.com/rancher/sbombastic/internal/messaging"
)

// ScanJobReconciler reconciles a ScanJob object
type ScanJobReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Publisher messaging.Publisher
}

// +kubebuilder:rbac:groups=sbombastic.rancher.io,resources=scanjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sbombastic.rancher.io,resources=scanjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sbombastic.rancher.io,resources=scanjobs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ScanJob object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *ScanJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling ScanJob")

	scanJob := &sbombasticv1alpha1.ScanJob{}
	if err := r.Get(ctx, req.NamespacedName, scanJob); err != nil {
		if errors.IsNotFound(err) {
			// Object not found, likely already deleted
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("unable to get ScanJob: %w", err)
	}

	// Check if resource is being deleted
	if !scanJob.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	if scanJob.Status.Phase == sbombasticv1alpha1.PhaseCompleted || scanJob.Status.Phase == sbombasticv1alpha1.PhaseFailed || scanJob.Status.Phase == sbombasticv1alpha1.PhaseInProgress {
		return ctrl.Result{}, nil
	}

	original := scanJob.DeepCopy()

	scanJob.InitializeConditions()

	reconcileResult, reconcileErr := r.reconcileScanJob(ctx, scanJob)

	// Update status if it changed
	if !equality.Semantic.DeepEqual(original.Status, scanJob.Status) {
		log.Info("Updating ScanJob status")
		if err := r.Status().Patch(ctx, scanJob, client.MergeFrom(original)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update ScanJob status: %w", err)
		}
	}

	return reconcileResult, reconcileErr
}

// reconcileScanJob implements the actual reconciliation logic
func (r *ScanJobReconciler) reconcileScanJob(ctx context.Context, scanJob *sbombasticv1alpha1.ScanJob) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	registry := &sbombasticv1alpha1.Registry{}
	if err := r.Get(ctx, client.ObjectKey{
		Name:      scanJob.Spec.Registry,
		Namespace: scanJob.Namespace,
	}, registry); err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "Registry not found", "registry", scanJob.Spec.Registry)
			scanJob.MarkFailed(sbombasticv1alpha1.ReasonRegistryNotFound, fmt.Sprintf("Registry %s not found", scanJob.Spec.Registry))
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("unable to get Registry %s: %w", scanJob.Spec.Registry, err)
	}

	registryData, err := json.Marshal(registry)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to marshal registry data: %w", err)
	}
	scanJob.Annotations = map[string]string{
		sbombasticv1alpha1.RegistryAnnotation: string(registryData),
	}

	if err := r.Update(ctx, scanJob); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update ScanJob with registry data: %w", err)
	}

	scanJob.MarkInProgress(sbombasticv1alpha1.ReasonProcessing, "Processing scan job")

	msg := messaging.CreateCatalog{
		ScanJobName:      scanJob.Name,
		ScanJobNamespace: scanJob.Namespace,
	}

	if err := r.Publisher.Publish(&msg); err != nil {
		log.Error(err, "Failed to publish NATS message")
		scanJob.MarkFailed(sbombasticv1alpha1.ReasonPublishFailed, fmt.Sprintf("Failed to publish to NATS: %v", err))
		return ctrl.Result{}, fmt.Errorf("unable to publish CreateSBOM message: %w", err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScanJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&sbombasticv1alpha1.ScanJob{}).
		Named("scanjob").
		Complete(r)
}
