package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/company/cellenza-operator/api/v1alpha1"
)

const (
	cellenzaFinalizer  = "platform.company.io/finalizer"
	labelManagedBy     = "platform.company.io/managed-by"
	labelCellenzaName  = "platform.company.io/cellenza-name"
	postgresSecretName = "postgres-credentials"
	postgresHost       = "postgres"
	migrationJobName   = "postgres-migrate"
	seedJobName        = "postgres-seed"
	aiJobCPURequest    = "50m"
	aiJobMemoryRequest = "128Mi"
	aiJobCPULimit      = "500m"
	aiJobMemoryLimit   = "512Mi"
)

// CellenzaReconciler reconciles Cellenza objects
type CellenzaReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	APIReader        client.Reader
	GitHubAPIBaseURL string
	GitHubHTTPClient *http.Client
	AIAPIBaseURL     string
	AIHTTPClient     *http.Client
	KubeClient       kubernetes.Interface
}

// +kubebuilder:rbac:groups=platform.company.io,resources=cellenzas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.company.io,resources=cellenzas/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.company.io,resources=cellenzas/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=resourcequotas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get
// +kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

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
	if isTTLExpired(cellenza) {
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
		syncGitHubAfterStatus(ctx, r, cellenza, cellenza.Status.URL)
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
		syncGitHubAfterStatus(ctx, r, cellenza, "")
	}

	// 8. Reconcile all child resources
	nsName := r.namespaceName(cellenza)

	if err := r.reconcileNamespace(ctx, cellenza, nsName); err != nil {
		return r.setFailedStatus(ctx, cellenza, "NamespaceFailed", err)
	}

	if err := r.reconcileResourceQuota(ctx, cellenza, nsName); err != nil {
		return r.setFailedStatus(ctx, cellenza, "QuotaFailed", err)
	}

	if handled, result, err := r.handleResetRequested(ctx, cellenza, nsName); handled {
		return result, err
	}

	databaseReady, reason, err := r.reconcileDatabase(ctx, cellenza, nsName)
	if err != nil {
		return r.setFailedStatus(ctx, cellenza, reason, err)
	}
	if !databaseReady {
		cellenza.Status.Phase = platformv1alpha1.PhaseProvisioning
		cellenza.Status.NamespaceName = nsName
		cellenza.Status.ObservedGeneration = cellenza.Generation
		if databaseEnabled(cellenza) {
			r.setDatabaseStatus(cellenza, false)
		}
		if err := r.Status().Update(ctx, cellenza); err != nil {
			return ctrl.Result{}, err
		}
		syncGitHubAfterStatus(ctx, r, cellenza, "")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
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

	if handled, result, err := r.handleAppAvailability(ctx, cellenza, nsName); handled {
		return result, err
	}

	// 9. Mark as Running
	previewURL := fmt.Sprintf("http://pr-%d.preview.localtest.me:8080", cellenza.Spec.PRNumber)
	statusChanged := cellenza.Status.Phase != platformv1alpha1.PhaseRunning || cellenza.Status.URL != previewURL
	r.markRunningStatus(cellenza, nsName, previewURL)

	if statusChanged {
		if err := r.Status().Update(ctx, cellenza); err != nil {
			return ctrl.Result{}, err
		}
		syncGitHubAfterStatus(ctx, r, cellenza, previewURL)
	}

	if aiEnrichmentEnabled(cellenza) {
		if err := r.refreshCellenza(ctx, req.NamespacedName, cellenza); err != nil {
			return ctrl.Result{}, err
		}
		if result, err := r.reconcileAIEnrichment(ctx, cellenza, nsName); err != nil || result.RequeueAfter > 0 {
			return result, err
		}
	}

	// Requeue before expiry to handle TTL cleanup
	remaining := ttlRemaining(cellenza)
	if remaining > 0 {
		logger.Info("Requeuing before TTL expiry", "in", remaining)
	}
	return ctrl.Result{RequeueAfter: remaining}, nil
}

func (r *CellenzaReconciler) markRunningStatus(cellenza *platformv1alpha1.Cellenza, nsName, previewURL string) {
	if cellenza.Status.ReadyAt == nil {
		now := metav1.Now()
		cellenza.Status.ReadyAt = &now
	}
	cellenza.Status.Phase = platformv1alpha1.PhaseRunning
	cellenza.Status.URL = previewURL
	cellenza.Status.NamespaceName = nsName
	cellenza.Status.ObservedGeneration = cellenza.Generation

	if databaseEnabled(cellenza) {
		r.setDatabaseStatus(cellenza, true)
		cellenza.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionDatabaseReady,
			Status:             metav1.ConditionTrue,
			Reason:             "PostgreSQLProvisioned",
			Message:            fmt.Sprintf("PostgreSQL %s running — credentials in secret %s/%s", databaseVersion(cellenza), nsName, postgresSecretName),
			LastTransitionTime: metav1.Now(),
		})
	}
	cellenza.Status.Diagnostics = nil

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
}

func (r *CellenzaReconciler) refreshCellenza(ctx context.Context, key types.NamespacedName, cellenza *platformv1alpha1.Cellenza) error {
	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}

	latest := &platformv1alpha1.Cellenza{}
	if err := reader.Get(ctx, key, latest); err != nil {
		return err
	}

	*cellenza = *latest
	return nil
}

func isTTLExpired(cellenza *platformv1alpha1.Cellenza) bool {
	return cellenza.Status.ExpiresAt != nil && time.Now().After(cellenza.Status.ExpiresAt.Time)
}

func ttlRemaining(cellenza *platformv1alpha1.Cellenza) time.Duration {
	if cellenza.Status.ExpiresAt == nil {
		return 0
	}
	return time.Until(cellenza.Status.ExpiresAt.Time)
}

// handleResetRequested processes a reset-db request from the Copilot Extension.
// Returns (handled bool, result, error). If handled=true, the caller should return immediately.
func (r *CellenzaReconciler) handleResetRequested(ctx context.Context, cellenza *platformv1alpha1.Cellenza, nsName string) (bool, ctrl.Result, error) {
	if !databaseEnabled(cellenza) || !cellenza.Spec.Database.ResetRequested {
		return false, ctrl.Result{}, nil
	}
	if err := r.deleteDatabaseJobs(ctx, nsName); err != nil {
		return true, ctrl.Result{}, err
	}
	if cellenza.Status.Database != nil {
		cellenza.Status.Database.Migration = ""
		cellenza.Status.Database.Seed = ""
		cellenza.Status.Database.Ready = false
	}
	cellenza.Spec.Database.ResetRequested = false
	if err := r.Update(ctx, cellenza); err != nil {
		return true, ctrl.Result{}, err
	}
	return true, ctrl.Result{Requeue: true}, nil
}

func (r *CellenzaReconciler) reconcileDatabase(ctx context.Context, cellenza *platformv1alpha1.Cellenza, nsName string) (bool, string, error) {
	if !databaseEnabled(cellenza) {
		return true, "", nil
	}

	if err := r.reconcilePostgresSecret(ctx, cellenza, nsName); err != nil {
		return false, "DatabaseSecretFailed", err
	}
	if err := r.reconcilePostgresService(ctx, cellenza, nsName); err != nil {
		return false, "DatabaseServiceFailed", err
	}
	if err := r.reconcilePostgresDeployment(ctx, cellenza, nsName); err != nil {
		return false, "DatabaseDeploymentFailed", err
	}
	if ready, err := r.reconcileDatabaseTask(ctx, cellenza, nsName, componentMigration, migrationJobName, cellenza.Spec.Database.Migration); err != nil {
		return false, "DatabaseMigrationFailed", err
	} else if !ready {
		return false, "", nil
	}
	if ready, err := r.reconcileDatabaseTask(ctx, cellenza, nsName, componentSeed, seedJobName, cellenza.Spec.Database.Seed); err != nil {
		return false, "DatabaseSeedFailed", err
	} else if !ready {
		return false, "", nil
	}

	return true, "", nil
}

func databaseEnabled(cellenza *platformv1alpha1.Cellenza) bool {
	return cellenza.Spec.Database != nil && cellenza.Spec.Database.Enabled
}

func databaseVersion(cellenza *platformv1alpha1.Cellenza) string {
	if cellenza.Spec.Database == nil || cellenza.Spec.Database.Version == "" {
		return "15"
	}
	return cellenza.Spec.Database.Version
}

func databaseName(cellenza *platformv1alpha1.Cellenza) string {
	if cellenza.Spec.Database == nil || cellenza.Spec.Database.DatabaseName == "" {
		return "appdb"
	}
	return cellenza.Spec.Database.DatabaseName
}

func (r *CellenzaReconciler) setDatabaseStatus(cellenza *platformv1alpha1.Cellenza, ready bool) {
	if cellenza.Status.Database == nil {
		cellenza.Status.Database = &platformv1alpha1.DatabaseStatus{}
	}
	cellenza.Status.DatabaseSecretName = postgresSecretName
	cellenza.Status.Database.Ready = ready
	cellenza.Status.Database.Host = postgresHost
	cellenza.Status.Database.DatabaseName = databaseName(cellenza)
	cellenza.Status.Database.SecretName = postgresSecretName
}

// handleDeletion requests deletion for all known child resources and the
// preview namespace before removing the finalizer.
func (r *CellenzaReconciler) handleDeletion(ctx context.Context, cellenza *platformv1alpha1.Cellenza) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(cellenza, cellenzaFinalizer) {
		return ctrl.Result{}, nil
	}

	cellenza.Status.Phase = platformv1alpha1.PhaseTerminating
	_ = r.Status().Update(ctx, cellenza)
	syncGitHubAfterStatus(ctx, r, cellenza, cellenza.Status.URL)

	nsName := r.namespaceName(cellenza)
	if err := r.deleteKnownChildren(ctx, nsName); err != nil {
		return ctrl.Result{}, err
	}

	// Clean up AI prompt ConfigMap stored outside the preview namespace.
	promptCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      aiPromptConfigMapName(cellenza.Name),
			Namespace: defaultAISecretNamespace,
		},
	}
	if err := r.Delete(ctx, promptCM); err != nil && !errors.IsNotFound(err) {
		logger.Error(err, "Failed to delete AI prompt ConfigMap", "name", promptCM.Name)
	}

	ns := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
		if ns.DeletionTimestamp.IsZero() {
			logger.Info("Deleting Namespace", "namespace", nsName)
			if err := r.Delete(ctx, ns); err != nil && !errors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("failed to delete namespace %s: %w", nsName, err)
			}
		} else {
			logger.Info("Namespace deletion already requested", "namespace", nsName)
		}
	} else if !errors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to get namespace %s: %w", nsName, err)
	}

	controllerutil.RemoveFinalizer(cellenza, cellenzaFinalizer)
	if err := r.Update(ctx, cellenza); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Cleanup complete", "name", cellenza.Name)
	return ctrl.Result{}, nil
}

func (r *CellenzaReconciler) deleteKnownChildren(ctx context.Context, nsName string) error {
	children := []client.Object{
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: nsName}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: nsName}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: migrationJobName, Namespace: nsName}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: seedJobName, Namespace: nsName}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: nsName}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: nsName}},
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: nsName}},
		&corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: "cellenza-quota", Namespace: nsName}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: postgresSecretName, Namespace: nsName}},
	}

	for _, child := range children {
		if err := r.Delete(ctx, child); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete %T %s/%s: %w", child, nsName, child.GetName(), err)
		}
	}
	return nil
}

func (r *CellenzaReconciler) ensureControllerReference(ctx context.Context, owner *platformv1alpha1.Cellenza, obj client.Object) error {
	before := obj.DeepCopyObject().(client.Object)
	if err := controllerutil.SetControllerReference(owner, obj, r.Scheme); err != nil {
		return err
	}
	return r.Patch(ctx, obj, client.MergeFrom(before))
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
		if err := controllerutil.SetControllerReference(c, ns, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, ns)
	}
	if err == nil {
		return r.ensureControllerReference(ctx, c, ns)
	}
	return err
}

// reconcileResourceQuota enforces CPU/Memory limits per tier, extended when PostgreSQL is enabled
func (r *CellenzaReconciler) reconcileResourceQuota(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) error {
	cpuLimitStr, memLimitStr, cpuReqStr, memReqStr := c.ResourceLimits()

	cpuLimit := resource.MustParse(cpuLimitStr)
	memLimit := resource.MustParse(memLimitStr)
	cpuReq := resource.MustParse(cpuReqStr)
	memReq := resource.MustParse(memReqStr)

	if c.Spec.Database != nil && c.Spec.Database.Enabled {
		// Reserve headroom for the PostgreSQL pod (500m CPU / 512Mi RAM limit)
		cpuLimit.Add(resource.MustParse("500m"))
		memLimit.Add(resource.MustParse("512Mi"))
		cpuReq.Add(resource.MustParse("100m"))
		memReq.Add(resource.MustParse("128Mi"))
	}

	if aiEnrichmentEnabled(c) {
		// AI enrichment jobs run sequentially, so the quota only needs headroom for one job at a time.
		cpuLimit.Add(resource.MustParse(aiJobCPULimit))
		memLimit.Add(resource.MustParse(aiJobMemoryLimit))
		cpuReq.Add(resource.MustParse(aiJobCPURequest))
		memReq.Add(resource.MustParse(aiJobMemoryRequest))
	}

	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cellenza-quota",
			Namespace: nsName,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, quota, func() error {
		if err := controllerutil.SetControllerReference(c, quota, r.Scheme); err != nil {
			return err
		}
		quota.Spec = corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceLimitsCPU:      cpuLimit,
				corev1.ResourceLimitsMemory:   memLimit,
				corev1.ResourceRequestsCPU:    cpuReq,
				corev1.ResourceRequestsMemory: memReq,
				corev1.ResourcePods:           resource.MustParse("10"),
			},
		}
		return nil
	})
	return err
}

// reconcileDeployment creates/updates the app deployment, wiring in PostgreSQL when enabled
func (r *CellenzaReconciler) reconcileDeployment(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) error {
	cpuLimit, memLimit, cpuReq, memReq := c.ResourceLimits()

	dbEnabled := c.Spec.Database != nil && c.Spec.Database.Enabled

	env := []corev1.EnvVar{
		{Name: "PREVIEW_BRANCH", Value: c.Spec.Branch},
		{Name: "PREVIEW_PR", Value: fmt.Sprintf("%d", c.Spec.PRNumber)},
		{Name: "ENVIRONMENT", Value: "preview"},
	}

	var initContainers []corev1.Container

	if dbEnabled {
		// Init container blocks app startup until PostgreSQL accepts connections.
		// Uses busybox (5MB) instead of the full postgres image just for a TCP check.
		initContainers = []corev1.Container{
			{
				Name:    "wait-for-postgres",
				Image:   "busybox:1.36",
				Command: []string{"sh", "-c", "until nc -z postgres 5432; do echo 'waiting for postgres...'; sleep 2; done"},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("10m"),
						corev1.ResourceMemory: resource.MustParse("32Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("50m"),
						corev1.ResourceMemory: resource.MustParse("64Mi"),
					},
				},
			},
		}

		fromSecret := func(key string) corev1.EnvVar {
			return corev1.EnvVar{
				Name: key,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName},
						Key:                  key,
					},
				},
			}
		}
		env = append(env,
			fromSecret("POSTGRES_USER"),
			fromSecret("POSTGRES_PASSWORD"),
			fromSecret("POSTGRES_DB"),
			fromSecret("DATABASE_URL"),
		)
	}
	env = append(env, telemetryEnv(c, nsName)...)
	podAnnotations := telemetryPodAnnotations(c)

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app",
			Namespace: nsName,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		replicas := c.Spec.Replicas
		if err := controllerutil.SetControllerReference(c, deploy, r.Scheme); err != nil {
			return err
		}
		deploy.Labels = map[string]string{
			labelManagedBy:    "cellenza-operator",
			labelCellenzaName: c.Name,
		}
		progressDeadlineSeconds := int32(60)
		deploy.Spec = appsv1.DeploymentSpec{
			Replicas:                &replicas,
			ProgressDeadlineSeconds: &progressDeadlineSeconds,
			Strategy:                appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
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
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					InitContainers: initContainers,
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: c.Spec.Image,
							Ports: []corev1.ContainerPort{
								{ContainerPort: 80, Protocol: corev1.ProtocolTCP},
							},
							Env: env,
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

func (r *CellenzaReconciler) handleAppAvailability(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) (bool, ctrl.Result, error) {
	appReady, appReason, err := r.appDeploymentReady(ctx, nsName)
	if err != nil {
		result, err := r.setFailedStatus(ctx, c, appReason, err)
		return true, result, err
	}
	if appReady {
		return false, ctrl.Result{}, nil
	}

	c.Status.Phase = platformv1alpha1.PhaseProvisioning
	c.Status.NamespaceName = nsName
	c.Status.ObservedGeneration = c.Generation
	c.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             appReason,
		Message:            "Waiting for app deployment to become available",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, c); err != nil {
		return true, ctrl.Result{}, err
	}
	syncGitHubAfterStatus(ctx, r, c, "")
	return true, ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r *CellenzaReconciler) appDeploymentReady(ctx context.Context, nsName string) (bool, string, error) {
	deploy := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: "app", Namespace: nsName}, deploy); err != nil {
		return false, "DeploymentUnavailable", err
	}

	for _, condition := range deploy.Status.Conditions {
		if condition.Type == appsv1.DeploymentProgressing && condition.Status == corev1.ConditionFalse {
			message := condition.Message
			if message == "" {
				message = "app deployment stopped progressing"
			}
			return false, "DeploymentFailed", fmt.Errorf("%s", message)
		}
	}

	if deploy.Status.AvailableReplicas >= *deploy.Spec.Replicas && deploy.Status.ObservedGeneration >= deploy.Generation {
		return true, "DeploymentAvailable", nil
	}

	return false, "DeploymentNotReady", nil
}

func telemetryPodAnnotations(c *platformv1alpha1.Cellenza) map[string]string {
	if c.Spec.Telemetry == nil || !c.Spec.Telemetry.Enabled || c.Spec.Telemetry.AutoInstrumentation == nil {
		return nil
	}

	auto := c.Spec.Telemetry.AutoInstrumentation
	if auto.Language == "" {
		return nil
	}

	value := auto.InstrumentationRef
	if value == "" {
		value = "true"
	}

	annotations := map[string]string{
		fmt.Sprintf("instrumentation.opentelemetry.io/inject-%s", auto.Language): value,
	}

	if auto.Language == platformv1alpha1.TelemetryLanguagePython && auto.PythonPlatform != "" {
		annotations["instrumentation.opentelemetry.io/otel-python-platform"] = auto.PythonPlatform
	}
	if auto.Language == platformv1alpha1.TelemetryLanguageGo && auto.GoTargetExecutable != "" {
		annotations["instrumentation.opentelemetry.io/otel-go-auto-target-exe"] = auto.GoTargetExecutable
	}

	return annotations
}

func telemetryEnv(c *platformv1alpha1.Cellenza, nsName string) []corev1.EnvVar {
	if c.Spec.Telemetry == nil || !c.Spec.Telemetry.Enabled {
		return nil
	}

	serviceName := c.Spec.Telemetry.ServiceName
	if serviceName == "" {
		serviceName = fmt.Sprintf("cellenza-%s", c.Name)
	}

	resourceAttributes := fmt.Sprintf(
		"cellenza.name=%s,cellenza.pr_number=%d,cellenza.branch=%s,k8s.namespace.name=%s",
		sanitizeOTelResourceValue(c.Name),
		c.Spec.PRNumber,
		sanitizeOTelResourceValue(c.Spec.Branch),
		sanitizeOTelResourceValue(nsName),
	)

	return []corev1.EnvVar{
		{Name: "OTEL_SERVICE_NAME", Value: serviceName},
		{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: resourceAttributes},
	}
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
		if err := controllerutil.SetControllerReference(c, svc, r.Scheme); err != nil {
			return err
		}
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
		if err := controllerutil.SetControllerReference(c, ing, r.Scheme); err != nil {
			return err
		}
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

// reconcilePostgresSecret creates a Secret with unique credentials the first time only.
// If the Secret already exists its data is never overwritten, preserving credentials across reconcile loops.
func (r *CellenzaReconciler) reconcilePostgresSecret(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) error {
	existing := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: postgresSecretName, Namespace: nsName}, existing)
	if err == nil {
		return r.ensureControllerReference(ctx, c, existing) // credentials already exist — do not regenerate
	}
	if !errors.IsNotFound(err) {
		return err
	}

	dbName := "appdb"
	if c.Spec.Database.DatabaseName != "" {
		dbName = c.Spec.Database.DatabaseName
	}
	username := fmt.Sprintf("preview_%d", c.Spec.PRNumber)
	password, err := generateSecureToken(32)
	if err != nil {
		return fmt.Errorf("generating database password: %w", err)
	}
	dbURL := fmt.Sprintf("postgresql://%s:%s@postgres:5432/%s?sslmode=disable", username, password, dbName)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      postgresSecretName,
			Namespace: nsName,
			Labels: map[string]string{
				labelManagedBy:    "cellenza-operator",
				labelCellenzaName: c.Name,
			},
			Annotations: map[string]string{
				"platform.company.io/managed-by":      "cellenza-operator",
				"platform.company.io/credentials-for": fmt.Sprintf("pr-%d", c.Spec.PRNumber),
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"POSTGRES_USER":     username,
			"POSTGRES_PASSWORD": password,
			"POSTGRES_DB":       dbName,
			"DATABASE_URL":      dbURL,
		},
	}
	if err := controllerutil.SetControllerReference(c, secret, r.Scheme); err != nil {
		return err
	}
	return r.Create(ctx, secret)
}

// reconcilePostgresDeployment creates/updates the PostgreSQL Deployment
func (r *CellenzaReconciler) reconcilePostgresDeployment(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) error {
	version := "15"
	if c.Spec.Database.Version != "" {
		version = c.Spec.Database.Version
	}
	image := fmt.Sprintf("postgres:%s-alpine", version)
	one := int32(1)

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "postgres",
			Namespace: nsName,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		if err := controllerutil.SetControllerReference(c, deploy, r.Scheme); err != nil {
			return err
		}
		deploy.Labels = map[string]string{
			labelManagedBy:                "cellenza-operator",
			labelCellenzaName:             c.Name,
			"app.kubernetes.io/component": "database",
		}
		deploy.Spec = appsv1.DeploymentSpec{
			Replicas: &one,
			// Recreate avoids two postgres pods briefly running against the same data directory
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "postgres", labelCellenzaName: c.Name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":                         "postgres",
						labelCellenzaName:             c.Name,
						labelManagedBy:                "cellenza-operator",
						"app.kubernetes.io/component": "database",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "postgres",
							Image: image,
							Ports: []corev1.ContainerPort{
								{ContainerPort: 5432, Protocol: corev1.ProtocolTCP},
							},
							Env: []corev1.EnvVar{
								secretKeyRef("POSTGRES_USER", "POSTGRES_USER"),
								secretKeyRef("POSTGRES_PASSWORD", "POSTGRES_PASSWORD"),
								secretKeyRef("POSTGRES_DB", "POSTGRES_DB"),
								// Store data in a sub-directory to avoid "lost+found" issues on emptyDir
								{Name: "PGDATA", Value: "/var/lib/postgresql/data/pgdata"},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"sh", "-c", `pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"`},
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
								TimeoutSeconds:      5,
								FailureThreshold:    6,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"sh", "-c", `pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"`},
									},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       30,
								TimeoutSeconds:      5,
								FailureThreshold:    3,
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

// reconcilePostgresService creates/updates the headless ClusterIP service for PostgreSQL
func (r *CellenzaReconciler) reconcilePostgresService(ctx context.Context, c *platformv1alpha1.Cellenza, nsName string) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "postgres",
			Namespace: nsName,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		if err := controllerutil.SetControllerReference(c, svc, r.Scheme); err != nil {
			return err
		}
		svc.Labels = map[string]string{
			labelManagedBy:                "cellenza-operator",
			"app.kubernetes.io/component": "database",
		}
		svc.Spec = corev1.ServiceSpec{
			Selector: map[string]string{"app": "postgres", labelCellenzaName: c.Name},
			Ports: []corev1.ServicePort{
				{
					Name:       "postgres",
					Port:       5432,
					TargetPort: intstr.FromInt(5432),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		}
		return nil
	})
	return err
}

func (r *CellenzaReconciler) reconcileDatabaseTask(ctx context.Context, c *platformv1alpha1.Cellenza, nsName, taskName, jobName string, task *platformv1alpha1.DatabaseTaskSpec) (bool, error) {
	conditionType := platformv1alpha1.ConditionMigrationReady
	if taskName == componentSeed {
		conditionType = platformv1alpha1.ConditionSeedReady
	}

	if task == nil || !task.Enabled {
		r.setDatabaseTaskStatus(c, taskName, phaseSkipped)
		c.SetCondition(metav1.Condition{
			Type:               conditionType,
			Status:             metav1.ConditionTrue,
			Reason:             "TaskDisabled",
			Message:            fmt.Sprintf("Database %s task is disabled", taskName),
			LastTransitionTime: metav1.Now(),
		})
		return true, nil
	}
	if r.databaseTaskStatus(c, taskName) == phaseSucceeded {
		c.SetCondition(metav1.Condition{
			Type:               conditionType,
			Status:             metav1.ConditionTrue,
			Reason:             "JobAlreadySucceeded",
			Message:            fmt.Sprintf("Database %s job already completed", taskName),
			LastTransitionTime: metav1.Now(),
		})
		return true, nil
	}

	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: nsName}, job)
	if errors.IsNotFound(err) {
		newJob := r.databaseTaskJob(c, nsName, taskName, jobName, task)
		if err := controllerutil.SetControllerReference(c, newJob, r.Scheme); err != nil {
			return false, err
		}
		if err := r.Create(ctx, newJob); err != nil {
			return false, err
		}
		r.markDatabaseTaskRunning(c, taskName, conditionType, jobName)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := r.ensureControllerReference(ctx, c, job); err != nil {
		return false, err
	}

	if job.Status.Succeeded > 0 {
		r.setDatabaseTaskStatus(c, taskName, phaseSucceeded)
		c.SetCondition(metav1.Condition{
			Type:               conditionType,
			Status:             metav1.ConditionTrue,
			Reason:             "JobSucceeded",
			Message:            fmt.Sprintf("Database %s job %s/%s completed", taskName, nsName, jobName),
			LastTransitionTime: metav1.Now(),
		})
		return true, nil
	}

	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			r.setDatabaseTaskStatus(c, taskName, phaseFailed)
			return false, fmt.Errorf("database %s job %s/%s failed: %s", taskName, nsName, jobName, cond.Message)
		}
	}

	r.markDatabaseTaskRunning(c, taskName, conditionType, jobName)
	return false, nil
}

func (r *CellenzaReconciler) databaseTaskJob(c *platformv1alpha1.Cellenza, nsName, taskName, jobName string, task *platformv1alpha1.DatabaseTaskSpec) *batchv1.Job {
	image := task.Image
	if image == "" {
		image = c.Spec.Image
	}
	backoffLimit := int32(1)
	ttlSecondsAfterFinished := int32(600)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: nsName,
			Labels: map[string]string{
				labelManagedBy:                "cellenza-operator",
				labelCellenzaName:             c.Name,
				"app.kubernetes.io/component": "database-task",
				"platform.company.io/task":    taskName,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttlSecondsAfterFinished,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labelManagedBy:             "cellenza-operator",
						labelCellenzaName:          c.Name,
						"platform.company.io/task": taskName,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					InitContainers: []corev1.Container{
						{
							Name:    "wait-for-postgres",
							Image:   "busybox:1.36",
							Command: []string{"sh", "-c", "until nc -z postgres 5432; do echo 'waiting for postgres...'; sleep 2; done"},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("32Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:    taskName,
							Image:   image,
							Command: task.Command,
							Args:    task.Args,
							Env: []corev1.EnvVar{
								secretKeyRef("POSTGRES_USER", "POSTGRES_USER"),
								secretKeyRef("POSTGRES_PASSWORD", "POSTGRES_PASSWORD"),
								secretKeyRef("POSTGRES_DB", "POSTGRES_DB"),
								secretKeyRef("DATABASE_URL", "DATABASE_URL"),
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
					},
				},
			},
		},
	}
}

func (r *CellenzaReconciler) markDatabaseTaskRunning(c *platformv1alpha1.Cellenza, taskName, conditionType, jobName string) {
	r.setDatabaseTaskStatus(c, taskName, phaseRunning)
	c.SetCondition(metav1.Condition{
		Type:               conditionType,
		Status:             metav1.ConditionFalse,
		Reason:             "JobRunning",
		Message:            fmt.Sprintf("Database %s job %s is running", taskName, jobName),
		LastTransitionTime: metav1.Now(),
	})
}

func (r *CellenzaReconciler) setDatabaseTaskStatus(c *platformv1alpha1.Cellenza, taskName, status string) {
	r.setDatabaseStatus(c, false)
	if taskName == componentSeed {
		c.Status.Database.Seed = status
		return
	}
	c.Status.Database.Migration = status
}

func (r *CellenzaReconciler) databaseTaskStatus(c *platformv1alpha1.Cellenza, taskName string) string {
	if c.Status.Database == nil {
		return ""
	}
	if taskName == componentSeed {
		return c.Status.Database.Seed
	}
	return c.Status.Database.Migration
}

// secretKeyRef builds an EnvVar that reads its value from a Secret key
func secretKeyRef(envName, key string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: envName,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName},
				Key:                  key,
			},
		},
	}
}

// generateSecureToken returns a hex-encoded cryptographically random string of n bytes (2n chars)
func generateSecureToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (r *CellenzaReconciler) setFailedStatus(ctx context.Context, c *platformv1alpha1.Cellenza, reason string, err error) (ctrl.Result, error) {
	c.Status.Phase = platformv1alpha1.PhaseFailed
	c.Status.Diagnostics = r.collectDiagnostics(ctx, c, reason, err)
	c.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            err.Error(),
		LastTransitionTime: metav1.Now(),
	})
	_ = r.Status().Update(ctx, c)
	syncGitHubAfterStatus(ctx, r, c, c.Status.URL)
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

func sanitizeOTelResourceValue(s string) string {
	return strings.NewReplacer(",", "_", "\n", "_", "\r", "_").Replace(s)
}

func strPtr(s string) *string { return &s }

func (r *CellenzaReconciler) deleteDatabaseJobs(ctx context.Context, nsName string) error {
	jobs := []client.Object{
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: migrationJobName, Namespace: nsName}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: seedJobName, Namespace: nsName}},
	}
	for _, job := range jobs {
		if err := r.Delete(ctx, job); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// SetupWithManager registers the controller
func (r *CellenzaReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Cellenza{}).
		Owns(&corev1.Namespace{}).
		Owns(&corev1.ResourceQuota{}).
		Owns(&corev1.Secret{}).
		Owns(&appsv1.Deployment{}).
		Owns(&batchv1.Job{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}
