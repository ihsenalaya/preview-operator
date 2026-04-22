package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/company/cellenza-operator/api/v1alpha1"
)

const (
	cellenzaFinalizer = "platform.company.io/finalizer"
	labelManagedBy    = "platform.company.io/managed-by"
	labelCellenzaName = "platform.company.io/cellenza-name"
)

// CellenzaReconciler reconciles Cellenza objects
type CellenzaReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=platform.company.io,resources=cellenzas,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=platform.company.io,resources=cellenzas/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=platform.company.io,resources=cellenzas/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=resourcequotas,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

func (r *CellenzaReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling Cellenza", "name", req.Name)

	// 1. Fetch the Cellenza object
	cellenza := &platformv1alpha1.Cellenza{}
	if err := r.Get(ctx, req.NamespacedName, cellenza); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// 2. Handle deletion (finalizer logic)
	if !cellenza.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, cellenza)
	}

	// 3. Add finalizer if not present
	if !controllerutil.ContainsFinalizer(cellenza, cellenzaFinalizer) {
		controllerutil.AddFinalizer(cellenza, cellenzaFinalizer)
		if err := r.Update(ctx, cellenza); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 4. Check TTL expiration
	if cellenza.Status.ExpiresAt != nil && time.Now().After(cellenza.Status.ExpiresAt.Time) {
		logger.Info("Cellenza TTL expired, deleting", "name", cellenza.Name)
		cellenza.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionExpired,
			Status:             metav1.ConditionTrue,
			Reason:             "TTLExpired",
			Message:            "Environment TTL has expired, triggering deletion",
			LastTransitionTime: metav1.Now(),
		})
		_ = r.Status().Update(ctx, cellenza)
		return ctrl.Result{}, r.Delete(ctx, cellenza)
	}

	// 5. Check approval gate
	if !cellenza.IsApproved() {
		logger.Info("Cellenza pending approval", "name", cellenza.Name)
		cellenza.Status.Phase = platformv1alpha1.PhasePending
		cellenza.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionApproved,
			Status:             metav1.ConditionFalse,
			Reason:             "PendingApproval",
			Message:            "Waiting for spec.approvedBy to be set by a platform team member",
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Update(ctx, cellenza); err != nil {
			return ctrl.Result{}, err
		}
		// Requeue every 30s to check if approval was granted
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// 6. Set ExpiresAt on first provision
	if cellenza.Status.ExpiresAt == nil {
		ttl, err := time.ParseDuration(cellenza.Spec.TTL)
		if err != nil {
			ttl = 48 * time.Hour
		}
		expiry := metav1.NewTime(time.Now().Add(ttl))
		cellenza.Status.ExpiresAt = &expiry
		cellenza.Status.ObservedGeneration = cellenza.Generation
	}

	// 7. Set phase to Provisioning
	if cellenza.Status.Phase == "" || cellenza.Status.Phase == platformv1alpha1.PhasePending {
		cellenza.Status.Phase = platformv1alpha1.PhaseProvisioning
		if err := r.Status().Update(ctx, cellenza); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 8. Reconcile all child resources
	nsName := r.namespaceName(cellenza)

	if err := r.reconcileNamespace(ctx, cellenza, nsName); err != nil {
		return r.setFailedStatus(ctx, cellenza, "NamespaceFailed", err)
	}

	if err := r.reconcileResourceQuota(ctx, cellenza, nsName); err != nil {
		return r.setFailedStatus(ctx, cellenza, "QuotaFailed", err)
	}

	if err := r.reconcileDeployment(ctx, cellenza, nsName); err != nil {
		return r.setFailedStatus(ctx, cellenza, "DeploymentFailed", err)
	}

	if err := r.reconcileService(ctx, cellenza, nsName); err != nil {
		return r.setFailedStatus(ctx, cellenza, "ServiceFailed", err)
	}

	if err := r.reconcileIngress(ctx, cellenza, nsName); err != nil {
		return r.setFailedStatus(ctx, cellenza, "IngressFailed", err)
	}

	// 9. Mark as Running
	previewURL := fmt.Sprintf("http://pr-%d.preview.localtest.me:8080", cellenza.Spec.PRNumber)
	cellenza.Status.Phase = platformv1alpha1.PhaseRunning
	cellenza.Status.URL = previewURL
	cellenza.Status.NamespaceName = nsName
	cellenza.Status.ObservedGeneration = cellenza.Generation

	cellenza.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "AllResourcesReady",
		Message:            fmt.Sprintf("Preview environment running at %s", previewURL),
		LastTransitionTime: metav1.Now(),
	})
	cellenza.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionApproved,
		Status:             metav1.ConditionTrue,
		Reason:             "Approved",
		Message:            fmt.Sprintf("Approved by: %s", cellenza.Spec.ApprovedBy),
		LastTransitionTime: metav1.Now(),
	})

	if err := r.Status().Update(ctx, cellenza); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue before expiry to handle TTL cleanup
	if cellenza.Status.ExpiresAt != nil {
		remaining := time.Until(cellenza.Status.ExpiresAt.Time)
		if remaining > 0 {
			logger.Info("Requeuing before TTL expiry", "in", remaining)
			return ctrl.Result{RequeueAfter: remaining}, nil
		}
	}

	return ctrl.Result{}, nil
}

// handleDeletion cleans up all child resources
func (r *CellenzaReconciler) handleDeletion(ctx context.Context, cellenza *platformv1alpha1.Cellenza) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(cellenza, cellenzaFinalizer) {
		return ctrl.Result{}, nil
	}

	logger.Info("Running cleanup finalizer", "name", cellenza.Name)
	cellenza.Status.Phase = platformv1alpha1.PhaseTerminating
	_ = r.Status().Update(ctx, cellenza)

	nsName := r.namespaceName(cellenza)
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
		logger.Info("Deleting namespace", "namespace", nsName)
		if err := r.Delete(ctx, ns); err != nil && !errors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("failed to delete namespace %s: %w", nsName, err)
		}
	}

	// Remove finalizer to allow garbage collection
	controllerutil.RemoveFinalizer(cellenza, cellenzaFinalizer)
	if err := r.Update(ctx, cellenza); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Cleanup complete", "name", cellenza.Name)
	return ctrl.Result{}, nil
}

// reconcileNamespace ensures the dedicated namespace exists
func (r *CellenzaReconciler) reconcileNamespace(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) error {
	ns := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: nsName}, ns)
	if errors.IsNotFound(err) {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: nsName,
				Labels: map[string]string{
					labelManagedBy:    "cellenza-operator",
					labelCellenzaName: c.Name,
					"branch":          sanitizeLabel(c.Spec.Branch),
					"pr-number":       fmt.Sprintf("%d", c.Spec.PRNumber),
				},
				Annotations: map[string]string{
					"platform.company.io/ttl":        c.Spec.TTL,
					"platform.company.io/created-by": "cellenza-operator",
				},
			},
		}
		return r.Create(ctx, ns)
	}
	return err
}

// reconcileResourceQuota enforces CPU/Memory limits per tier
func (r *CellenzaReconciler) reconcileResourceQuota(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) error {
	cpuLimit, memLimit, cpuReq, memReq := c.ResourceLimits()

	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cellenza-quota",
			Namespace: nsName,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, quota, func() error {
		quota.Spec = corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceLimitsCPU:      resource.MustParse(cpuLimit),
				corev1.ResourceLimitsMemory:   resource.MustParse(memLimit),
				corev1.ResourceRequestsCPU:    resource.MustParse(cpuReq),
				corev1.ResourceRequestsMemory: resource.MustParse(memReq),
				corev1.ResourcePods:           resource.MustParse("10"),
			},
		}
		return nil
	})
	return err
}

// reconcileDeployment creates/updates the app deployment
func (r *CellenzaReconciler) reconcileDeployment(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) error {
	_, memLimit, cpuReq, memReq := c.ResourceLimits()
	cpuLimit := "500m"
	if c.Spec.ResourceTier == platformv1alpha1.TierLarge {
		cpuLimit = "1000m"
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app",
			Namespace: nsName,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		replicas := c.Spec.Replicas
		deploy.Labels = map[string]string{
			labelManagedBy:    "cellenza-operator",
			labelCellenzaName: c.Name,
		}
		deploy.Spec = appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "cellenza-preview"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":          "cellenza-preview",
						"branch":       sanitizeLabel(c.Spec.Branch),
						"pr":           fmt.Sprintf("%d", c.Spec.PRNumber),
						labelManagedBy: "cellenza-operator",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: c.Spec.Image,
							Ports: []corev1.ContainerPort{
								{ContainerPort: 80, Protocol: corev1.ProtocolTCP},
							},
							Env: []corev1.EnvVar{
								{Name: "PREVIEW_BRANCH", Value: c.Spec.Branch},
								{Name: "PREVIEW_PR", Value: fmt.Sprintf("%d", c.Spec.PRNumber)},
								{Name: "ENVIRONMENT", Value: "preview"},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse(cpuReq),
									corev1.ResourceMemory: resource.MustParse(memReq),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse(cpuLimit),
									corev1.ResourceMemory: resource.MustParse(memLimit),
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/",
										Port: intstr.FromInt(80),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
						},
					},
				},
			},
		}
		return nil
	})
	return err
}

// reconcileService creates/updates the ClusterIP service
func (r *CellenzaReconciler) reconcileService(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app",
			Namespace: nsName,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Labels = map[string]string{labelManagedBy: "cellenza-operator"}
		svc.Spec = corev1.ServiceSpec{
			Selector: map[string]string{"app": "cellenza-preview"},
			Ports: []corev1.ServicePort{
				{
					Port:       80,
					TargetPort: intstr.FromInt(80),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		}
		return nil
	})
	return err
}

// reconcileIngress creates/updates the ingress
func (r *CellenzaReconciler) reconcileIngress(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) error {
	pathType := networkingv1.PathTypePrefix
	host := fmt.Sprintf("pr-%d.preview.localtest.me", c.Spec.PRNumber)

	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app",
			Namespace: nsName,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ing, func() error {
		ing.Labels = map[string]string{labelManagedBy: "cellenza-operator"}
		ing.Annotations = map[string]string{
			"nginx.ingress.kubernetes.io/rewrite-target": "/",
		}
		ing.Spec = networkingv1.IngressSpec{
			IngressClassName: strPtr("nginx"),
			Rules: []networkingv1.IngressRule{
				{
					Host: host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "app",
											Port: networkingv1.ServiceBackendPort{Number: 80},
										},
									},
								},
							},
						},
					},
				},
			},
		}
		return nil
	})
	return err
}

func (r *CellenzaReconciler) setFailedStatus(ctx context.Context, c *platformv1alpha1.Cellenza, reason string, err error) (ctrl.Result, error) {
	c.Status.Phase = platformv1alpha1.PhaseFailed
	c.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            err.Error(),
		LastTransitionTime: metav1.Now(),
	})
	_ = r.Status().Update(ctx, c)
	return ctrl.Result{RequeueAfter: 30 * time.Second}, err
}

func (r *CellenzaReconciler) namespaceName(c *platformv1alpha1.Cellenza) string {
	return fmt.Sprintf("preview-pr-%d", c.Spec.PRNumber)
}

func sanitizeLabel(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
			result = append(result, c)
		} else {
			result = append(result, '-')
		}
	}
	if len(result) > 63 {
		return string(result[:63])
	}
	return string(result)
}

func strPtr(s string) *string { return &s }

// SetupWithManager registers the controller
func (r *CellenzaReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Cellenza{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}
