package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage"

	storagev1alpha1 "github.com/kubewarden/sbomscanner/api/storage/v1alpha1"
)

// event is the payload sent via NATS for watch events.
type event struct {
	EventType  watch.EventType   `json:"eventType"`
	ObjectMeta metav1.ObjectMeta `json:"objectMeta"`
}

// natsWatcher subscribes to NATS watch events and broadcasts them locally.
type natsWatcher struct {
	nc               *nats.Conn
	subject          string
	watchBroadcaster *watch.Broadcaster
	logger           *slog.Logger
	store            *store
}

func newNatsWatcher(nc *nats.Conn, resource string, watchBroadcaster *watch.Broadcaster, store *store, logger *slog.Logger) *natsWatcher {
	subject := fmt.Sprintf("watch.%s", resource)

	return &natsWatcher{
		nc:               nc,
		subject:          subject,
		watchBroadcaster: watchBroadcaster,
		store:            store,
		logger:           logger.With("component", "nats-watcher", "subject", subject),
	}
}

// Start begins subscribing to NATS messages on the given subject.
func (w *natsWatcher) Start(ctx context.Context) error {
	sub, err := w.nc.Subscribe(w.subject, func(msg *nats.Msg) {
		if err := w.handleMessage(ctx, msg); err != nil {
			w.logger.ErrorContext(ctx, "Failed to handle NATS message",
				"error", err,
				"subject", msg.Subject,
			)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe to NATS subject %s: %w", w.subject, err)
	}

	w.logger.InfoContext(ctx, "Watch broadcaster started", "subject", w.subject)

	<-ctx.Done()

	w.logger.InfoContext(ctx, "Shutting down watcher", "subject", w.subject)
	w.watchBroadcaster.Shutdown()
	if err := sub.Unsubscribe(); err != nil {
		w.logger.ErrorContext(ctx, "Failed to unsubscribe from NATS", "error", err)
	}

	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("context error while shutting down watcher: %w", err)
	}

	return nil
}

// handleMessage processes a NATS message and broadcasts it locally.
func (w *natsWatcher) handleMessage(ctx context.Context, msg *nats.Msg) error {
	var payload event
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	key := fmt.Sprintf("%s/%s/%s/%s", storagev1alpha1.GroupName, w.store.table, payload.ObjectMeta.GetNamespace(), payload.ObjectMeta.GetName())
	obj := w.store.newFunc()

	// NOTE: For deleted events, we broadcast the event without fetching the object from the store.
	// Instead, we create a minimal object with just the metadata.
	if payload.EventType == watch.Deleted {
		obj, ok := obj.(metav1.Object)
		if !ok {
			return fmt.Errorf("object does not implement metav1.Object: %T", obj)
		}
		copyObjectMeta(obj, payload.ObjectMeta)
	} else if err := w.store.Get(ctx, key, storage.GetOptions{}, obj); err != nil {
		if storage.IsNotFound(err) {
			// Object not found, possibly deleted after the event was sent.
			w.logger.DebugContext(ctx, "Object not found in store while handling message, skipping",
				"key", key,
			)
		}
		return fmt.Errorf("failed to get object from store while handling message: %w", err)
	}

	if err := w.watchBroadcaster.Action(payload.EventType, obj); err != nil {
		return fmt.Errorf("failed to broadcast action while handling message: %w", err)
	}

	w.logger.DebugContext(ctx, "Broadcasted watch event",
		"type", payload.EventType,
		"objectMeta", payload.ObjectMeta,
	)
	return nil
}

// natsBroadcaster broadcasts watch events using NATS.
type natsBroadcaster struct {
	nc               *nats.Conn
	subject          string
	watchBroadcaster *watch.Broadcaster
	logger           *slog.Logger
}

func newNatsBroadcaster(nc *nats.Conn, resource string, watchBroadcaster *watch.Broadcaster, logger *slog.Logger) *natsBroadcaster {
	subject := fmt.Sprintf("watch.%s", resource)

	return &natsBroadcaster{
		nc:               nc,
		subject:          subject,
		watchBroadcaster: watchBroadcaster,
		logger:           logger.With("component", "nats-broadcaster", "subject", subject),
	}
}

// Watch returns a watch.Interface that receives events from this broadcaster.
func (b *natsBroadcaster) Watch() (watch.Interface, error) {
	watch, err := b.watchBroadcaster.Watch()
	if err != nil {
		return nil, fmt.Errorf("failed to add new watcher: %w", err)
	}

	return watch, nil
}

// WatchWithPrefix returns a watch.Interface that receives a prefix of events.
func (b *natsBroadcaster) WatchWithPrefix(events []watch.Event) (watch.Interface, error) {
	watch, err := b.watchBroadcaster.WatchWithPrefix(events)
	if err != nil {
		return nil, fmt.Errorf("failed to add new watcher with prefix: %w", err)
	}

	return watch, nil
}

// Action broadcasts an event to all local watchers.
func (b *natsBroadcaster) Action(eventType watch.EventType, obj runtime.Object) error {
	objectMeta, err := objectMetaFromRuntimeObject(obj)
	if err != nil {
		return fmt.Errorf("failed to get object meta: %w", err)
	}

	payload := event{
		EventType:  eventType,
		ObjectMeta: objectMeta,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	if err := b.nc.Publish(b.subject, payloadBytes); err != nil {
		return fmt.Errorf("publish to NATS: %w", err)
	}

	b.logger.Debug("Published watch event to NATS", "eventType", eventType, "objectMeta", objectMeta)

	return nil
}

func objectMetaFromRuntimeObject(obj runtime.Object) (metav1.ObjectMeta, error) {
	metaAccessor, err := meta.Accessor(obj)
	if err != nil {
		return metav1.ObjectMeta{}, fmt.Errorf("failed to obtain object meta accessor: %w", err)
	}

	return metav1.ObjectMeta{
		Name:              metaAccessor.GetName(),
		Namespace:         metaAccessor.GetNamespace(),
		UID:               metaAccessor.GetUID(),
		ResourceVersion:   metaAccessor.GetResourceVersion(),
		Generation:        metaAccessor.GetGeneration(),
		CreationTimestamp: metaAccessor.GetCreationTimestamp(),
		DeletionTimestamp: metaAccessor.GetDeletionTimestamp(),
		Labels:            metaAccessor.GetLabels(),
		Annotations:       metaAccessor.GetAnnotations(),
		OwnerReferences:   metaAccessor.GetOwnerReferences(),
		Finalizers:        metaAccessor.GetFinalizers(),
		ManagedFields:     metaAccessor.GetManagedFields(),
	}, nil
}

func copyObjectMeta(dst metav1.Object, src metav1.ObjectMeta) {
	dst.SetName(src.Name)
	dst.SetNamespace(src.Namespace)
	dst.SetUID(src.UID)
	dst.SetResourceVersion(src.ResourceVersion)
	dst.SetGeneration(src.Generation)
	dst.SetCreationTimestamp(src.CreationTimestamp)
	dst.SetDeletionTimestamp(src.DeletionTimestamp)
	dst.SetLabels(src.Labels)
	dst.SetAnnotations(src.Annotations)
	dst.SetOwnerReferences(src.OwnerReferences)
	dst.SetFinalizers(src.Finalizers)
	dst.SetManagedFields(src.ManagedFields)
}
