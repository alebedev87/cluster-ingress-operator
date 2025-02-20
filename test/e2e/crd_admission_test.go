//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/openshift/api/features"
)

// Test_GatewayAPICRDAdmissionOutsideCluster verifies that the ingress operator's ValidatingAdmissionPolicy
// fails admission requests to modify Gateway API CRDs for a user which resides outside the cluster.
func Test_GatewayAPICRDAdmissionOutsideCluster(t *testing.T) {
	if gwapiEnabled, err := isFeatureGateEnabled(features.FeatureGateGatewayAPI); err != nil {
		t.Fatalf("Failed to get Gateway API feature gate: %v", err)
	} else if !gwapiEnabled {
		t.Skipf("Skipping Test_GatewayAPICRDAdmissionOutsideCluster when GatewayAPI featuregate is not enabled")
	}

	testCRDs := []*apiextensionsv1.CustomResourceDefinition{
		// managed CRDs
		makeGWAPICRD("gatewayclass", "GatewayClass"),
		makeGWAPICRD("gateway", "Gateway"),
		makeGWAPICRD("httproute", "HTTPRoute"),
		makeGWAPICRD("referencegrant", "ReferenceGrant"),
		// unmanaged CRDs
		//makeGWAPICRD("tcproute", "TCPRoute"),
	}
	expectedErrMsg := "modifications to Gateway API Custom Resource Definitions may only be made by the Ingress Operator"

	t.Log("Verifying GatewayAPI CRD creation is forbidden from outside the cluster")
	for i := range testCRDs {
		if err := kclient.Create(context.Background(), testCRDs[i]); err != nil {
			if !strings.Contains(err.Error(), expectedErrMsg) {
				t.Errorf("Unexpected error received while creating %q CRD: %v", testCRDs[i].Name, err)
			}
		} else {
			t.Errorf("Admission error is expected while creating %q CRD but not received", testCRDs[i].Name)
		}
	}

	t.Log("Verifying GatewayAPI CRD update is forbidden from outside the cluster")
	for i := range testCRDs {
		crdName := types.NamespacedName{Name: testCRDs[i].Name}
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := kclient.Get(context.Background(), crdName, crd); err != nil {
			t.Errorf("Failed to get %q CRD: %v", crdName.Name, err)
			continue
		}
		crd.Spec = testCRDs[i].Spec
		crd.Spec.Conversion = testCRDs[i].Spec.Conversion
		if err := kclient.Update(context.Background(), crd); err != nil {
			if !strings.Contains(err.Error(), expectedErrMsg) {
				t.Errorf("Unexpected error received while updating %q CRD: %v", testCRDs[i].Name, err)
			}
		} else {
			t.Errorf("Admission error is expected while updating %q CRD but not received", testCRDs[i].Name)
		}
	}

	t.Log("Verifying GatewayAPI CRD deletion is forbidden from outside the cluster")
	for i := range testCRDs {
		if err := kclient.Delete(context.Background(), testCRDs[i]); err != nil {
			if !strings.Contains(err.Error(), expectedErrMsg) {
				t.Errorf("Unexpected error received while deleting %q CRD: %v", testCRDs[i].Name, err)
			}
		} else {
			t.Errorf("Admission error is expected while deleting %q CRD but not received", testCRDs[i].Name)
		}
	}
}

func makeGWAPICRD(singular, kind string) *apiextensionsv1.CustomResourceDefinition {
	var (
		group  = "gateway.networking.k8s.io"
		plural = singular + "s"
		scope  = apiextensionsv1.NamespaceScoped
	)
	if singular == "gatewayclass" {
		plural = singular + "es"
		scope = apiextensionsv1.ClusterScoped
	}
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: plural + "." + group,
			Annotations: map[string]string{
				"api-approved.kubernetes.io": "https://github.com/kubernetes-sigs/gateway-api/pull/2466",
			},
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Singular: singular,
				Plural:   plural,
				Kind:     kind,
			},
			Scope: scope,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Storage: true,
					Served:  true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
						},
					},
				},
				{
					Name:    "v1beta1",
					Storage: false,
					Served:  true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
						},
					},
				},
			},
		},
	}
}
