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

package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	platformv1alpha1 "github.com/company/cellenza-operator/api/v1alpha1"
	// TODO (user): Add any additional imports if needed
)

var _ = Describe("Cellenza Webhook", func() {
	var (
		obj       *platformv1alpha1.Cellenza
		oldObj    *platformv1alpha1.Cellenza
		validator CellenzaCustomValidator
		defaulter CellenzaCustomDefaulter
	)

	BeforeEach(func() {
		obj = &platformv1alpha1.Cellenza{}
		oldObj = &platformv1alpha1.Cellenza{}
		validator = CellenzaCustomValidator{}
		Expect(validator).NotTo(BeNil(), "Expected validator to be initialized")
		defaulter = CellenzaCustomDefaulter{}
		Expect(defaulter).NotTo(BeNil(), "Expected defaulter to be initialized")
		Expect(oldObj).NotTo(BeNil(), "Expected oldObj to be initialized")
		Expect(obj).NotTo(BeNil(), "Expected obj to be initialized")
	})

	AfterEach(func() {
		// TODO (user): Add any teardown logic common to all tests
	})

	Context("When creating Cellenza under Defaulting Webhook", func() {
		// TODO (user): Add logic for defaulting webhooks
		// Example:
		// It("Should apply defaults when a required field is empty", func() {
		//     By("simulating a scenario where defaults should be applied")
		//     obj.SomeFieldWithDefault = ""
		//     By("calling the Default method to apply defaults")
		//     defaulter.Default(ctx, obj)
		//     By("checking that the default values are set")
		//     Expect(obj.SomeFieldWithDefault).To(Equal("default_value"))
		// })
	})

	Context("When creating or updating Cellenza under Validating Webhook", func() {
		validCellenza := func() *platformv1alpha1.Cellenza {
			return &platformv1alpha1.Cellenza{
				Spec: platformv1alpha1.CellenzaSpec{
					Branch:       "demo",
					PRNumber:     1,
					Image:        "ghcr.io/ihsenalaya/cellenza-demo-app:0.5.1",
					ResourceTier: platformv1alpha1.TierSmall,
					Replicas:     1,
				},
			}
		}

		It("Should admit valid Python auto-instrumentation", func() {
			obj = validCellenza()
			obj.Spec.Telemetry = &platformv1alpha1.TelemetrySpec{
				Enabled:     true,
				ServiceName: "cellenza-demo",
				AutoInstrumentation: &platformv1alpha1.AutoInstrumentationSpec{
					Language:           platformv1alpha1.TelemetryLanguagePython,
					InstrumentationRef: "observability/python",
				},
			}

			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should deny enabled telemetry without auto-instrumentation", func() {
			obj = validCellenza()
			obj.Spec.Telemetry = &platformv1alpha1.TelemetrySpec{Enabled: true}

			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(MatchError(ContainSubstring("spec.telemetry.autoInstrumentation is required")))
		})

		It("Should require a target executable for Go auto-instrumentation", func() {
			obj = validCellenza()
			obj.Spec.Telemetry = &platformv1alpha1.TelemetrySpec{
				Enabled: true,
				AutoInstrumentation: &platformv1alpha1.AutoInstrumentationSpec{
					Language: platformv1alpha1.TelemetryLanguageGo,
				},
			}

			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(MatchError(ContainSubstring("goTargetExecutable is required")))
		})

		It("Should reject Python platform on non-Python auto-instrumentation", func() {
			obj = validCellenza()
			obj.Spec.Telemetry = &platformv1alpha1.TelemetrySpec{
				Enabled: true,
				AutoInstrumentation: &platformv1alpha1.AutoInstrumentationSpec{
					Language:       platformv1alpha1.TelemetryLanguageJava,
					PythonPlatform: "glibc",
				},
			}

			_, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(MatchError(ContainSubstring("pythonPlatform is only valid for Python")))
		})

		// TODO (user): Add logic for validating webhooks
		// Example:
		// It("Should deny creation if a required field is missing", func() {
		//     By("simulating an invalid creation scenario")
		//     obj.SomeRequiredField = ""
		//     Expect(validator.ValidateCreate(ctx, obj)).Error().To(HaveOccurred())
		// })
		//
		// It("Should admit creation if all required fields are present", func() {
		//     By("simulating an invalid creation scenario")
		//     obj.SomeRequiredField = "valid_value"
		//     Expect(validator.ValidateCreate(ctx, obj)).To(BeNil())
		// })
		//
		// It("Should validate updates correctly", func() {
		//     By("simulating a valid update scenario")
		//     oldObj.SomeRequiredField = "updated_value"
		//     obj.SomeRequiredField = "updated_value"
		//     Expect(validator.ValidateUpdate(ctx, oldObj, obj)).To(BeNil())
		// })
	})

})
