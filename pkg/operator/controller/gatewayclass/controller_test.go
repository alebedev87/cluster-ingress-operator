package gatewayclass

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/cache/informertest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	maistrav2 "github.com/maistra/istio-operator/pkg/apis/maistra/v2"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"

	configv1 "github.com/openshift/api/config/v1"

	testutil "github.com/openshift/cluster-ingress-operator/pkg/operator/controller/test/util"
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
			client := &testutil.FakeClientRecorder{
				Client: fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tc.existingObjects...).Build(),
				T:      t,
			}
			cache := testutil.FakeCache{
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
			gatewayClassController = &testutil.FakeController{t, false, nil}
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
			if diff := cmp.Diff(tc.expectCreate, client.Added, cmpOpts...); diff != "" {
				t.Fatalf("found diff between expected and actual creates: %s", diff)
			}
		})
	}
}
