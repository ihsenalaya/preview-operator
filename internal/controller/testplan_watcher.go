package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
	"github.com/ihsenalaya/preview-operator/internal/policy"
)

// testPlanEventHandler returns a handler that maps a TestPlan change to the owning Preview.
// This causes the controller to re-reconcile the Preview when the agent fills a TestPlan.
func testPlanEventHandler() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, obj client.Object) []reconcile.Request {
			plan, ok := obj.(*platformv1alpha1.TestPlan)
			if !ok {
				return nil
			}
			previewName := plan.Labels[policy.LabelPreviewName]
			if previewName == "" {
				return nil
			}
			// Preview is cluster-scoped; its name is used as the namespace-less key.
			return []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: previewName}},
			}
		},
	)
}
