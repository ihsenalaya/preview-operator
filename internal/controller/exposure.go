package controller

import (
	"context"
	"fmt"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

const (
	virtualServiceGVR = "networking.istio.io/v1beta1"
	istioGateway      = "istio-system/preview-gateway"
)

// previewHost returns the public hostname for a preview environment.
func (r *PreviewReconciler) previewHost(c *platformv1alpha1.Preview) string {
	domain := r.PreviewDomain
	if domain == "" {
		domain = "preview.localtest.me"
	}
	return fmt.Sprintf("pr-%d.%s", c.Spec.PRNumber, domain)
}

// previewURL returns the full public URL for a preview environment.
func (r *PreviewReconciler) previewURL(c *platformv1alpha1.Preview) string {
	return "http://" + r.previewHost(c)
}

// reconcileExposure creates either an Istio VirtualService or an Nginx Ingress
// depending on whether Istio is available in the cluster.
func (r *PreviewReconciler) reconcileExposure(ctx context.Context, c *platformv1alpha1.Preview, nsName string) error {
	if r.IstioEnabled {
		return r.reconcileVirtualService(ctx, c, nsName)
	}
	return r.reconcileIngress(ctx, c, nsName)
}

// reconcileVirtualService creates/updates an Istio VirtualService for the preview.
func (r *PreviewReconciler) reconcileVirtualService(ctx context.Context, c *platformv1alpha1.Preview, nsName string) error {
	host := r.previewHost(c)

	var httpRoutes []interface{}
	if multiServiceEnabled(c) {
		for _, svc := range c.Spec.Services {
			if svc.PathPrefix == "" {
				continue
			}
			port := int64(svc.Port)
			if port == 0 {
				port = 8080
			}
			httpRoutes = append(httpRoutes, map[string]interface{}{
				"match": []interface{}{
					map[string]interface{}{
						"uri": map[string]interface{}{"prefix": svc.PathPrefix},
					},
				},
				"route": []interface{}{
					map[string]interface{}{
						"destination": map[string]interface{}{
							"host": serviceDeploymentName(svc.Name),
							"port": map[string]interface{}{"number": port},
						},
					},
				},
			})
		}
	} else {
		httpRoutes = append(httpRoutes, map[string]interface{}{
			"route": []interface{}{
				map[string]interface{}{
					"destination": map[string]interface{}{
						"host": "app",
						"port": map[string]interface{}{"number": int64(80)},
					},
				},
			},
		})
	}

	vs := &unstructured.Unstructured{}
	vs.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1beta1",
		Kind:    "VirtualService",
	})
	vs.SetName("app")
	vs.SetNamespace(nsName)

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, vs, func() error {
		if err := controllerutil.SetControllerReference(c, vs, r.Scheme); err != nil {
			return err
		}
		vs.SetLabels(map[string]string{labelManagedBy: "preview-operator"})
		return unstructured.SetNestedMap(vs.Object, map[string]interface{}{
			"hosts":    []interface{}{host},
			"gateways": []interface{}{istioGateway},
			"http":     httpRoutes,
		}, "spec")
	})
	return err
}

// istioAvailable checks whether the Istio VirtualService CRD is registered in the cluster.
func istioAvailable(mapper meta.RESTMapper) bool {
	_, err := mapper.RESTMapping(schema.GroupKind{
		Group: "networking.istio.io",
		Kind:  "VirtualService",
	})
	return err == nil
}

// reconcileIngress creates/updates an Nginx Ingress for the preview.
func (r *PreviewReconciler) reconcileIngress(ctx context.Context, c *platformv1alpha1.Preview, nsName string) error {
	if multiServiceEnabled(c) {
		return r.reconcileMultiServiceIngress(ctx, c, nsName)
	}
	host := r.previewHost(c)
	pathType := networkingv1.PathTypePrefix

	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: nsName},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ing, func() error {
		if err := controllerutil.SetControllerReference(c, ing, r.Scheme); err != nil {
			return err
		}
		ing.Labels = map[string]string{labelManagedBy: "preview-operator"}
		ing.Annotations = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
		}
		ing.Spec = networkingv1.IngressSpec{
			IngressClassName: strPtr("nginx"),
			Rules: []networkingv1.IngressRule{{
				Host: host,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "app",
									Port: networkingv1.ServiceBackendPort{Number: 80},
								},
							},
						}},
					},
				},
			}},
		}
		return nil
	})
	return err
}

// reconcileMultiServiceIngress creates a single Nginx Ingress with path-based routing.
func (r *PreviewReconciler) reconcileMultiServiceIngress(ctx context.Context, c *platformv1alpha1.Preview, nsName string) error {
	host := r.previewHost(c)
	pathType := networkingv1.PathTypePrefix

	type svcPath struct {
		prefix  string
		svcName string
		port    int32
	}
	var paths []svcPath
	for _, svc := range c.Spec.Services {
		if svc.PathPrefix == "" {
			continue
		}
		port := svc.Port
		if port == 0 {
			port = 8080
		}
		paths = append(paths, svcPath{prefix: svc.PathPrefix, svcName: serviceDeploymentName(svc.Name), port: port})
	}
	if len(paths) == 0 {
		return nil
	}

	var ingressPaths []networkingv1.HTTPIngressPath
	for _, p := range paths {
		ingressPaths = append(ingressPaths, networkingv1.HTTPIngressPath{
			Path:     p.prefix,
			PathType: &pathType,
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: p.svcName,
					Port: networkingv1.ServiceBackendPort{Number: p.port},
				},
			},
		})
	}

	ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: nsName}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ing, func() error {
		if err := controllerutil.SetControllerReference(c, ing, r.Scheme); err != nil {
			return err
		}
		ing.Labels = map[string]string{labelManagedBy: "preview-operator"}
		ing.Spec = networkingv1.IngressSpec{
			IngressClassName: strPtr("nginx"),
			Rules: []networkingv1.IngressRule{{
				Host: host,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{Paths: ingressPaths},
				},
			}},
		}
		return nil
	})
	return err
}

// deleteExposure removes the Ingress or VirtualService when a preview is deleted.
func (r *PreviewReconciler) deleteExposure(ctx context.Context, nsName string) {
	if r.IstioEnabled {
		vs := &unstructured.Unstructured{}
		vs.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "networking.istio.io",
			Version: "v1beta1",
			Kind:    "VirtualService",
		})
		vs.SetName("app")
		vs.SetNamespace(nsName)
		_ = r.Client.Delete(ctx, vs)
		return
	}
	ing := &networkingv1.Ingress{}
	ing.Name = "app"
	ing.Namespace = nsName
	if err := r.Client.Delete(ctx, ing); err != nil && !errors.IsNotFound(err) {
		return
	}
}
