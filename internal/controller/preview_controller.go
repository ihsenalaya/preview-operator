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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

const (
	previewFinalizer   = "platform.company.io/finalizer"
	labelManagedBy     = "platform.company.io/managed-by"
	labelPreviewName   = "platform.company.io/preview-name"
	postgresSecretName = "postgres-credentials"
	postgresHost       = "postgres"
	migrationJobName   = "postgres-migrate"
	seedJobName        = "postgres-seed"
	aiJobCPURequest    = "50m"
	aiJobMemoryRequest = "128Mi"
	aiJobCPULimit      = "500m"
	aiJobMemoryLimit   = "512Mi"
)

// PreviewReconciler reconciles Preview objects
type PreviewReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	APIReader         client.Reader
	OperatorNamespace string
	GitHubAPIBaseURL  string
	GitHubHTTPClient  *http.Client
	AIAPIBaseURL      string
	AIHTTPClient      *http.Client
	KubeClient        kubernetes.Interface
	PreviewDomain     string // base domain, e.g. "preview.ihsenalaya.xyz"
	IstioEnabled      bool   // auto-detected at startup
}

// +kubebuilder:rbac:groups=platform.company.io,resources=previews,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.company.io,resources=previews/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.company.io,resources=previews/finalizers,verbs=update
// +kubebuilder:rbac:groups=platform.company.io,resources=testplans,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.company.io,resources=testplans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.company.io,resources=testruns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.company.io,resources=testruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.company.io,resources=reconcileevents,verbs=get;list;watch;create
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
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagent.dev,resources=agents,verbs=get;create

func (r *PreviewReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling Preview", "name", req.Name)

	// 1. Fetch the Preview object
	preview := &platformv1alpha1.Preview{}
	if err := r.Get(ctx, req.NamespacedName, preview); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// 2. Handle deletion (finalizer logic)
	if !preview.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, preview)
	}

	// 3. Add finalizer if not present
	if !controllerutil.ContainsFinalizer(preview, previewFinalizer) {
		controllerutil.AddFinalizer(preview, previewFinalizer)
		if err := r.Update(ctx, preview); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if err := r.resetDerivedStateForNewGeneration(ctx, preview, r.namespaceName(preview)); err != nil {
		return ctrl.Result{}, err
	}

	// 4. Check TTL expiration
	if isTTLExpired(preview) {
		logger.Info("Preview TTL expired, deleting", "name", preview.Name)
		preview.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionExpired,
			Status:             metav1.ConditionTrue,
			Reason:             "TTLExpired",
			Message:            "Environment TTL has expired, triggering deletion",
			LastTransitionTime: metav1.Now(),
		})
		_ = r.Status().Update(ctx, preview)
		return ctrl.Result{}, r.Delete(ctx, preview)
	}

	// 5. Check approval gate
	if !preview.IsApproved() {
		logger.Info("Preview pending approval", "name", preview.Name)
		preview.Status.Phase = platformv1alpha1.PhasePending
		preview.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionApproved,
			Status:             metav1.ConditionFalse,
			Reason:             "PendingApproval",
			Message:            "Waiting for spec.approvedBy to be set by a platform team member",
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Update(ctx, preview); err != nil {
			return ctrl.Result{}, err
		}
		syncGitHubAfterStatus(ctx, r, preview, preview.Status.URL)
		// Requeue every 30s to check if approval was granted
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// 6. Set ExpiresAt on first provision
	if preview.Status.ExpiresAt == nil {
		ttl, err := time.ParseDuration(preview.Spec.TTL)
		if err != nil {
			ttl = 48 * time.Hour
		}
		expiry := metav1.NewTime(time.Now().Add(ttl))
		preview.Status.ExpiresAt = &expiry
		preview.Status.ObservedGeneration = preview.Generation
	}

	// 7. Set phase to Provisioning
	if preview.Status.Phase == "" || preview.Status.Phase == platformv1alpha1.PhasePending {
		preview.Status.Phase = platformv1alpha1.PhaseProvisioning
		if err := r.Status().Update(ctx, preview); err != nil {
			return ctrl.Result{}, err
		}
		syncGitHubAfterStatus(ctx, r, preview, "")
	}

	// 8 & 9. Provision all child resources and run post-deploy jobs
	nsName := r.namespaceName(preview)
	return r.reconcileProvisioning(ctx, req.NamespacedName, preview, nsName)
}

// reconcileProvisioning handles child resource reconciliation once the Preview is approved and ready.
// Extracted from Reconcile to keep cyclomatic complexity manageable.
func (r *PreviewReconciler) reconcileProvisioning(ctx context.Context, key types.NamespacedName, preview *platformv1alpha1.Preview, nsName string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if err := r.reconcileNamespace(ctx, preview, nsName); err != nil {
		return r.setFailedStatus(ctx, preview, "NamespaceFailed", err)
	}

	if err := r.reconcileResourceQuota(ctx, preview, nsName); err != nil {
		return r.setFailedStatus(ctx, preview, "QuotaFailed", err)
	}

	if err := r.reconcileNetworkPolicy(ctx, preview, nsName); err != nil {
		return r.setFailedStatus(ctx, preview, "NetworkPolicyFailed", err)
	}

	if err := r.migrateDiffPatch(ctx, preview, nsName); err != nil {
		return r.setFailedStatus(ctx, preview, "DiffPatchMigrationFailed", err)
	}

	if handled, result, err := r.handleResetRequested(ctx, preview, nsName); handled {
		return result, err
	}
	if handled, result, err := r.handleAIRerunRequested(ctx, preview, nsName); handled {
		return result, err
	}

	if result, err := r.reconcileDatabaseWait(ctx, preview, nsName); result.RequeueAfter > 0 || err != nil {
		return result, err
	}

	if handled, result, err := r.reconcileCheckpoints(ctx, preview, nsName); handled {
		return result, err
	}

	if err := r.reconcileDeployment(ctx, preview, nsName); err != nil {
		if errors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return r.setFailedStatus(ctx, preview, "DeploymentFailed", err)
	}

	if err := r.reconcileService(ctx, preview, nsName); err != nil {
		if errors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return r.setFailedStatus(ctx, preview, "ServiceFailed", err)
	}

	if err := r.reconcileExposure(ctx, preview, nsName); err != nil {
		if errors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return r.setFailedStatus(ctx, preview, "ExposureFailed", err)
	}

	if handled, result, err := r.handleAppAvailability(ctx, preview, nsName); handled {
		return result, err
	}

	previewURL := r.previewURL(preview)
	if preview.Status.Phase != platformv1alpha1.PhaseRunning || preview.Status.URL != previewURL {
		r.markRunningStatus(preview, nsName, previewURL)
		if err := r.Status().Update(ctx, preview); err != nil {
			return ctrl.Result{}, err
		}
		syncGitHubAfterStatus(ctx, r, preview, previewURL)
	}
	r.triggerKagentDiffAnalysis(ctx, preview)

	if aiEnrichmentEnabled(preview) {
		if err := r.refreshPreview(ctx, key, preview); err != nil {
			return ctrl.Result{}, err
		}
		if result, err := r.reconcileAIEnrichment(ctx, preview, nsName); err != nil || result.RequeueAfter > 0 {
			return result, err
		}
	}

	// Test suite runs AFTER AI enrichment so the database already contains seed data.
	if testSuiteEnabled(preview) && !aiRerunOnly(preview) {
		if aiEnrichmentEnabled(preview) {
			aiPhase := ""
			if preview.Status.AIEnrichment != nil {
				aiPhase = preview.Status.AIEnrichment.Phase
			}
			if aiPhase != phaseSucceeded && aiPhase != phaseFailed {
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}
		}
		if err := r.refreshPreview(ctx, key, preview); err != nil {
			return ctrl.Result{}, err
		}

		// Resolve the effective TestPlan (agent-driven, manual, or full-suite fallback).
		stratResult, plan, stratErr := r.reconcileTestStrategy(ctx, preview, nsName)
		if stratErr != nil {
			return r.setFailedStatus(ctx, preview, "TestStrategyFailed", stratErr)
		}
		if stratResult.Requeue || stratResult.RequeueAfter > 0 {
			// Awaiting agent — status already updated inside reconcileTestStrategy.
			if err := r.Status().Update(ctx, preview); err != nil {
				return ctrl.Result{}, err
			}
			return stratResult, nil
		}
		// Persist plan resolution to status before running tests.
		if err := r.Status().Update(ctx, preview); err != nil {
			return ctrl.Result{}, err
		}

		// Only start the run once — when tests haven't begun yet.
		testsNotStarted := preview.Status.Tests == nil || preview.Status.Tests.Phase == ""
		if plan != nil && testsNotStarted {
			if err := r.createTestRun(ctx, preview, nsName, plan); err != nil {
				logger.Error(err, "failed to create TestRun")
			}
			r.emitReconcileEvent(ctx, preview, nsName, platformv1alpha1.ReconcileEventTestStarted,
				"", "", "test suite starting", plan.Spec.CorrelationID)
		}

		// plan may be nil when fallbackOnAgentTimeout=Skip — that means no tests run.
		if plan != nil {
			prevPhase := ""
			if preview.Status.Tests != nil {
				prevPhase = preview.Status.Tests.Phase
			}
			if result, err := r.reconcileTestSuite(ctx, preview, nsName, plan); err != nil || result.RequeueAfter > 0 {
				return result, err
			}
			// Emit TestFinished only on the transition to a terminal phase.
			newPhase := ""
			if preview.Status.Tests != nil {
				newPhase = preview.Status.Tests.Phase
			}
			if (newPhase == phaseSucceeded || newPhase == phaseFailed) && prevPhase != newPhase {
				r.emitReconcileEvent(ctx, preview, nsName, platformv1alpha1.ReconcileEventTestFinished,
					"", newPhase, "test suite finished", plan.Spec.CorrelationID)
			}
		}

		// Retry kagent analysis until it succeeds (phase=Succeeded).
		r.triggerKagentAnalysis(ctx, preview)
		if kagentEnabled(preview) && preview.Status.Kagent != nil &&
			preview.Status.Kagent.Phase == phaseFailed {
			return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
		}
	}

	remaining := ttlRemaining(preview)
	if remaining > 0 {
		logger.Info("Requeuing before TTL expiry", "in", remaining)
	}
	return ctrl.Result{RequeueAfter: remaining}, nil
}

// reconcileDatabaseWait wraps reconcileDatabase and handles the not-ready wait path.
func (r *PreviewReconciler) reconcileDatabaseWait(ctx context.Context, preview *platformv1alpha1.Preview, nsName string) (ctrl.Result, error) {
	ready, reason, err := r.reconcileDatabase(ctx, preview, nsName)
	if err != nil {
		return r.setFailedStatus(ctx, preview, reason, err)
	}
	if ready {
		return ctrl.Result{}, nil
	}
	preview.Status.Phase = platformv1alpha1.PhaseProvisioning
	preview.Status.NamespaceName = nsName
	preview.Status.ObservedGeneration = preview.Generation
	if databaseEnabled(preview) {
		r.setDatabaseStatus(preview, false)
	}
	if err := r.Status().Update(ctx, preview); err != nil {
		return ctrl.Result{}, err
	}
	syncGitHubAfterStatus(ctx, r, preview, "")
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r *PreviewReconciler) markRunningStatus(preview *platformv1alpha1.Preview, nsName, previewURL string) {
	if preview.Status.ReadyAt == nil {
		now := metav1.Now()
		preview.Status.ReadyAt = &now
	}
	preview.Status.Phase = platformv1alpha1.PhaseRunning
	preview.Status.URL = previewURL
	preview.Status.NamespaceName = nsName
	preview.Status.ObservedGeneration = preview.Generation

	if databaseEnabled(preview) {
		r.setDatabaseStatus(preview, true)
		preview.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionDatabaseReady,
			Status:             metav1.ConditionTrue,
			Reason:             "PostgreSQLProvisioned",
			Message:            fmt.Sprintf("PostgreSQL %s running — credentials in secret %s/%s", databaseVersion(preview), nsName, postgresSecretName),
			LastTransitionTime: metav1.Now(),
		})
	}
	preview.Status.Diagnostics = nil

	preview.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "AllResourcesReady",
		Message:            fmt.Sprintf("Preview environment running at %s", previewURL),
		LastTransitionTime: metav1.Now(),
	})
	preview.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionApproved,
		Status:             metav1.ConditionTrue,
		Reason:             "Approved",
		Message:            fmt.Sprintf("Approved by: %s", preview.Spec.ApprovedBy),
		LastTransitionTime: metav1.Now(),
	})
}

func (r *PreviewReconciler) refreshPreview(ctx context.Context, key types.NamespacedName, preview *platformv1alpha1.Preview) error {
	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}

	latest := &platformv1alpha1.Preview{}
	if err := reader.Get(ctx, key, latest); err != nil {
		return err
	}

	*preview = *latest
	return nil
}

func (r *PreviewReconciler) resetDerivedStateForNewGeneration(ctx context.Context, c *platformv1alpha1.Preview, nsName string) error {
	if c.Status.ObservedGeneration == 0 || c.Status.ObservedGeneration == c.Generation {
		return nil
	}
	if hasTransientDatabaseRequest(c) || aiRerunRequested(c) {
		c.Status.ObservedGeneration = c.Generation
		return r.Status().Update(ctx, c)
	}

	for _, name := range []string{
		smokeJobName,
		microcksJobName,
		regressionJobName,
		e2eJobName,
		aiSeedJobName,
		aiTestJobName,
		aiSchemaJobName,
	} {
		job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: nsName}}
		if err := r.Delete(ctx, job); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	for _, name := range []string{aiEnrichmentConfigMap} {
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: nsName}}
		if err := r.Delete(ctx, cm); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	c.Status.ObservedGeneration = c.Generation
	c.Status.Tests = nil
	c.Status.AIEnrichment = nil
	// Preserve CommentID so the next kagent run updates the existing PR comment
	// rather than creating a duplicate. Clear phase/triggeredAt so it reruns.
	var kagentCommentID int64
	if c.Status.Kagent != nil {
		kagentCommentID = c.Status.Kagent.CommentID
	}
	c.Status.Kagent = nil
	if kagentCommentID != 0 {
		c.Status.Kagent = &platformv1alpha1.KagentStatus{CommentID: kagentCommentID}
	}
	if c.Status.GitHub != nil {
		c.Status.GitHub.TestsCommentID = 0
		c.Status.GitHub.CommentID = 0
		c.Status.GitHub.DeploymentState = ""
		c.Status.GitHub.LastNotifiedPhase = ""
		c.Status.GitHub.LastEnvironmentURL = ""
		c.Status.GitHub.LastError = ""
	}

	filtered := c.Status.Conditions[:0]
	for _, cond := range c.Status.Conditions {
		if cond.Type == platformv1alpha1.ConditionTestSuiteReady || cond.Type == platformv1alpha1.ConditionAIEnrichmentReady {
			continue
		}
		filtered = append(filtered, cond)
	}
	c.Status.Conditions = filtered

	return r.Status().Update(ctx, c)
}

func hasTransientDatabaseRequest(c *platformv1alpha1.Preview) bool {
	if c.Spec.Database == nil || !c.Spec.Database.Enabled {
		return false
	}
	return c.Spec.Database.ResetRequested ||
		c.Spec.Database.CheckpointSave != "" ||
		c.Spec.Database.CheckpointRestore != ""
}

func isTTLExpired(preview *platformv1alpha1.Preview) bool {
	return preview.Status.ExpiresAt != nil && time.Now().After(preview.Status.ExpiresAt.Time)
}

func ttlRemaining(preview *platformv1alpha1.Preview) time.Duration {
	if preview.Status.ExpiresAt == nil {
		return 0
	}
	return time.Until(preview.Status.ExpiresAt.Time)
}

// handleResetRequested processes a reset-db request from the Copilot Extension.
// Returns (handled bool, result, error). If handled=true, the caller should return immediately.
func (r *PreviewReconciler) handleResetRequested(ctx context.Context, preview *platformv1alpha1.Preview, nsName string) (bool, ctrl.Result, error) {
	if !databaseEnabled(preview) || !preview.Spec.Database.ResetRequested {
		return false, ctrl.Result{}, nil
	}
	if err := r.deleteDatabaseJobs(ctx, nsName); err != nil {
		return true, ctrl.Result{}, err
	}

	statusBase := preview.DeepCopy()
	if preview.Status.Database != nil {
		preview.Status.Database.Migration = ""
		preview.Status.Database.Seed = ""
		preview.Status.Database.Ready = false
	}
	if err := r.Status().Patch(ctx, preview, client.MergeFrom(statusBase)); err != nil {
		return true, ctrl.Result{}, err
	}

	specBase := preview.DeepCopy()
	preview.Spec.Database.ResetRequested = false
	if err := r.Patch(ctx, preview, client.MergeFrom(specBase)); err != nil {
		return true, ctrl.Result{}, err
	}
	return true, ctrl.Result{Requeue: true}, nil
}

func (r *PreviewReconciler) handleAIRerunRequested(ctx context.Context, preview *platformv1alpha1.Preview, nsName string) (bool, ctrl.Result, error) {
	if !aiRerunRequested(preview) {
		return false, ctrl.Result{}, nil
	}

	aiStatus := ensureAIEnrichmentStatus(preview)
	if aiStatus.RerunOnly {
		return false, ctrl.Result{}, nil
	}

	if err := r.deleteAIResources(ctx, nsName); err != nil {
		return true, ctrl.Result{}, err
	}
	if err := r.deleteTestSuiteJobs(ctx, nsName); err != nil {
		return true, ctrl.Result{}, err
	}
	if databaseEnabled(preview) {
		if err := r.deleteDatabaseJobs(ctx, nsName); err != nil {
			return true, ctrl.Result{}, err
		}
	}

	statusBase := preview.DeepCopy()
	if preview.Status.Database != nil {
		preview.Status.Database.Migration = ""
		preview.Status.Database.Seed = ""
		preview.Status.Database.Ready = false
	}
	preview.Status.AIEnrichment = &platformv1alpha1.AIEnrichmentStatus{
		RerunOnly: true,
	}
	clearStatusCondition(preview, platformv1alpha1.ConditionAIEnrichmentReady)
	if err := r.Status().Patch(ctx, preview, client.MergeFrom(statusBase)); err != nil {
		return true, ctrl.Result{}, err
	}

	return true, ctrl.Result{Requeue: true}, nil
}

func (r *PreviewReconciler) reconcileDatabase(ctx context.Context, preview *platformv1alpha1.Preview, nsName string) (bool, string, error) {
	if !databaseEnabled(preview) {
		return true, "", nil
	}

	if err := r.reconcilePostgresSecret(ctx, preview, nsName); err != nil {
		return false, "DatabaseSecretFailed", err
	}
	if err := r.reconcilePostgresService(ctx, preview, nsName); err != nil {
		return false, "DatabaseServiceFailed", err
	}
	if err := r.reconcilePostgresDeployment(ctx, preview, nsName); err != nil {
		return false, "DatabaseDeploymentFailed", err
	}
	if ready, err := r.reconcileDatabaseTask(ctx, preview, nsName, componentMigration, migrationJobName, preview.Spec.Database.Migration); err != nil {
		return false, "DatabaseMigrationFailed", err
	} else if !ready {
		return false, "", nil
	}
	if ready, err := r.reconcileDatabaseTask(ctx, preview, nsName, componentSeed, seedJobName, preview.Spec.Database.Seed); err != nil {
		return false, "DatabaseSeedFailed", err
	} else if !ready {
		return false, "", nil
	}

	return true, "", nil
}

func databaseEnabled(preview *platformv1alpha1.Preview) bool {
	return preview.Spec.Database != nil && preview.Spec.Database.Enabled
}

func multiServiceEnabled(c *platformv1alpha1.Preview) bool {
	return len(c.Spec.Services) > 0
}

func serviceDeploymentName(name string) string {
	return "svc-" + name
}

// appServiceURL returns the in-cluster HTTP URL of the primary app service.
// In multi-service mode it targets the first defined service.
func appServiceURL(c *platformv1alpha1.Preview) string {
	if len(c.Spec.Services) > 0 {
		svc := c.Spec.Services[0]
		port := svc.Port
		if port == 0 {
			port = 8080
		}
		return fmt.Sprintf("http://%s:%d", serviceDeploymentName(svc.Name), port)
	}
	return "http://app:8080"
}

// appServiceFQDN returns the full FQDN URL for the primary app service, usable from other namespaces.
func appServiceFQDN(c *platformv1alpha1.Preview, ns string) string {
	if len(c.Spec.Services) > 0 {
		svc := c.Spec.Services[0]
		port := svc.Port
		if port == 0 {
			port = 8080
		}
		return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", serviceDeploymentName(svc.Name), ns, port)
	}
	return fmt.Sprintf("http://app.%s.svc.cluster.local:8080", ns)
}

// frontendServiceURL returns the in-cluster URL of the service with pathPrefix "/".
// Falls back to appServiceURL when no root-path service is found.
func frontendServiceURL(c *platformv1alpha1.Preview) string {
	for _, svc := range c.Spec.Services {
		if svc.PathPrefix == "/" {
			port := svc.Port
			if port == 0 {
				port = 8080
			}
			return fmt.Sprintf("http://%s:%d", serviceDeploymentName(svc.Name), port)
		}
	}
	return appServiceURL(c)
}

// mainAppImage returns the primary app image. In multi-service mode the first service's image is used.
func mainAppImage(c *platformv1alpha1.Preview) string {
	if len(c.Spec.Services) > 0 {
		return c.Spec.Services[0].Image
	}
	return c.Spec.Image
}

func databaseVersion(preview *platformv1alpha1.Preview) string {
	if preview.Spec.Database == nil || preview.Spec.Database.Version == "" {
		return "15"
	}
	return preview.Spec.Database.Version
}

func databaseName(preview *platformv1alpha1.Preview) string {
	if preview.Spec.Database == nil || preview.Spec.Database.DatabaseName == "" {
		return "appdb"
	}
	return preview.Spec.Database.DatabaseName
}

func (r *PreviewReconciler) setDatabaseStatus(preview *platformv1alpha1.Preview, ready bool) {
	if preview.Status.Database == nil {
		preview.Status.Database = &platformv1alpha1.DatabaseStatus{}
	}
	preview.Status.DatabaseSecretName = postgresSecretName
	preview.Status.Database.Ready = ready
	preview.Status.Database.Host = postgresHost
	preview.Status.Database.DatabaseName = databaseName(preview)
	preview.Status.Database.SecretName = postgresSecretName
}

// handleDeletion requests deletion for all known child resources and the
// preview namespace before removing the finalizer.
func (r *PreviewReconciler) handleDeletion(ctx context.Context, preview *platformv1alpha1.Preview) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(preview, previewFinalizer) {
		return ctrl.Result{}, nil
	}

	preview.Status.Phase = platformv1alpha1.PhaseTerminating
	_ = r.Status().Update(ctx, preview)
	syncGitHubAfterStatus(ctx, r, preview, preview.Status.URL)

	nsName := r.namespaceName(preview)
	if err := r.deleteKnownChildren(ctx, nsName); err != nil {
		return ctrl.Result{}, err
	}

	// Clean up AI prompt ConfigMap stored outside the preview namespace.
	promptCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      aiPromptConfigMapName(preview.Name),
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

	controllerutil.RemoveFinalizer(preview, previewFinalizer)
	if err := r.Update(ctx, preview); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Cleanup complete", "name", preview.Name)
	return ctrl.Result{}, nil
}

func (r *PreviewReconciler) deleteKnownChildren(ctx context.Context, nsName string) error {
	children := []client.Object{
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: nsName}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: nsName}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: migrationJobName, Namespace: nsName}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: seedJobName, Namespace: nsName}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: smokeJobName, Namespace: nsName}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: microcksJobName, Namespace: nsName}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: regressionJobName, Namespace: nsName}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: e2eJobName, Namespace: nsName}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: nsName}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: nsName}},
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: nsName}},
		&corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: "preview-quota", Namespace: nsName}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: postgresSecretName, Namespace: nsName}},
	}

	for _, child := range children {
		if err := r.Delete(ctx, child); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete %T %s/%s: %w", child, nsName, child.GetName(), err)
		}
	}
	return nil
}

func (r *PreviewReconciler) ensureControllerReference(ctx context.Context, owner *platformv1alpha1.Preview, obj client.Object) error {
	before := obj.DeepCopyObject().(client.Object)
	if err := controllerutil.SetControllerReference(owner, obj, r.Scheme); err != nil {
		return err
	}
	return r.Patch(ctx, obj, client.MergeFrom(before))
}

// reconcileNamespace ensures the dedicated namespace exists
func (r *PreviewReconciler) reconcileNamespace(ctx context.Context, c *platformv1alpha1.Preview, nsName string) error {
	ns := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: nsName}, ns)
	if errors.IsNotFound(err) {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: nsName,
				Labels: map[string]string{
					labelManagedBy:   "preview-operator",
					labelPreviewName: c.Name,
					"branch":         sanitizeLabel(c.Spec.Branch),
					"pr-number":      fmt.Sprintf("%d", c.Spec.PRNumber),
					// Pod Security Standards: enforce baseline, warn on restricted violations
					"pod-security.kubernetes.io/enforce": "baseline",
					"pod-security.kubernetes.io/warn":    "restricted",
				},
				Annotations: map[string]string{
					"platform.company.io/ttl":        c.Spec.TTL,
					"platform.company.io/created-by": "preview-operator",
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
func (r *PreviewReconciler) reconcileResourceQuota(ctx context.Context, c *platformv1alpha1.Preview, nsName string) error {
	cpuLimitStr, memLimitStr, cpuReqStr, memReqStr := c.ResourceLimits()

	cpuLimit := resource.MustParse(cpuLimitStr)
	memLimit := resource.MustParse(memLimitStr)
	cpuReq := resource.MustParse(cpuReqStr)
	memReq := resource.MustParse(memReqStr)

	if multiServiceEnabled(c) {
		// Each service beyond the first needs its own CPU/memory headroom.
		for i := 1; i < len(c.Spec.Services); i++ {
			cpuLimit.Add(resource.MustParse(cpuLimitStr))
			memLimit.Add(resource.MustParse(memLimitStr))
			cpuReq.Add(resource.MustParse(cpuReqStr))
			memReq.Add(resource.MustParse(memReqStr))
		}
	}

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

	if testSuiteEnabled(c) {
		// Smoke + regression run with standard test resources; E2E uses more for Chromium.
		for range 2 {
			cpuLimit.Add(resource.MustParse(testJobCPULimit))
			memLimit.Add(resource.MustParse(testJobMemoryLimit))
			cpuReq.Add(resource.MustParse(testJobCPURequest))
			memReq.Add(resource.MustParse(testJobMemoryRequest))
		}
		cpuLimit.Add(resource.MustParse(e2eJobCPULimit))
		memLimit.Add(resource.MustParse(e2eJobMemoryLimit))
		cpuReq.Add(resource.MustParse(e2eJobCPURequest))
		memReq.Add(resource.MustParse(e2eJobMemoryRequest))
		if contractTestEnabled(c) {
			cpuLimit.Add(resource.MustParse(testJobCPULimit))
			memLimit.Add(resource.MustParse(testJobMemoryLimit))
			cpuReq.Add(resource.MustParse(testJobCPURequest))
			memReq.Add(resource.MustParse(testJobMemoryRequest))
		}
	}

	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "preview-quota",
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

// reconcileDeployment creates/updates app deployments.
// In multi-service mode each entry in spec.services gets its own Deployment.
func (r *PreviewReconciler) reconcileDeployment(ctx context.Context, c *platformv1alpha1.Preview, nsName string) error {
	if multiServiceEnabled(c) {
		return r.reconcileServiceDeployments(ctx, c, nsName)
	}
	return r.reconcileSingleDeployment(ctx, c, nsName)
}

func (r *PreviewReconciler) reconcileSingleDeployment(ctx context.Context, c *platformv1alpha1.Preview, nsName string) error {
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
			labelManagedBy:   "preview-operator",
			labelPreviewName: c.Name,
		}
		progressDeadlineSeconds := int32(60)
		deploy.Spec = appsv1.DeploymentSpec{
			Replicas:                &replicas,
			ProgressDeadlineSeconds: &progressDeadlineSeconds,
			Strategy:                appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "preview-preview"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":          "preview-preview",
						"branch":       sanitizeLabel(c.Spec.Branch),
						"pr":           fmt.Sprintf("%d", c.Spec.PRNumber),
						labelManagedBy: "preview-operator",
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
								{ContainerPort: 8080, Protocol: corev1.ProtocolTCP},
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
										Path: "/healthz",
										Port: intstr.FromInt(8080),
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

// reconcileServiceDeployments creates/updates one Deployment per entry in spec.services.
func (r *PreviewReconciler) reconcileServiceDeployments(ctx context.Context, c *platformv1alpha1.Preview, nsName string) error {
	cpuLimit, memLimit, cpuReq, memReq := c.ResourceLimits()
	dbEnabled := c.Spec.Database != nil && c.Spec.Database.Enabled
	podAnnotations := telemetryPodAnnotations(c)
	telEnv := telemetryEnv(c, nsName)

	var initContainers []corev1.Container
	var dbEnv []corev1.EnvVar
	if dbEnabled {
		initContainers = []corev1.Container{{
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
		}}
		fromSecret := func(key string) corev1.EnvVar {
			return corev1.EnvVar{Name: key, ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName},
					Key:                  key,
				},
			}}
		}
		dbEnv = []corev1.EnvVar{
			fromSecret("POSTGRES_USER"),
			fromSecret("POSTGRES_PASSWORD"),
			fromSecret("POSTGRES_DB"),
			fromSecret("DATABASE_URL"),
		}
	}

	for _, svc := range c.Spec.Services {
		deployName := serviceDeploymentName(svc.Name)
		replicas := svc.Replicas
		if replicas == 0 {
			replicas = c.Spec.Replicas
			if replicas == 0 {
				replicas = 1
			}
		}
		port := svc.Port
		if port == 0 {
			port = 8080
		}

		env := []corev1.EnvVar{
			{Name: "PREVIEW_BRANCH", Value: c.Spec.Branch},
			{Name: "PREVIEW_PR", Value: fmt.Sprintf("%d", c.Spec.PRNumber)},
			{Name: "ENVIRONMENT", Value: "preview"},
		}
		env = append(env, dbEnv...)
		env = append(env, svc.Env...)
		env = append(env, telEnv...)

		deploy := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deployName, Namespace: nsName}}
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
			if err := controllerutil.SetControllerReference(c, deploy, r.Scheme); err != nil {
				return err
			}
			deploy.Labels = map[string]string{labelManagedBy: "preview-operator", labelPreviewName: c.Name}
			progressDeadlineSeconds := int32(60)
			deploy.Spec = appsv1.DeploymentSpec{
				Replicas:                &replicas,
				ProgressDeadlineSeconds: &progressDeadlineSeconds,
				Strategy:                appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
				Selector:                &metav1.LabelSelector{MatchLabels: map[string]string{"app": deployName}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":          deployName,
							"branch":       sanitizeLabel(c.Spec.Branch),
							"pr":           fmt.Sprintf("%d", c.Spec.PRNumber),
							labelManagedBy: "preview-operator",
						},
						Annotations: podAnnotations,
					},
					Spec: corev1.PodSpec{
						InitContainers: initContainers,
						Containers: []corev1.Container{{
							Name:  svc.Name,
							Image: svc.Image,
							Ports: []corev1.ContainerPort{{ContainerPort: port, Protocol: corev1.ProtocolTCP}},
							Env:   env,
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
									HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt32(port)},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
						}},
					},
				},
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("service %s: %w", svc.Name, err)
		}
	}
	return nil
}

// reconcileMultiServices creates/updates one ClusterIP Service per entry in spec.services.
func (r *PreviewReconciler) reconcileMultiServices(ctx context.Context, c *platformv1alpha1.Preview, nsName string) error {
	for _, svc := range c.Spec.Services {
		deployName := serviceDeploymentName(svc.Name)
		port := svc.Port
		if port == 0 {
			port = 8080
		}
		k8sSvc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: deployName, Namespace: nsName}}
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, k8sSvc, func() error {
			if err := controllerutil.SetControllerReference(c, k8sSvc, r.Scheme); err != nil {
				return err
			}
			k8sSvc.Labels = map[string]string{labelManagedBy: "preview-operator"}
			k8sSvc.Spec = corev1.ServiceSpec{
				Selector: map[string]string{"app": deployName},
				Ports: []corev1.ServicePort{{
					Port:       port,
					TargetPort: intstr.FromInt32(port),
					Protocol:   corev1.ProtocolTCP,
				}},
				Type: corev1.ServiceTypeClusterIP,
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("service %s: %w", svc.Name, err)
		}
	}
	return nil
}

func (r *PreviewReconciler) handleAppAvailability(ctx context.Context, c *platformv1alpha1.Preview, nsName string) (bool, ctrl.Result, error) {
	deployNames := []string{"app"}
	if multiServiceEnabled(c) {
		deployNames = make([]string, len(c.Spec.Services))
		for i, svc := range c.Spec.Services {
			deployNames[i] = serviceDeploymentName(svc.Name)
		}
	}

	for _, deployName := range deployNames {
		appReady, appReason, err := r.appDeploymentReady(ctx, nsName, deployName)
		if err != nil {
			result, err := r.setFailedStatus(ctx, c, appReason, err)
			return true, result, err
		}
		if !appReady {
			c.Status.Phase = platformv1alpha1.PhaseProvisioning
			c.Status.NamespaceName = nsName
			c.Status.ObservedGeneration = c.Generation
			c.SetCondition(metav1.Condition{
				Type:               platformv1alpha1.ConditionReady,
				Status:             metav1.ConditionFalse,
				Reason:             appReason,
				Message:            fmt.Sprintf("Waiting for %s deployment to become available", deployName),
				LastTransitionTime: metav1.Now(),
			})
			if err := r.Status().Update(ctx, c); err != nil {
				return true, ctrl.Result{}, err
			}
			syncGitHubAfterStatus(ctx, r, c, "")
			return true, ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}
	return false, ctrl.Result{}, nil
}

func (r *PreviewReconciler) appDeploymentReady(ctx context.Context, nsName, deployName string) (bool, string, error) {
	deploy := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: deployName, Namespace: nsName}, deploy); err != nil {
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

func telemetryPodAnnotations(c *platformv1alpha1.Preview) map[string]string {
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

func telemetryEnv(c *platformv1alpha1.Preview, nsName string) []corev1.EnvVar {
	if c.Spec.Telemetry == nil || !c.Spec.Telemetry.Enabled {
		return nil
	}

	serviceName := c.Spec.Telemetry.ServiceName
	if serviceName == "" {
		serviceName = fmt.Sprintf("preview-%s", c.Name)
	}

	resourceAttributes := fmt.Sprintf(
		"preview.name=%s,preview.pr_number=%d,preview.branch=%s,k8s.namespace.name=%s",
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
func (r *PreviewReconciler) reconcileService(ctx context.Context, c *platformv1alpha1.Preview, nsName string) error {
	if multiServiceEnabled(c) {
		return r.reconcileMultiServices(ctx, c, nsName)
	}
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
		svc.Labels = map[string]string{labelManagedBy: "preview-operator"}
		svc.Spec = corev1.ServiceSpec{
			Selector: map[string]string{"app": "preview-preview"},
			Ports: []corev1.ServicePort{
				{
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		}
		return nil
	})
	return err
}

// reconcilePostgresSecret creates a Secret with unique credentials the first time only.
// If the Secret already exists its data is never overwritten, preserving credentials across reconcile loops.
func (r *PreviewReconciler) reconcilePostgresSecret(ctx context.Context, c *platformv1alpha1.Preview, nsName string) error {
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
				labelManagedBy:   "preview-operator",
				labelPreviewName: c.Name,
			},
			Annotations: map[string]string{
				"platform.company.io/managed-by":      "preview-operator",
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
func (r *PreviewReconciler) reconcilePostgresDeployment(ctx context.Context, c *platformv1alpha1.Preview, nsName string) error {
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
			labelManagedBy:                "preview-operator",
			labelPreviewName:              c.Name,
			"app.kubernetes.io/component": "database",
		}
		deploy.Spec = appsv1.DeploymentSpec{
			Replicas: &one,
			// Recreate avoids two postgres pods briefly running against the same data directory
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "postgres", labelPreviewName: c.Name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":                         "postgres",
						labelPreviewName:              c.Name,
						labelManagedBy:                "preview-operator",
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
func (r *PreviewReconciler) reconcilePostgresService(ctx context.Context, c *platformv1alpha1.Preview, nsName string) error {
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
			labelManagedBy:                "preview-operator",
			"app.kubernetes.io/component": "database",
		}
		svc.Spec = corev1.ServiceSpec{
			Selector: map[string]string{"app": "postgres", labelPreviewName: c.Name},
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

func (r *PreviewReconciler) reconcileDatabaseTask(ctx context.Context, c *platformv1alpha1.Preview, nsName, taskName, jobName string, task *platformv1alpha1.DatabaseTaskSpec) (bool, error) {
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

func (r *PreviewReconciler) databaseTaskJob(c *platformv1alpha1.Preview, nsName, taskName, jobName string, task *platformv1alpha1.DatabaseTaskSpec) *batchv1.Job {
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
				labelManagedBy:                "preview-operator",
				labelPreviewName:              c.Name,
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
						labelManagedBy:             "preview-operator",
						labelPreviewName:           c.Name,
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

func (r *PreviewReconciler) markDatabaseTaskRunning(c *platformv1alpha1.Preview, taskName, conditionType, jobName string) {
	r.setDatabaseTaskStatus(c, taskName, phaseRunning)
	c.SetCondition(metav1.Condition{
		Type:               conditionType,
		Status:             metav1.ConditionFalse,
		Reason:             "JobRunning",
		Message:            fmt.Sprintf("Database %s job %s is running", taskName, jobName),
		LastTransitionTime: metav1.Now(),
	})
}

func (r *PreviewReconciler) setDatabaseTaskStatus(c *platformv1alpha1.Preview, taskName, status string) {
	r.setDatabaseStatus(c, false)
	if taskName == componentSeed {
		c.Status.Database.Seed = status
		return
	}
	c.Status.Database.Migration = status
}

func (r *PreviewReconciler) databaseTaskStatus(c *platformv1alpha1.Preview, taskName string) string {
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

func (r *PreviewReconciler) setFailedStatus(ctx context.Context, c *platformv1alpha1.Preview, reason string, err error) (ctrl.Result, error) {
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
	r.captureFailureReport(ctx, c)
	syncGitHubAfterStatus(ctx, r, c, c.Status.URL)
	return ctrl.Result{RequeueAfter: 30 * time.Second}, err
}

func (r *PreviewReconciler) namespaceName(c *platformv1alpha1.Preview) string {
	return fmt.Sprintf("preview-pr-%d", c.Spec.PRNumber)
}

func clearStatusCondition(c *platformv1alpha1.Preview, conditionType string) {
	filtered := c.Status.Conditions[:0]
	for _, cond := range c.Status.Conditions {
		if cond.Type == conditionType {
			continue
		}
		filtered = append(filtered, cond)
	}
	c.Status.Conditions = filtered
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

func (r *PreviewReconciler) deleteDatabaseJobs(ctx context.Context, nsName string) error {
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

func (r *PreviewReconciler) deleteTestSuiteJobs(ctx context.Context, nsName string) error {
	jobs := []client.Object{
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: smokeJobName, Namespace: nsName}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: microcksJobName, Namespace: nsName}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: regressionJobName, Namespace: nsName}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: e2eJobName, Namespace: nsName}},
	}
	for _, job := range jobs {
		if err := r.Delete(ctx, job); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (r *PreviewReconciler) deleteAIResources(ctx context.Context, nsName string) error {
	objects := []client.Object{
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: aiEnrichmentConfigMap, Namespace: nsName}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: aiSeedJobName, Namespace: nsName}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: aiTestJobName, Namespace: nsName}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: aiSchemaJobName, Namespace: nsName}},
	}
	for _, obj := range objects {
		if err := r.Delete(ctx, obj); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// reconcileNetworkPolicy creates a NetworkPolicy that isolates the preview namespace:
// - allows inter-pod traffic within the namespace
// - allows ingress from ingress-nginx and istio-system (so preview URLs are reachable)
// - allows all egress (DB connections, AI API, GitHub API, image pulls)
// - denies all other ingress by default
func (r *PreviewReconciler) reconcileNetworkPolicy(ctx context.Context, c *platformv1alpha1.Preview, nsName string) error {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "preview-isolation",
			Namespace: nsName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, np, func() error {
		if err := controllerutil.SetControllerReference(c, np, r.Scheme); err != nil {
			return err
		}
		np.Labels = map[string]string{labelManagedBy: "preview-operator"}
		np.Spec = networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					// Allow inter-pod communication within the preview namespace
					From: []networkingv1.NetworkPolicyPeer{
						{PodSelector: &metav1.LabelSelector{}},
					},
				},
				{
					// Allow ingress from ingress-nginx and Istio ingress gateway
					From: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": "ingress-nginx",
								},
							},
						},
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": "istio-system",
								},
							},
						},
					},
				},
			},
			// Allow all egress: external DB, AI API, GitHub API, container registry
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{},
			},
		}
		return nil
	})
	return err
}

// migrateDiffPatch moves spec.changeContext.diffPatch into a ConfigMap so that
// the raw diff does not appear in kubectl describe output.
func (r *PreviewReconciler) migrateDiffPatch(ctx context.Context, c *platformv1alpha1.Preview, nsName string) error {
	if c.Spec.ChangeContext == nil || c.Spec.ChangeContext.DiffPatch == "" {
		return nil
	}
	cmName := "preview-diff-" + c.Name
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: nsName},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		if err := controllerutil.SetControllerReference(c, cm, r.Scheme); err != nil {
			return err
		}
		cm.Labels = map[string]string{labelManagedBy: "preview-operator"}
		cm.Data = map[string]string{"diff.patch": c.Spec.ChangeContext.DiffPatch}
		return nil
	})
	if err != nil {
		return err
	}
	patch := client.MergeFrom(c.DeepCopy())
	c.Spec.ChangeContext.DiffPatchRef = cmName
	c.Spec.ChangeContext.DiffPatch = ""
	return r.Patch(ctx, c, patch)
}

// SetupWithManager registers the controller
func (r *PreviewReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.IstioEnabled = istioAvailable(mgr.GetRESTMapper())

	b := ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Preview{}).
		Owns(&corev1.Namespace{}).
		Owns(&corev1.ResourceQuota{}).
		Owns(&corev1.Secret{}).
		Owns(&appsv1.Deployment{}).
		Owns(&batchv1.Job{}).
		Owns(&corev1.Service{}).
		Watches(&platformv1alpha1.TestPlan{}, testPlanEventHandler())

	if r.IstioEnabled {
		vs := &unstructured.Unstructured{}
		vs.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "networking.istio.io",
			Version: "v1beta1",
			Kind:    "VirtualService",
		})
		b = b.Owns(vs)
	} else {
		b = b.Owns(&networkingv1.Ingress{})
	}

	b = b.Owns(&networkingv1.NetworkPolicy{})

	return b.Complete(r)
}
