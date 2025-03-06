package gatewayclass

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache/informertest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	maistrav2 "github.com/maistra/istio-operator/pkg/apis/maistra/v2"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"

	configv1 "github.com/openshift/api/config/v1"

	logf "github.com/openshift/cluster-ingress-operator/pkg/log"
)

func Test_Reconcile(t *testing.T) {
	tests := []struct {
		name                        string
		gatewayAPIControllerEnabled bool
		existingObjects             []runtime.Object
		expectCreate                []client.Object
	}{
		{
			name:                        "gateway API controller disabled",
			gatewayAPIControllerEnabled: false,
		},
		{
			name:                        "gateway API controller enabled",
			gatewayAPIControllerEnabled: true,
			existingObjects: []runtime.Object{
				&gatewayapiv1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "openshift-default",
					},
				},
			},
			expectCreate: []client.Object{
				&operatorsv1alpha1.Subscription{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicemeshoperator",
						Namespace: "openshift-operators",
					},
				},
				&maistrav2.ServiceMeshControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "openshift-gateway",
						Namespace: "openshift-ingress",
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway.networking.k8s.io/v1",
								Kind:       "GatewayClass",
								Name:       "openshift-default",
							},
						},
					},
				},
			},
		},
	}

	scheme := runtime.NewScheme()
	configv1.Install(scheme)
	gatewayapiv1.AddToScheme(scheme)
	operatorsv1alpha1.AddToScheme(scheme)
	maistrav2.SchemeBuilder.AddToScheme(scheme)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeClientRecorder{
				Client: fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tc.existingObjects...).Build(),
				T:      t,
			}
			cache := fakeCache{
				Informers: &informertest.FakeInformers{
					Scheme: client.Scheme(),
				},
				Reader: client,
			}
			reconciler := &reconciler{
				client: client,
				cache:  cache,
				config: Config{
					GatewayAPIControllerEnabled: tc.gatewayAPIControllerEnabled,
					OperandNamespace:            "openshift-ingress",
				},
			}
			gatewayClassController = &fakeController{t, false, nil}
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: "openshift-default",
				},
			}

			// Test function.
			res, err := reconciler.Reconcile(context.Background(), req)

			assert.NoError(t, err)
			assert.Equal(t, reconcile.Result{}, res)
			cmpOpts := []cmp.Option{
				cmpopts.EquateEmpty(),
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "Annotations", "ResourceVersion"),
				cmpopts.IgnoreFields(metav1.TypeMeta{}, "Kind", "APIVersion"),
				cmpopts.IgnoreFields(operatorsv1alpha1.Subscription{}, "Spec"),
				cmpopts.IgnoreFields(maistrav2.ServiceMeshControlPlane{}, "Spec"),
			}
			if diff := cmp.Diff(tc.expectCreate, client.added, cmpOpts...); diff != "" {
				t.Fatalf("found diff between expected and actual creates: %s", diff)
			}
		})
	}
}

type fakeCache struct {
	cache.Informers
	client.Reader
}

type fakeClientRecorder struct {
	client.Client
	*testing.T

	added   []client.Object
	updated []client.Object
	deleted []client.Object
}

func (c *fakeClientRecorder) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return c.Client.Get(ctx, key, obj, opts...)
}

func (c *fakeClientRecorder) List(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
	return c.Client.List(ctx, obj, opts...)
}

func (c *fakeClientRecorder) Scheme() *runtime.Scheme {
	return c.Client.Scheme()
}

func (c *fakeClientRecorder) RESTMapper() meta.RESTMapper {
	return c.Client.RESTMapper()
}

func (c *fakeClientRecorder) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	c.added = append(c.added, obj)
	return c.Client.Create(ctx, obj, opts...)
}

func (c *fakeClientRecorder) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	c.deleted = append(c.deleted, obj)
	return c.Client.Delete(ctx, obj, opts...)
}

func (c *fakeClientRecorder) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	return c.Client.DeleteAllOf(ctx, obj, opts...)
}

func (c *fakeClientRecorder) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	c.updated = append(c.updated, obj)
	return c.Client.Update(ctx, obj, opts...)
}

func (c *fakeClientRecorder) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return c.Client.Patch(ctx, obj, patch, opts...)
}

func (c *fakeClientRecorder) Status() client.StatusWriter {
	return c.Client.Status()
}

type fakeController struct {
	*testing.T
	// started indicates whether Start() has been called.
	started bool
	// startNotificationChan is an optional channel by which a test can
	// receive a notification when Start() is called.
	startNotificationChan chan struct{}
}

func (_ *fakeController) Reconcile(context.Context, reconcile.Request) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

func (_ *fakeController) Watch(_ source.Source) error {
	return nil
}

func (c *fakeController) Start(_ context.Context) error {
	if c.started {
		c.T.Fatal("controller was started twice!")
	}
	c.started = true
	if c.startNotificationChan != nil {
		c.startNotificationChan <- struct{}{}
	}
	return nil
}

func (_ *fakeController) GetLogger() logr.Logger {
	return logf.Logger
}
