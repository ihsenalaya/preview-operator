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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/ihsenalaya/preview-operator/api/v1alpha1"
)

var _ = Describe("Preview Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		preview := &platformv1alpha1.Preview{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Preview")
			err := k8sClient.Get(ctx, typeNamespacedName, preview)
			if err != nil && errors.IsNotFound(err) {
				resource := &platformv1alpha1.Preview{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: platformv1alpha1.PreviewSpec{
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
			resource := &platformv1alpha1.Preview{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Preview")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &PreviewReconciler{
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
			preview := &platformv1alpha1.Preview{
				ObjectMeta: metav1.ObjectMeta{Name: "demo"},
				Spec: platformv1alpha1.PreviewSpec{
					Branch:   "feature/demo",
					PRNumber: 2,
					Telemetry: &platformv1alpha1.TelemetrySpec{
						Enabled:     true,
						ServiceName: "preview-demo",
						AutoInstrumentation: &platformv1alpha1.AutoInstrumentationSpec{
							Language:           platformv1alpha1.TelemetryLanguagePython,
							InstrumentationRef: "observability/python",
						},
					},
				},
			}

			Expect(telemetryPodAnnotations(preview)).To(Equal(map[string]string{
				"instrumentation.opentelemetry.io/inject-python": "observability/python",
			}))
			Expect(telemetryEnv(preview, "preview-pr-2")).To(ContainElements(
				corev1.EnvVar{Name: "OTEL_SERVICE_NAME", Value: "preview-demo"},
				corev1.EnvVar{Name: "OTEL_RESOURCE_ATTRIBUTES", Value: "preview.name=demo,preview.pr_number=2,preview.branch=feature/demo,k8s.namespace.name=preview-pr-2"},
			))
		})

		It("should use OpenTelemetry defaults when optional fields are omitted", func() {
			preview := &platformv1alpha1.Preview{
				ObjectMeta: metav1.ObjectMeta{Name: "demo"},
				Spec: platformv1alpha1.PreviewSpec{
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

			Expect(telemetryPodAnnotations(preview)).To(Equal(map[string]string{
				"instrumentation.opentelemetry.io/inject-nodejs": "true",
			}))
			Expect(telemetryEnv(preview, "preview-pr-7")).To(ContainElement(
				corev1.EnvVar{Name: "OTEL_SERVICE_NAME", Value: "preview-demo"},
			))
		})
	})

	Context("When database tasks are enabled", func() {
		It("should build migration jobs with database credentials injected", func() {
			preview := &platformv1alpha1.Preview{
				ObjectMeta: metav1.ObjectMeta{Name: "demo"},
				Spec: platformv1alpha1.PreviewSpec{
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

			reconciler := &PreviewReconciler{}
			job := reconciler.databaseTaskJob(preview, "preview-pr-12", "migration", migrationJobName, preview.Spec.Database.Migration)

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
		It("should map Preview phases to GitHub deployment states", func() {
			Expect(githubDeploymentStateForPhase(platformv1alpha1.PhasePending)).To(Equal("queued"))
			Expect(githubDeploymentStateForPhase(platformv1alpha1.PhaseProvisioning)).To(Equal("in_progress"))
			Expect(githubDeploymentStateForPhase(platformv1alpha1.PhaseRunning)).To(Equal("success"))
			Expect(githubDeploymentStateForPhase(platformv1alpha1.PhaseFailed)).To(Equal("failure"))
			Expect(githubDeploymentStateForPhase(platformv1alpha1.PhaseTerminating)).To(Equal("inactive"))
		})

		It("should detect already delivered GitHub notifications", func() {
			preview := &platformv1alpha1.Preview{
				Status: platformv1alpha1.PreviewStatus{
					Phase: platformv1alpha1.PhaseRunning,
					GitHub: &platformv1alpha1.GitHubIntegrationStatus{
						DeploymentState:    "success",
						LastNotifiedPhase:  platformv1alpha1.PhaseRunning,
						LastEnvironmentURL: "http://pr-7.preview.localtest.me:8080",
						CommentID:          123,
					},
				},
			}

			Expect(githubAlreadyNotified(preview, "success", "http://pr-7.preview.localtest.me:8080", true)).To(BeTrue())
			Expect(githubAlreadyNotified(preview, "success", "http://pr-7.preview.localtest.me:8080", false)).To(BeTrue())
			Expect(githubAlreadyNotified(preview, "pending", "http://pr-7.preview.localtest.me:8080", false)).To(BeFalse())
		})

		It("should build a ready comment with database evidence", func() {
			expiry := metav1.Now()
			preview := &platformv1alpha1.Preview{
				ObjectMeta: metav1.ObjectMeta{Name: "pr-7"},
				Spec: platformv1alpha1.PreviewSpec{
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
				Status: platformv1alpha1.PreviewStatus{
					NamespaceName: "preview-pr-7",
					ExpiresAt:     &expiry,
					Database: &platformv1alpha1.DatabaseStatus{
						Ready:     true,
						Migration: "Succeeded",
						Seed:      "Skipped",
					},
				},
			}

			body := githubReadyCommentBody(preview, "http://pr-7.preview.localtest.me:8080")

			Expect(body).To(ContainSubstring("## Preview Ready"))
			Expect(body).To(ContainSubstring("PostgreSQL: ready"))
			Expect(body).To(ContainSubstring("Migration: Succeeded"))
			Expect(body).To(ContainSubstring("Seed: Skipped"))
			Expect(body).To(ContainSubstring("Telemetry: enabled"))
			Expect(body).To(ContainSubstring("AI-Assisted Summary"))
		})

		Context("When using multi-service mode", func() {
			It("should detect multi-service mode from spec.services", func() {
				single := &platformv1alpha1.Preview{Spec: platformv1alpha1.PreviewSpec{Image: "nginx:latest"}}
				Expect(multiServiceEnabled(single)).To(BeFalse())

				multi := &platformv1alpha1.Preview{
					Spec: platformv1alpha1.PreviewSpec{
						Services: []platformv1alpha1.ServiceSpec{
							{Name: "backend", Image: "api:latest", Port: 8080},
						},
					},
				}
				Expect(multiServiceEnabled(multi)).To(BeTrue())
			})

			It("should prefix service names with svc-", func() {
				Expect(serviceDeploymentName("backend")).To(Equal("svc-backend"))
				Expect(serviceDeploymentName("frontend")).To(Equal("svc-frontend"))
			})

			It("should build app URL targeting first service in multi-service mode", func() {
				multi := &platformv1alpha1.Preview{
					Spec: platformv1alpha1.PreviewSpec{
						Services: []platformv1alpha1.ServiceSpec{
							{Name: "backend", Port: 8080},
							{Name: "frontend", Port: 3000},
						},
					},
				}
				Expect(appServiceURL(multi)).To(Equal("http://svc-backend:8080"))
			})

			It("should default app URL port to 80 when service port is unset", func() {
				multi := &platformv1alpha1.Preview{
					Spec: platformv1alpha1.PreviewSpec{
						Services: []platformv1alpha1.ServiceSpec{
							{Name: "api"},
						},
					},
				}
				Expect(appServiceURL(multi)).To(Equal("http://svc-api:8080"))
			})

			It("should return http://app:80 in single-service mode", func() {
				single := &platformv1alpha1.Preview{Spec: platformv1alpha1.PreviewSpec{Image: "nginx:latest"}}
				Expect(appServiceURL(single)).To(Equal("http://app:8080"))
			})

			It("should reconcile multi-service deployments and ingress in the cluster", func() {
				ctx := context.Background()
				const prNumber = 55
				nsName := fmt.Sprintf("preview-pr-%d", prNumber)
				resourceName := fmt.Sprintf("pr-%d", prNumber)

				By("creating the namespace")
				ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
				_ = k8sClient.Create(ctx, ns)

				By("creating the Preview resource with two services")
				cr := &platformv1alpha1.Preview{
					ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
					Spec: platformv1alpha1.PreviewSpec{
						Branch:       "feature/multi-svc",
						PRNumber:     prNumber,
						Image:        "unused",
						TTL:          "24h",
						ResourceTier: platformv1alpha1.TierSmall,
						Services: []platformv1alpha1.ServiceSpec{
							{Name: "backend", Image: "api:test", Port: 8080, PathPrefix: "/api"},
							{Name: "frontend", Image: "ui:test", Port: 3000, PathPrefix: "/"},
						},
					},
				}
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())

				By("reconciling — pass 1 adds finalizer, pass 2 provisions resources")
				reconciler := &PreviewReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
				req := reconcile.Request{NamespacedName: types.NamespacedName{Name: resourceName, Namespace: "default"}}
				_, err := reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				_, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())

				By("verifying svc-backend deployment exists")
				backendDeploy := &appsv1.Deployment{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "svc-backend", Namespace: nsName}, backendDeploy)).To(Succeed())
				Expect(backendDeploy.Spec.Template.Spec.Containers).To(HaveLen(1))
				Expect(backendDeploy.Spec.Template.Spec.Containers[0].Image).To(Equal("api:test"))
				Expect(backendDeploy.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort).To(Equal(int32(8080)))

				By("verifying svc-frontend deployment exists")
				frontendDeploy := &appsv1.Deployment{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "svc-frontend", Namespace: nsName}, frontendDeploy)).To(Succeed())
				Expect(frontendDeploy.Spec.Template.Spec.Containers[0].Image).To(Equal("ui:test"))
				Expect(frontendDeploy.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort).To(Equal(int32(3000)))

				By("verifying ClusterIP services exist")
				backendSvc := &corev1.Service{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "svc-backend", Namespace: nsName}, backendSvc)).To(Succeed())
				Expect(backendSvc.Spec.Ports[0].Port).To(Equal(int32(8080)))
				Expect(backendSvc.Spec.Selector).To(HaveKeyWithValue("app", "svc-backend"))

				frontendSvc := &corev1.Service{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "svc-frontend", Namespace: nsName}, frontendSvc)).To(Succeed())
				Expect(frontendSvc.Spec.Ports[0].Port).To(Equal(int32(3000)))

				By("verifying ingress has path-based routing (longer prefix first)")
				ing := &networkingv1.Ingress{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "app", Namespace: nsName}, ing)).To(Succeed())
				Expect(ing.Spec.Rules).To(HaveLen(1))
				paths := ing.Spec.Rules[0].HTTP.Paths
				Expect(paths).To(HaveLen(2))
				Expect(paths[0].Path).To(Equal("/api"))
				Expect(paths[0].Backend.Service.Name).To(Equal("svc-backend"))
				Expect(paths[1].Path).To(Equal("/"))
				Expect(paths[1].Backend.Service.Name).To(Equal("svc-frontend"))

				By("verifying PREVIEW_BRANCH and PREVIEW_PR env vars are injected")
				backendEnv := backendDeploy.Spec.Template.Spec.Containers[0].Env
				Expect(backendEnv).To(ContainElement(corev1.EnvVar{Name: "PREVIEW_BRANCH", Value: "feature/multi-svc"}))
				Expect(backendEnv).To(ContainElement(corev1.EnvVar{Name: "PREVIEW_PR", Value: "55"}))

				By("verifying readiness probe uses /healthz not / (backend has no GET / route)")
				probe := backendDeploy.Spec.Template.Spec.Containers[0].ReadinessProbe
				Expect(probe).NotTo(BeNil())
				Expect(probe.HTTPGet.Path).To(Equal("/healthz"))
				Expect(probe.HTTPGet.Port.IntVal).To(Equal(int32(8080)))

				By("cleanup")
				Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
			})

			It("should inject DB env vars into all services when database is enabled", func() {
				ctx := context.Background()
				const prNumber = 56
				nsName := fmt.Sprintf("preview-pr-%d", prNumber)

				By("creating the namespace")
				ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
				_ = k8sClient.Create(ctx, ns)

				By("creating postgres-credentials secret in the namespace")
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: postgresSecretName, Namespace: nsName},
					StringData: map[string]string{
						"POSTGRES_USER":     "preview_56",
						"POSTGRES_PASSWORD": "secret",
						"POSTGRES_DB":       "appdb",
						"DATABASE_URL":      "postgresql://preview_56:secret@postgres:5432/appdb",
					},
				}
				_ = k8sClient.Create(ctx, secret)

				cr := &platformv1alpha1.Preview{
					ObjectMeta: metav1.ObjectMeta{Name: "pr-56", Namespace: "default"},
					Spec: platformv1alpha1.PreviewSpec{
						Branch:       "feature/db-multi",
						PRNumber:     prNumber,
						Image:        "unused",
						TTL:          "24h",
						ResourceTier: platformv1alpha1.TierSmall,
						Database:     &platformv1alpha1.DatabaseSpec{Enabled: true, DatabaseName: "appdb"},
						Services: []platformv1alpha1.ServiceSpec{
							{Name: "backend", Image: "api:test", Port: 8080, PathPrefix: "/api"},
							{Name: "frontend", Image: "ui:test", Port: 3000, PathPrefix: "/"},
						},
					},
				}
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())

				By("reconciling — pass 1 adds finalizer, pass 2 provisions resources")
				reconciler := &PreviewReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
				req56 := reconcile.Request{NamespacedName: types.NamespacedName{Name: "pr-56", Namespace: "default"}}
				_, err := reconciler.Reconcile(ctx, req56)
				Expect(err).NotTo(HaveOccurred())
				_, err = reconciler.Reconcile(ctx, req56)
				Expect(err).NotTo(HaveOccurred())

				By("verifying DATABASE_URL is injected via secretKeyRef into backend")
				backendDeploy := &appsv1.Deployment{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "svc-backend", Namespace: nsName}, backendDeploy)).To(Succeed())
				backendEnv := backendDeploy.Spec.Template.Spec.Containers[0].Env
				Expect(backendEnv).To(ContainElement(secretKeyRef("DATABASE_URL", "DATABASE_URL")))

				By("verifying wait-for-postgres init container is added")
				Expect(backendDeploy.Spec.Template.Spec.InitContainers).To(HaveLen(1))
				Expect(backendDeploy.Spec.Template.Spec.InitContainers[0].Name).To(Equal("wait-for-postgres"))

				By("cleanup")
				Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
			})
		})

		It("should build a failed comment with diagnostics", func() {
			preview := &platformv1alpha1.Preview{
				ObjectMeta: metav1.ObjectMeta{Name: "pr-9"},
				Spec: platformv1alpha1.PreviewSpec{
					PRNumber: 9,
				},
				Status: platformv1alpha1.PreviewStatus{
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

			body := githubFailedCommentBody(preview)

			Expect(body).To(ContainSubstring("## Preview Failed"))
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

	Context("Idempotence — multiple reconcile calls produce no error", func() {
		It("should be fully idempotent across repeated reconcile passes", func() {
			ctx := context.Background()
			const prNumber = 70
			nsName := fmt.Sprintf("preview-pr-%d", prNumber)
			resourceName := fmt.Sprintf("pr-%d", prNumber)

			cr := &platformv1alpha1.Preview{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
				Spec: platformv1alpha1.PreviewSpec{
					Branch:       "feature/idempotent",
					PRNumber:     prNumber,
					Image:        "nginx:alpine",
					TTL:          "48h",
					ResourceTier: platformv1alpha1.TierSmall,
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			reconciler := &PreviewReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: resourceName, Namespace: "default"}}

			for i := 0; i < 4; i++ {
				_, err := reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred(), "reconcile pass %d should not error", i+1)
			}

			By("namespace must exist exactly once")
			nsList := &corev1.NamespaceList{}
			Expect(k8sClient.List(ctx, nsList, client.MatchingLabels{labelPreviewName: resourceName})).To(Succeed())
			Expect(nsList.Items).To(HaveLen(1))

			By("cleanup")
			Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
			_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}})
		})
	})

	Context("Resource creation — namespace (PSS labels), NetworkPolicy, Deployment, Service", func() {
		It("should create all expected resources after two reconcile passes", func() {
			ctx := context.Background()
			const prNumber = 71
			nsName := fmt.Sprintf("preview-pr-%d", prNumber)
			resourceName := fmt.Sprintf("pr-%d", prNumber)

			cr := &platformv1alpha1.Preview{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
				Spec: platformv1alpha1.PreviewSpec{
					Branch:       "feature/resources",
					PRNumber:     prNumber,
					Image:        "nginx:alpine",
					TTL:          "48h",
					ResourceTier: platformv1alpha1.TierSmall,
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			reconciler := &PreviewReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: resourceName, Namespace: "default"}}
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			By("namespace has Pod Security Standards labels")
			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns)).To(Succeed())
			Expect(ns.Labels).To(HaveKeyWithValue("pod-security.kubernetes.io/enforce", "baseline"))
			Expect(ns.Labels).To(HaveKeyWithValue("pod-security.kubernetes.io/warn", "restricted"))

			By("NetworkPolicy preview-isolation isolates the namespace")
			np := &networkingv1.NetworkPolicy{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "preview-isolation", Namespace: nsName}, np)).To(Succeed())
			Expect(np.Spec.PolicyTypes).To(ContainElements(networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress))
			Expect(np.Spec.Egress).To(HaveLen(1))
			Expect(np.Spec.Ingress).To(HaveLen(2))

			By("Deployment app is created with the correct image")
			deploy := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "app", Namespace: nsName}, deploy)).To(Succeed())
			Expect(deploy.Spec.Template.Spec.Containers[0].Image).To(Equal("nginx:alpine"))

			By("ClusterIP Service app is created")
			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "app", Namespace: nsName}, svc)).To(Succeed())
			Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))

			By("cleanup")
			Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
			_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}})
		})
	})

	Context("Job resumption — pre-existing Job does not cause a reconcile error", func() {
		It("should skip Job creation if Job already exists", func() {
			ctx := context.Background()
			const prNumber = 72
			nsName := fmt.Sprintf("preview-pr-%d", prNumber)
			resourceName := fmt.Sprintf("pr-%d", prNumber)

			By("pre-creating the namespace and a smoke-tests Job")
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
			_ = k8sClient.Create(ctx, ns)

			existingJob := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: smokeJobName, Namespace: nsName},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyNever,
							Containers:    []corev1.Container{{Name: "test", Image: "alpine"}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, existingJob)).To(Succeed())

			cr := &platformv1alpha1.Preview{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
				Spec: platformv1alpha1.PreviewSpec{
					Branch:       "feature/job-resume",
					PRNumber:     prNumber,
					Image:        "nginx:alpine",
					TTL:          "48h",
					ResourceTier: platformv1alpha1.TierSmall,
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			reconciler := &PreviewReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: resourceName, Namespace: "default"}}
			for i := 0; i < 3; i++ {
				_, err := reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred(), "reconcile pass %d should not error with pre-existing job", i+1)
			}

			By("pre-existing smoke job is still present — not deleted or duplicated")
			job := &batchv1.Job{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: smokeJobName, Namespace: nsName}, job)).To(Succeed())
			Expect(job.Spec.Template.Spec.Containers[0].Image).To(Equal("alpine"))

			By("cleanup")
			Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
			_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}})
		})
	})

	Context("Cleanup — finalizer added then removed on CR deletion", func() {
		It("should add finalizer on first reconcile and remove it when CR is deleted", func() {
			ctx := context.Background()
			const prNumber = 73
			resourceName := fmt.Sprintf("pr-%d", prNumber)
			nsName := fmt.Sprintf("preview-pr-%d", prNumber)

			cr := &platformv1alpha1.Preview{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
				Spec: platformv1alpha1.PreviewSpec{
					Branch:       "feature/finalizer",
					PRNumber:     prNumber,
					Image:        "nginx:alpine",
					TTL:          "48h",
					ResourceTier: platformv1alpha1.TierSmall,
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			reconciler := &PreviewReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: resourceName, Namespace: "default"}}
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			By("finalizer is added after the first reconcile")
			updated := &platformv1alpha1.Preview{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, updated)).To(Succeed())
			Expect(updated.Finalizers).To(ContainElement(previewFinalizer))

			By("deleting the CR and running deletion reconcile removes the finalizer")
			Expect(k8sClient.Delete(ctx, updated)).To(Succeed())
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			deleted := &platformv1alpha1.Preview{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: "default"}, deleted)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "CR should be gone after finalizer runs")

			_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}})
		})
	})
})
