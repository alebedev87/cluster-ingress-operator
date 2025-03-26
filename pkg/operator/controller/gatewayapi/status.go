package gatewayapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	configv1 "github.com/openshift/api/config/v1"

	operatorcontroller "github.com/openshift/cluster-ingress-operator/pkg/operator/controller"
)

// This file contains reconciler methods and functions which help to update the status of ingress cluster operator.

// ingressOperatorStatusExtension ...
type ingressOperatorStatusExtension struct {
	UnmanagedGatewayAPICRDNames string `json:"unmanagedGatewayAPICRDNames"`
}

// setUnmanagedGatewayAPICRDNamesStatus sets the status of the ingress cluster operator
// with the names of the unmanaged Gateway CRDs.
func (r *reconciler) setUnmanagedGatewayAPICRDNamesStatus(ctx context.Context, crdNames []string) error {
	desiredExtension := &ingressOperatorStatusExtension{}
	// If no CRDs were found, keep desired extension empty to reset the field to null.
	if len(crdNames) > 0 {
		desiredExtension.UnmanagedGatewayAPICRDNames = strings.Join(crdNames, ",")
	}
	return r.setClusterOperatorExtension(ctx, desiredExtension)
}

// setClusterOperatorExtension attempts to ensure that the ingress cluster operator's status
// is updated with the given extension. Returns an error if failed to update the status.
func (r *reconciler) setClusterOperatorExtension(ctx context.Context, desiredExtension *ingressOperatorStatusExtension) error {
	have, current, err := r.currentClusterOperator(ctx, operatorcontroller.IngressClusterOperatorName())
	if err != nil {
		return err
	}
	if !have {
		return fmt.Errorf("cluster operator %q not found", operatorcontroller.IngressClusterOperatorName().Name)
	}
	if _, err := r.updateClusterOperatorExtension(ctx, current, desiredExtension); err != nil {
		return err
	}
	return nil
}

// currentClusterOperator returns a boolean indicating whether a cluster operator
// with the given name exists, as well as its definition if it does exist and an error value.
func (r *reconciler) currentClusterOperator(ctx context.Context, name types.NamespacedName) (bool, *configv1.ClusterOperator, error) {
	co := &configv1.ClusterOperator{}
	if err := r.client.Get(ctx, name, co); err != nil {
		if errors.IsNotFound(err) {
			return false, nil, nil
		}
		return false, nil, fmt.Errorf("failed to get cluster operator %q: %w", name.Name, err)
	}
	return true, co, nil
}

// updateClusterOperatorExtension updates a cluster operator's status extension.
// Returns a boolean indicating whether the cluster operator was updated, and an error value.
func (r *reconciler) updateClusterOperatorExtension(ctx context.Context, current *configv1.ClusterOperator, desiredExtension *ingressOperatorStatusExtension) (bool, error) {
	currentExtension := &ingressOperatorStatusExtension{}
	if len(current.Status.Extension.Raw) > 0 /*to avoid "unexpected end of JSON input" error*/ {
		if err := json.Unmarshal(current.Status.Extension.Raw, currentExtension); err != nil {
			return false, fmt.Errorf("failed to unmarshal status extension of cluster operator %q: %w", current.Name, err)
		}
	}
	if equality.Semantic.DeepEqual(*currentExtension, *desiredExtension) {
		return false, nil
	}

	updated := current.DeepCopy()
	rawDesiredExtension, err := json.Marshal(desiredExtension)
	if err != nil {
		return false, fmt.Errorf("failed to marshal desired status extension of cluster operator %q: %w", updated.Name, err)
	}
	updated.Status.Extension.Raw = rawDesiredExtension
	if err := r.client.Status().Update(ctx, updated); err != nil {
		return false, fmt.Errorf("failed to update cluster operator %q: %w", updated.Name, err)
	}
	log.Info("updated cluster operator", "name", updated.Name, "status", updated.Status)
	return true, nil
}
