/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/company/cellenza-operator/api/v1alpha1"
)

var _ = Describe("Cellenza Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		cellenza := &platformv1alpha1.Cellenza{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Cellenza")
			err := k8sClient.Get(ctx, typeNamespacedName, cellenza)
			if err != nil && errors.IsNotFound(err) {
				resource := &platformv1alpha1.Cellenza{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: platformv1alpha1.CellenzaSpec{
						Branch:       "feature-test",
						PRNumber:     42,
						Image:        "nginx:1.27-alpine",
						TTL:          "48h",
						ResourceTier: platformv1alpha1.TierSmall,
						Replicas:     1,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &platformv1alpha1.Cellenza{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Cellenza")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &CellenzaReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})

	Context("When telemetry is enabled", func() {
		It("should build OpenTelemetry pod annotations and environment variables", func() {
			cellenza := &platformv1alpha1.Cellenza{
				ObjectMeta: metav1.ObjectMeta{Name: "demo"},
				Spec: platformv1alpha1.CellenzaSpec{
					Branch:   "feature/demo",
					PRNumber: 2,
					Telemetry: &platformv1alpha1.TelemetrySpec{
						Enabled:     true,
						ServiceName: "cellenza-demo",
						AutoInstrumentation: &platformv1alpha1.AutoInstrumentationSpec{
							Language:           platformv1alpha1.TelemetryLanguagePython,
							InstrumentationRef: "observability/python",
						},
					},
				},
			}

			Expect(telemetryPodAnnotations(cellenza)).To(Equal(map[string]string{
				"instrumentation.opentelemetry.io/inject-python": "observability/python",
			}))
			Expect(telemetryEnv(cellenza, "preview-pr-2")).To(ContainElements(
				corev1.EnvVar{Name: "OTEL_SERVICE_NAME", Value: "cellenza-demo"},
				corev1.EnvVar{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: "cellenza.name=demo,cellenza.pr_number=2,cellenza.branch=feature/demo,k8s.namespace.name=preview-pr-2"},
			))
		})

		It("should use OpenTelemetry defaults when optional fields are omitted", func() {
			cellenza := &platformv1alpha1.Cellenza{
				ObjectMeta: metav1.ObjectMeta{Name: "demo"},
				Spec: platformv1alpha1.CellenzaSpec{
					Branch:   "demo",
					PRNumber: 7,
					Telemetry: &platformv1alpha1.TelemetrySpec{
						Enabled: true,
						AutoInstrumentation: &platformv1alpha1.AutoInstrumentationSpec{
							Language: platformv1alpha1.TelemetryLanguageNodeJS,
						},
					},
				},
			}

			Expect(telemetryPodAnnotations(cellenza)).To(Equal(map[string]string{
				"instrumentation.opentelemetry.io/inject-nodejs": "true",
			}))
			Expect(telemetryEnv(cellenza, "preview-pr-7")).To(ContainElement(
				corev1.EnvVar{Name: "OTEL_SERVICE_NAME", Value: "cellenza-demo"},
			))
		})
	})

	Context("When database tasks are enabled", func() {
		It("should build migration jobs with database credentials injected", func() {
			cellenza := &platformv1alpha1.Cellenza{
				ObjectMeta: metav1.ObjectMeta{Name: "demo"},
				Spec: platformv1alpha1.CellenzaSpec{
					Branch:   "feature/db",
					PRNumber: 12,
					Image:    "ghcr.io/example/app:sha",
					Database: &platformv1alpha1.DatabaseSpec{
						Enabled: true,
						Migration: &platformv1alpha1.DatabaseTaskSpec{
							Enabled: true,
							Command: []string{"python", "-m", "alembic", "upgrade", "head"},
						},
					},
				},
			}

			reconciler := &CellenzaReconciler{}
			job := reconciler.databaseTaskJob(cellenza, "preview-pr-12", "migration", migrationJobName, cellenza.Spec.Database.Migration)

			Expect(job.Name).To(Equal(migrationJobName))
			Expect(job.Namespace).To(Equal("preview-pr-12"))
			Expect(job.Spec.Template.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyNever))
			Expect(job.Spec.Template.Spec.InitContainers).To(HaveLen(1))
			Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(job.Spec.Template.Spec.Containers[0].Image).To(Equal("ghcr.io/example/app:sha"))
			Expect(job.Spec.Template.Spec.Containers[0].Command).To(Equal([]string{"python", "-m", "alembic", "upgrade", "head"}))
			Expect(job.Spec.Template.Spec.Containers[0].Env).To(ContainElement(
				secretKeyRef("DATABASE_URL", "DATABASE_URL"),
			))
		})
	})

	Context("When collecting diagnostics", func() {
		It("should keep context around Python syntax errors", func() {
			lines := []string{
				"Traceback (most recent call last):",
				"  File \"/app/app.py\", line 2",
				"    this is not valid python",
				"                ^",
				"SyntaxError: invalid syntax",
			}

			significant := selectSignificantLines(lines, 6)

			Expect(significant).To(ContainElement("Traceback (most recent call last):"))
			Expect(significant).To(ContainElement("File \"/app/app.py\", line 2"))
			Expect(significant).To(ContainElement("this is not valid python"))
			Expect(significant).To(ContainElement("SyntaxError: invalid syntax"))
		})

		It("should infer syntax errors as a high-confidence app failure", func() {
			diag := &platformv1alpha1.DiagnosticsStatus{
				Component: componentApp,
				Message:   "Deployment app is unavailable: Deployment does not have minimum availability.",
				SignificantLogs: []platformv1alpha1.DiagnosticLogExcerpt{
					{
						Component: componentApp,
						Lines: []string{
							"File \"/app/app.py\", line 2",
							"SyntaxError: invalid syntax",
						},
					},
				},
			}

			rootCause, confidence := inferRootCause(diag)

			Expect(rootCause).To(Equal("Application failed to start due to a syntax error"))
			Expect(confidence).To(Equal(diagnosticConfidenceHigh))
		})
	})

	Context("When GitHub integration is enabled", func() {
		It("should map Cellenza phases to GitHub deployment states", func() {
			Expect(githubDeploymentStateForPhase(platformv1alpha1.PhasePending)).To(Equal("queued"))
			Expect(githubDeploymentStateForPhase(platformv1alpha1.PhaseProvisioning)).To(Equal("in_progress"))
			Expect(githubDeploymentStateForPhase(platformv1alpha1.PhaseRunning)).To(Equal("success"))
			Expect(githubDeploymentStateForPhase(platformv1alpha1.PhaseFailed)).To(Equal("failure"))
			Expect(githubDeploymentStateForPhase(platformv1alpha1.PhaseTerminating)).To(Equal("inactive"))
		})

		It("should detect already delivered GitHub notifications", func() {
			cellenza := &platformv1alpha1.Cellenza{
				Status: platformv1alpha1.CellenzaStatus{
					Phase: platformv1alpha1.PhaseRunning,
					GitHub: &platformv1alpha1.GitHubIntegrationStatus{
						DeploymentState:    "success",
						LastNotifiedPhase:  platformv1alpha1.PhaseRunning,
						LastEnvironmentURL: "http://pr-7.preview.localtest.me:8080",
						CommentID:          123,
					},
				},
			}

			Expect(githubAlreadyNotified(cellenza, "success", "http://pr-7.preview.localtest.me:8080", true)).To(BeTrue())
			Expect(githubAlreadyNotified(cellenza, "success", "http://pr-7.preview.localtest.me:8080", false)).To(BeTrue())
			Expect(githubAlreadyNotified(cellenza, "pending", "http://pr-7.preview.localtest.me:8080", false)).To(BeFalse())
		})

		It("should build a ready comment with database evidence", func() {
			expiry := metav1.Now()
			cellenza := &platformv1alpha1.Cellenza{
				ObjectMeta: metav1.ObjectMeta{Name: "pr-7"},
				Spec: platformv1alpha1.CellenzaSpec{
					PRNumber: 7,
					Database: &platformv1alpha1.DatabaseSpec{
						Enabled: true,
					},
					Telemetry: &platformv1alpha1.TelemetrySpec{
						Enabled: true,
					},
					GitHub: &platformv1alpha1.GitHubIntegrationSpec{
						Environment: "pr-7",
					},
				},
				Status: platformv1alpha1.CellenzaStatus{
					NamespaceName: "preview-pr-7",
					ExpiresAt:     &expiry,
					Database: &platformv1alpha1.DatabaseStatus{
						Ready:     true,
						Migration: "Succeeded",
						Seed:      "Skipped",
					},
				},
			}

			body := githubReadyCommentBody(cellenza, "http://pr-7.preview.localtest.me:8080")

			Expect(body).To(ContainSubstring("Cellenza Preview Ready"))
			Expect(body).To(ContainSubstring("PostgreSQL: ready"))
			Expect(body).To(ContainSubstring("Migration: Succeeded"))
			Expect(body).To(ContainSubstring("Seed: Skipped"))
			Expect(body).To(ContainSubstring("Telemetry: enabled"))
			Expect(body).To(ContainSubstring("AI-Assisted Summary"))
		})

		It("should build a failed comment with diagnostics", func() {
			cellenza := &platformv1alpha1.Cellenza{
				ObjectMeta: metav1.ObjectMeta{Name: "pr-9"},
				Spec: platformv1alpha1.CellenzaSpec{
					PRNumber: 9,
				},
				Status: platformv1alpha1.CellenzaStatus{
					NamespaceName: "preview-pr-9",
					Diagnostics: &platformv1alpha1.DiagnosticsStatus{
						Reason:     "DatabaseMigrationFailed",
						Component:  "migration",
						Message:    "Job postgres-migrate failed: relation messages already exists",
						RootCause:  "Database migration failed",
						Confidence: "high",
						Recommendations: []string{
							"Make the migration idempotent.",
						},
						SignificantLogs: []platformv1alpha1.DiagnosticLogExcerpt{
							{
								Component: "migration",
								Source:    "pod/postgres-migrate-abc container/migration",
								Lines: []string{
									"ERROR relation messages already exists",
								},
							},
						},
						LastEvents: []string{
							"Pod/postgres-migrate: BackoffLimitExceeded",
						},
						DebugCommands: []string{
							"kubectl logs -n preview-pr-9 job/postgres-migrate",
						},
					},
				},
			}

			body := githubFailedCommentBody(cellenza)

			Expect(body).To(ContainSubstring("Cellenza Preview Failed"))
			Expect(body).To(ContainSubstring("DatabaseMigrationFailed"))
			Expect(body).To(ContainSubstring("migration"))
			Expect(body).To(ContainSubstring("Database migration failed"))
			Expect(body).To(ContainSubstring("Confidence: `high`"))
			Expect(body).To(ContainSubstring("ERROR relation messages already exists"))
			Expect(body).To(ContainSubstring("Make the migration idempotent"))
			Expect(body).To(ContainSubstring("relation messages already exists"))
			Expect(body).To(ContainSubstring("kubectl logs -n preview-pr-9 job/postgres-migrate"))
		})
	})
})
