//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/api/features"
)

// TestOSSMOperatorUpgradeViaIntermediateVersions verifies that the OSSM operator
// correctly upgrades through intermediate versions. It ensures the upgrade logic automatically approves
// each InstallPlan along the upgrade graph until the desired version is reached.
// This test upgrades from version 3.0.0 to 3.0.2, validating that it passes through
// the known intermediate version 3.0.1. Starting from the default (== hard coded) version
// is not feasible as future upgrade paths cannot be predetermined.
func TestOSSMOperatorUpgradeViaIntermediateVersions(t *testing.T) {
	gatewayAPIEnabled, err := isFeatureGateEnabled(features.FeatureGateGatewayAPI)
	if err != nil {
		t.Fatalf("Error checking feature gate enabled status: %v", err)
	}

	gatewayAPIControllerEnabled, err := isFeatureGateEnabled(features.FeatureGateGatewayAPIController)
	if err != nil {
		t.Fatalf("Error checking controller feature gate enabled status: %v", err)
	}

	if !gatewayAPIEnabled || !gatewayAPIControllerEnabled {
		t.Skip("Gateway API featuregates are not enabled, skipping TestOSSMOperatorUpgradeViaIntermediateVersions")
	}

	// Delete GatewayClass and uninstall OSSM operator at the end of the test.
	t.Cleanup(func() {
		gc := &gatewayapiv1.GatewayClass{ObjectMeta: metav1.ObjectMeta{Name: "openshift-default"}}
		if err := kclient.Delete(context.TODO(), gc); err != nil {
			if errors.IsNotFound(err) {
				return
			}
			t.Errorf("Failed to delete gatewayclass %q: %v", gc.Name, err)
		}
		// TODO: Uninstall OSSM after test is completed.
	})

	var (
		initialOSSMVersion = "servicemeshoperator3.v3.0.0"
		upgradeOSSMVersion = "servicemeshoperator3.v3.0.2"
	)

	// Set OSSM version to 3.0.0.
	t.Logf("Creating GatewayClass with OSSMversion %q...", initialOSSMVersion)
	gatewayClass := buildGatewayClass("openshift-default", "openshift.io/gateway-controller/v1")
	if gatewayClass.Annotations == nil {
		gatewayClass.Annotations = map[string]string{}
	}
	gatewayClass.Annotations["unsupported.do-not-use.openshift.io/ossm-version"] = initialOSSMVersion
	gatewayClass.Annotations["unsupported.do-not-use.openshift.io/istio-version"] = "v1.24.3"
	if err := kclient.Create(context.TODO(), gatewayClass); err != nil {
		t.Fatalf("Failed to create gatewayclass %s: %v", gatewayClass.Name, err)
	}
	t.Log("Checking for the Subscription...")
	if err := assertSubscription(t, openshiftOperatorsNamespace, expectedSubscriptionName); err != nil {
		t.Fatalf("Failed to find expected Subscription %s: %v", expectedSubscriptionName, err)
	}
	t.Log("Checking for the CatalogSource...")
	if err := assertCatalogSource(t, expectedCatalogSourceNamespace, expectedCatalogSourceName); err != nil {
		t.Fatalf("Failed to find expected CatalogSource %s: %v", expectedCatalogSourceName, err)
	}
	t.Log("Checking for the OSSM operator deployment...")
	if err := assertOSSMOperatorWithConfig(t, initialOSSMVersion, 2*time.Second, 60*time.Second); err != nil {
		t.Fatalf("Failed to find expected Istio operator: %v", err)
	}
	t.Log("Checking for the Istio CR...")
	if err := assertIstio(t); err != nil {
		t.Fatalf("Failed to find expected Istio: %v", err)
	}
	t.Log("Checking for the Istiod deployment...")
	if err := assertIstiodControlPlane(t); err != nil {
		t.Fatalf("Failed to find expected Istiod control plane: %v", err)
	}
	t.Log("Checking for the GatewayClass readiness...")
	if _, err := assertGatewayClassSuccessful(t, "openshift-default"); err != nil {
		t.Fatalf("Failed to find successful GatewayClass: %v", err)
	}

	// Set OSSM version to 3.0.2
	t.Logf("Updating GatewayClass to version %q...", upgradeOSSMVersion)
	if err := wait.PollUntilContextTimeout(context.Background(), 1*time.Second, 30*time.Second, false, func(context context.Context) (bool, error) {
		gc := &gatewayapiv1.GatewayClass{}
		if err := kclient.Get(context, types.NamespacedName{Name: gatewayClass.Name}, gc); err != nil {
			t.Logf("Failed to get GatewayClass %q: %v, retrying...", gatewayClass.Name, err)
			return false, nil
		}
		if gc.Annotations == nil {
			gc.Annotations = map[string]string{}
		}
		gc.Annotations["unsupported.do-not-use.openshift.io/ossm-version"] = upgradeOSSMVersion
		if err := kclient.Update(context, gc); err != nil {
			t.Logf("Failed to update GatewayClass %q: %v, retrying...", gc.Name, err)
			return false, nil
		}
		t.Logf("GatewayClass %q has been updated to version %q", gc.Name, upgradeOSSMVersion)
		return true, nil
	}); err != nil {
		t.Fatalf("Failed to update GatewayClass to next version: %v", err)
	}
	t.Log("Checking for the status...")
	expectedProgressing := configv1.ClusterOperatorStatusCondition{
		Type:   configv1.OperatorProgressing,
		Status: configv1.ConditionTrue,
		Reason: "OSSMOperatorUpgrading",
	}
	if err := waitForClusterOperatorConditions(t, kclient, expectedProgressing); err != nil {
		t.Fatalf("Operator should be Progressing=True while upgrading: %v", err)
	}
	t.Log("Checking for the OSSM operator deployment...")
	if err := assertOSSMOperatorWithConfig(t, upgradeOSSMVersion, 2*time.Second, 2*time.Minute); err != nil {
		t.Fatalf("failed to find expected Istio operator: %v", err)
	}
	t.Log("Re-checking for the status...")
	expectedProgressing = configv1.ClusterOperatorStatusCondition{
		Type:   configv1.OperatorProgressing,
		Status: configv1.ConditionFalse,
		Reason: "AsExpectedAndOSSMOperatorUpToDate",
	}
	if err := waitForClusterOperatorConditions(t, kclient, expectedProgressing); err != nil {
		t.Fatalf("Operator should be Progressing=False once upgrade reached desired version: %v", err)
	}
	t.Log("Checking for the GatewayClass readiness...")
	if _, err := assertGatewayClassSuccessful(t, "openshift-default"); err != nil {
		t.Fatalf("Failed to find successful GatewayClass: %v", err)
	}
}
