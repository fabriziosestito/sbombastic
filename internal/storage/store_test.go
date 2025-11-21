package storage

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/utils/ptr"

	"github.com/kubewarden/sbomscanner/api/storage/v1alpha1"
)

const keyPrefix = "/storage.sbomscanner.kubewarden.io/sboms"

type storeTestSuite struct {
	suite.Suite
	store       *store
	db          *pgxpool.Pool
	broadcaster *watch.Broadcaster
	watcher     *natsWatcher
	pgContainer *postgres.PostgresContainer
	natsServer  *server.Server
	nc          *nats.Conn
}

func (suite *storeTestSuite) SetupSuite() {
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpassword"),
		postgres.BasicWaitStrategies(),
	)
	suite.Require().NoError(err, "failed to start postgres container")
	suite.pgContainer = pgContainer

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	suite.Require().NoError(err, "failed to get connection string")

	db, err := pgxpool.New(ctx, connStr)
	suite.Require().NoError(err, "failed to create connection pool")
	suite.db = db

	_, err = db.Exec(ctx, CreateSBOMTableSQL)
	suite.Require().NoError(err, "failed to create SBOM table")

	// Setup NATS
	opts := test.DefaultTestOptions
	opts.Port = -1 // Use a random port
	opts.JetStream = true
	opts.StoreDir = suite.T().TempDir()
	suite.natsServer = test.RunServer(&opts)

	nc, err := nats.Connect(suite.natsServer.ClientURL())
	suite.Require().NoError(err, "failed to connect to NATS")
	suite.nc = nc
}

func (suite *storeTestSuite) TearDownSuite() {
	if suite.db != nil {
		suite.db.Close()
	}

	if suite.pgContainer != nil {
		err := suite.pgContainer.Terminate(context.Background())
		suite.Require().NoError(err, "failed to terminate postgres container")
	}

	if suite.nc != nil {
		suite.nc.Close()
	}

	if suite.natsServer != nil {
		suite.natsServer.Shutdown()
	}
}

func (suite *storeTestSuite) SetupTest() {
	_, err := suite.db.Exec(suite.T().Context(), "TRUNCATE TABLE sboms")
	suite.Require().NoError(err, "failed to truncate table")

	watchBroadcaster := watch.NewBroadcaster(1000, watch.WaitIfChannelFull)
	natsBroadcaster := newNatsBroadcaster(suite.nc, "sboms", watchBroadcaster, slog.Default())
	store := &store{
		db:          suite.db,
		broadcaster: natsBroadcaster,
		table:       "sboms",
		newFunc:     func() runtime.Object { return &v1alpha1.SBOM{} },
		newListFunc: func() runtime.Object { return &v1alpha1.SBOMList{} },
		logger:      slog.Default(),
	}
	natsWatcher := newNatsWatcher(suite.nc, "sboms", watchBroadcaster, store, slog.Default())

	suite.store = store
	suite.broadcaster = watchBroadcaster
	suite.watcher = natsWatcher
}

func TestStoreTestSuite(t *testing.T) {
	suite.Run(t, &storeTestSuite{})
}

func (suite *storeTestSuite) TestCreate() {
	sbom := &v1alpha1.SBOM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
	}

	key := keyPrefix + "/default/test"
	out := &v1alpha1.SBOM{}
	err := suite.store.Create(context.Background(), key, sbom, out, 0)
	suite.Require().NoError(err)

	suite.Equal(sbom, out)
	suite.Equal("1", out.ResourceVersion)

	err = suite.store.Create(context.Background(), key, sbom, out, 0)
	suite.Require().Equal(storage.NewKeyExistsError(key, 0).Error(), err.Error())
}

func (suite *storeTestSuite) TestDelete() {
	sbom := &v1alpha1.SBOM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
	}

	key := keyPrefix + "/default/test"

	tests := []struct {
		name             string
		preconditions    *storage.Preconditions
		validateDeletion storage.ValidateObjectFunc
		expectedError    error
	}{
		{
			name:          "happy path",
			preconditions: &storage.Preconditions{},
			validateDeletion: func(_ context.Context, _ runtime.Object) error {
				return nil
			},
			expectedError: nil,
		},
		{
			name:          "deletion fails with incorrect UID precondition",
			preconditions: &storage.Preconditions{UID: ptr.To(types.UID("incorrect-uid"))},
			validateDeletion: func(_ context.Context, _ runtime.Object) error {
				return nil
			},
			expectedError: storage.NewInvalidObjError(
				key,
				"Precondition failed: UID in precondition: incorrect-uid, UID in object meta: ",
			),
		},
	}

	for _, test := range tests {
		suite.Run(test.name, func() {
			err := suite.store.Create(context.Background(), key, sbom, &v1alpha1.SBOM{}, 0)
			suite.Require().NoError(err)

			out := &v1alpha1.SBOM{}
			err = suite.store.Delete(
				context.Background(),
				key,
				out,
				test.preconditions,
				test.validateDeletion,
				nil,
				storage.DeleteOptions{},
			)

			if test.expectedError != nil {
				suite.Require().Error(err)
				suite.Equal(test.expectedError.Error(), err.Error())
			} else {
				suite.Require().NoError(err)
				suite.Equal(sbom, out)

				err = suite.store.Get(context.Background(), key, storage.GetOptions{}, &v1alpha1.SBOM{})
				suite.True(storage.IsNotFound(err))
			}
		})
	}
}

func (suite *storeTestSuite) TestWatchEmptyResourceVersion() {
	key := keyPrefix + "/default/test"
	opts := storage.ListOptions{ResourceVersion: ""}

	w, err := suite.store.Watch(context.Background(), key, opts)
	suite.Require().NoError(err)

	suite.broadcaster.Shutdown()

	suite.Require().Empty(w.ResultChan())
}

func (suite *storeTestSuite) TestWatchResourceVersionZero() {
	key := keyPrefix + "/default/test"
	sbom := &v1alpha1.SBOM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
	}
	err := suite.store.Create(context.Background(), key, sbom, &v1alpha1.SBOM{}, 0)
	suite.Require().NoError(err)

	opts := storage.ListOptions{ResourceVersion: "0"}

	w, err := suite.store.Watch(context.Background(), key, opts)
	suite.Require().NoError(err)

	go suite.watcher.Start(suite.T().Context())

	validateDeletion := func(_ context.Context, _ runtime.Object) error {
		return nil
	}
	err = suite.store.Delete(
		context.Background(),
		key,
		&v1alpha1.SBOM{},
		&storage.Preconditions{},
		validateDeletion,
		nil,
		storage.DeleteOptions{},
	)
	suite.Require().NoError(err)

	events := mustReadEvents(suite.T(), w, 2)
	suite.Equal(watch.Added, events[0].Type)
	suite.Equal(sbom, events[0].Object)
	suite.Equal(watch.Deleted, events[1].Type)
	suite.Equal(sbom, events[1].Object)
}

func (suite *storeTestSuite) TestWatchSpecificResourceVersion() {
	key := keyPrefix + "/default"
	sbom := &v1alpha1.SBOM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
	}
	suite.Require().NoError(suite.store.Create(context.Background(), key+"/test", sbom, &v1alpha1.SBOM{}, 0))

	opts := storage.ListOptions{
		ResourceVersion: "1",
		Predicate:       matcher(labels.Everything(), fields.Everything()),
	}

	w, err := suite.store.Watch(context.Background(), key, opts)
	suite.Require().NoError(err)

	go suite.watcher.Start(suite.T().Context())

	tryUpdate := func(input runtime.Object, _ storage.ResponseMeta) (runtime.Object, *uint64, error) {
		return input, ptr.To(uint64(0)), nil
	}
	updatedSBOM := &v1alpha1.SBOM{}
	err = suite.store.GuaranteedUpdate(
		context.Background(),
		key+"/test",
		updatedSBOM,
		false,
		&storage.Preconditions{},
		tryUpdate,
		nil,
	)
	suite.Require().NoError(err)

	events := mustReadEvents(suite.T(), w, 2)
	suite.Equal(watch.Added, events[0].Type)
	suite.Equal(sbom, events[0].Object)
	suite.Equal(watch.Modified, events[1].Type)
	suite.Equal(updatedSBOM, events[1].Object)
}

func (suite *storeTestSuite) TestWatchWithLabelSelector() {
	key := keyPrefix + "/default"
	sbom1 := &v1alpha1.SBOM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test1",
			Namespace: "default",
			Labels: map[string]string{
				"sbomscanner.kubewarden.io/test": "true",
			},
		},
	}
	err := suite.store.Create(context.Background(), key+"/test1", sbom1, &v1alpha1.SBOM{}, 0)
	suite.Require().NoError(err)

	sbom2 := &v1alpha1.SBOM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test2",
			Namespace: "default",
			Labels:    map[string]string{},
		},
	}
	err = suite.store.Create(context.Background(), key+"/test2", sbom2, &v1alpha1.SBOM{}, 0)
	suite.Require().NoError(err)

	opts := storage.ListOptions{
		ResourceVersion: "1",
		Predicate: matcher(labels.SelectorFromSet(labels.Set{
			"sbomscanner.kubewarden.io/test": "true",
		}), fields.Everything()),
	}
	w, err := suite.store.Watch(context.Background(), key, opts)
	suite.Require().NoError(err)

	go suite.watcher.Start(suite.T().Context())

	events := mustReadEvents(suite.T(), w, 1)
	suite.Require().Len(events, 1)
	suite.Equal(watch.Added, events[0].Type)
	suite.Equal(sbom1, events[0].Object)
}

// mustReadEvents reads n events from the watch.Interface or fails the test if not enough events are received in time.
func mustReadEvents(t *testing.T, w watch.Interface, n int) []watch.Event {
	events := make([]watch.Event, 0, n)

	require.Eventually(t, func() bool {
		select {
		case evt := <-w.ResultChan():
			events = append(events, evt)
			return len(events) == n
		default:
			return false
		}
	}, time.Second, 5*time.Millisecond, "expected %d events", n)

	return events
}

func (suite *storeTestSuite) TestGetList() {
	key := keyPrefix + "/default"
	sbom1 := v1alpha1.SBOM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test1",
			Namespace: "default",
			Labels: map[string]string{
				"sbomscanner.kubewarden.io/env": "test",
			},
		},
	}
	err := suite.store.Create(context.Background(), key+"/test1", &sbom1, nil, 0)
	suite.Require().NoError(err)

	sbom2 := v1alpha1.SBOM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test2",
			Namespace: "default",
			Labels: map[string]string{
				"sbomscanner.kubewarden.io/env": "dev",
			},
		},
	}
	err = suite.store.Create(context.Background(), key+"/test2", &sbom2, nil, 0)
	suite.Require().NoError(err)

	sbom3 := v1alpha1.SBOM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test3",
			Namespace: "default",
			Labels: map[string]string{
				"sbomscanner.kubewarden.io/env":      "prod",
				"sbomscanner.kubewarden.io/critical": "true",
			},
		},
	}
	err = suite.store.Create(context.Background(), key+"/test3", &sbom3, nil, 0)
	suite.Require().NoError(err)

	tests := []struct {
		name          string
		listOptions   storage.ListOptions
		expectedItems []v1alpha1.SBOM
	}{
		{
			name:          "list all",
			expectedItems: []v1alpha1.SBOM{sbom1, sbom2, sbom3},
			listOptions: storage.ListOptions{
				Predicate: matcher(labels.Everything(), fields.Everything()),
			},
		},
		{
			name:          "list label selector (=)",
			expectedItems: []v1alpha1.SBOM{sbom1},
			listOptions: storage.ListOptions{
				Predicate: matcher(mustParseLabelSelector("sbomscanner.kubewarden.io/env=test"), fields.Everything()),
			},
		},
		{
			name:          "list label selector (!=)",
			expectedItems: []v1alpha1.SBOM{sbom2, sbom3},
			listOptions: storage.ListOptions{
				Predicate: matcher(mustParseLabelSelector("sbomscanner.kubewarden.io/env!=test"), fields.Everything()),
			},
		},
		{
			name:          "list label selector (in)",
			expectedItems: []v1alpha1.SBOM{sbom2, sbom3},
			listOptions: storage.ListOptions{
				Predicate: matcher(mustParseLabelSelector("sbomscanner.kubewarden.io/env in (dev,prod)"), fields.Everything()),
			},
		},
		{
			name:          "list label selector (notin)",
			expectedItems: []v1alpha1.SBOM{sbom3},
			listOptions: storage.ListOptions{
				Predicate: matcher(mustParseLabelSelector("sbomscanner.kubewarden.io/env notin (test,dev)"), fields.Everything()),
			},
		},
		{
			name:          "list label selector (exists)",
			expectedItems: []v1alpha1.SBOM{sbom3},
			listOptions: storage.ListOptions{
				Predicate: matcher(mustParseLabelSelector("sbomscanner.kubewarden.io/critical"), fields.Everything()),
			},
		},
		{
			name:          "list label selector (does not exist)",
			expectedItems: []v1alpha1.SBOM{sbom1, sbom2},
			listOptions: storage.ListOptions{
				Predicate: matcher(mustParseLabelSelector("!sbomscanner.kubewarden.io/critical"), fields.Everything()),
			},
		},
		{
			name:          "list field selector (=)",
			expectedItems: []v1alpha1.SBOM{sbom1},
			listOptions: storage.ListOptions{
				Predicate: matcher(labels.Everything(), mustParseFieldSelector("metadata.name=test1")),
			},
		},
		{
			name:          "list field selector (!=)",
			expectedItems: []v1alpha1.SBOM{sbom2, sbom3},
			listOptions: storage.ListOptions{
				Predicate: matcher(labels.Everything(), mustParseFieldSelector("metadata.name!=test1")),
			},
		},
	}

	for _, test := range tests {
		suite.Run(test.name, func() {
			sbomList := &v1alpha1.SBOMList{}
			err = suite.store.GetList(context.Background(), key, test.listOptions, sbomList)
			suite.Require().NoError(err)
			suite.ElementsMatch(test.expectedItems, sbomList.Items)
		})
	}
}

func mustParseLabelSelector(selector string) labels.Selector {
	labelSelector, err := labels.Parse(selector)
	if err != nil {
		panic("failed to parse label selector: " + err.Error())
	}

	return labelSelector
}

func mustParseFieldSelector(selector string) fields.Selector {
	fieldSelector, err := fields.ParseSelector(selector)
	if err != nil {
		panic("failed to parse field selector: " + err.Error())
	}
	return fieldSelector
}

func (suite *storeTestSuite) TestGuaranteedUpdate() {
	tests := []struct {
		name                string
		key                 string
		ignoreNotFound      bool
		preconditions       *storage.Preconditions
		tryUpdate           storage.UpdateFunc
		sbom                *v1alpha1.SBOM
		expectedUpdatedSBOM *v1alpha1.SBOM
		expectedError       error
	}{
		{
			name:          "happy path",
			key:           keyPrefix + "/default/test1",
			preconditions: &storage.Preconditions{},
			tryUpdate: func(input runtime.Object, _ storage.ResponseMeta) (runtime.Object, *uint64, error) {
				sbom, ok := input.(*v1alpha1.SBOM)
				if !ok {
					return nil, ptr.To(uint64(0)), errors.New("input is not of type *v1alpha1.SBOM")
				}

				sbom.SPDX.Raw = []byte(`{"foo": "bar"}`)

				return input, ptr.To(uint64(0)), nil
			},
			sbom: &v1alpha1.SBOM{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test1",
					Namespace: "default",
					UID:       "test1-uid",
				},
				SPDX: runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
			expectedUpdatedSBOM: &v1alpha1.SBOM{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test1",
					Namespace:       "default",
					UID:             "test1-uid",
					ResourceVersion: "2",
				},
				SPDX: runtime.RawExtension{
					Raw: []byte(`{"foo": "bar"}`),
				},
			},
		},
		{
			name: "preconditions failed",
			key:  keyPrefix + "/default/test2",
			preconditions: &storage.Preconditions{
				UID: ptr.To(types.UID("incorrect-uid")),
			},
			tryUpdate: func(_ runtime.Object, _ storage.ResponseMeta) (runtime.Object, *uint64, error) {
				suite.Fail("tryUpdate should not be called when preconditions fail")
				return nil, nil, nil
			},
			sbom: &v1alpha1.SBOM{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test2",
					Namespace: "default",
					UID:       "test2-uid",
				},
				SPDX: runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
			expectedError: storage.NewInvalidObjError(keyPrefix+"/default/test2",
				"Precondition failed: UID in precondition: incorrect-uid, UID in object meta: test2-uid"),
		},
		{
			name:          "tryUpdate failed with a non-conflict error",
			key:           keyPrefix + "/default/test3",
			preconditions: &storage.Preconditions{},
			tryUpdate: func(_ runtime.Object, _ storage.ResponseMeta) (runtime.Object, *uint64, error) {
				return nil, nil, storage.NewInternalError(errors.New("tryUpdate failed"))
			},
			sbom: &v1alpha1.SBOM{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test3",
					Namespace: "default",
					UID:       "test3-uid",
				},
				SPDX: runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
			expectedError: storage.NewInternalError(errors.New("tryUpdate failed")),
		},
		{
			name:          "not found",
			key:           keyPrefix + "/default/notfound",
			preconditions: &storage.Preconditions{},
			tryUpdate: func(_ runtime.Object, _ storage.ResponseMeta) (runtime.Object, *uint64, error) {
				suite.Fail("tryUpdate should not be called when object is not found")
				return nil, nil, nil
			},
			expectedError: storage.NewKeyNotFoundError(keyPrefix+"/default/notfound", 0),
		},
		{
			name:          "not found with ignore not found",
			key:           keyPrefix + "/default/notfound",
			preconditions: &storage.Preconditions{},
			tryUpdate: func(_ runtime.Object, _ storage.ResponseMeta) (runtime.Object, *uint64, error) {
				suite.Fail("tryUpdate should not be called when object is not found")
				return nil, nil, nil
			},
			ignoreNotFound:      true,
			expectedUpdatedSBOM: &v1alpha1.SBOM{},
		},
	}

	for _, test := range tests {
		suite.Run(test.name, func() {
			if test.sbom != nil {
				err := suite.store.Create(context.Background(), test.key, test.sbom, &v1alpha1.SBOM{}, 0)
				suite.Require().NoError(err)
			}

			destinationSBOM := &v1alpha1.SBOM{}
			err := suite.store.GuaranteedUpdate(
				context.Background(),
				test.key,
				destinationSBOM,
				test.ignoreNotFound,
				test.preconditions,
				test.tryUpdate,
				nil,
			)

			currentSBOM := &v1alpha1.SBOM{}
			if test.expectedError != nil {
				suite.Require().Error(err)
				suite.Require().Equal(test.expectedError.Error(), err.Error())

				if test.sbom != nil {
					// If there is an error, the original object should not be updated.
					err = suite.store.Get(context.Background(), test.key, storage.GetOptions{}, currentSBOM)
					suite.Require().NoError(err)
					suite.Equal(test.sbom, currentSBOM)
				}
			} else {
				suite.Require().NoError(err)
				suite.Require().Equal(test.expectedUpdatedSBOM, destinationSBOM)

				if !test.ignoreNotFound {
					// Verify the object was updated in the store.
					err = suite.store.Get(context.Background(), test.key, storage.GetOptions{}, currentSBOM)
					suite.Require().NoError(err)
					suite.Equal(test.expectedUpdatedSBOM, currentSBOM)
				}
			}
		})
	}
}

func (suite *storeTestSuite) TestCount() {
	err := suite.store.Create(context.Background(), keyPrefix+"/default/test1", &v1alpha1.SBOM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test1",
			Namespace: "default",
		},
	}, &v1alpha1.SBOM{}, 0)
	suite.Require().NoError(err)

	err = suite.store.Create(context.Background(), keyPrefix+"/default/test2", &v1alpha1.SBOM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test2",
			Namespace: "default",
		},
	}, &v1alpha1.SBOM{}, 0)
	suite.Require().NoError(err)

	err = suite.store.Create(context.Background(), keyPrefix+"/other/test4", &v1alpha1.SBOM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test4",
			Namespace: "other",
		},
	}, &v1alpha1.SBOM{}, 0)
	suite.Require().NoError(err)

	tests := []struct {
		name          string
		key           string
		expectedCount int64
	}{
		{
			name:          "count entries in default namespace",
			key:           keyPrefix + "/default",
			expectedCount: 2,
		},
		{
			name:          "count all entries",
			key:           keyPrefix,
			expectedCount: 3,
		},
	}

	for _, test := range tests {
		suite.Run(test.name, func() {
			var count int64
			count, err = suite.store.Count(test.key)
			suite.Require().NoError(err)
			suite.Require().Equal(test.expectedCount, count)
		})
	}
}
