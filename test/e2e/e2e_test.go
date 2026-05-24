//go:build e2e
// +build e2e

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

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/example/kube-billing/test/utils"
)

// namespace where the project is deployed in
const namespace = "kube-billing-system"

// serviceAccountName created for the project
const serviceAccountName = "kube-billing-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "kube-billing-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "kube-billing-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", managerImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace)
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod's status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Serving metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted, 3*time.Minute, time.Second).Should(Succeed())
		})
	})

	Context("Billing Flow E2E", func() {
		const (
			testNamespace = "billing-test"
			planName      = "e2e-test-plan"
			subName       = "e2e-test-sub"
		)

		BeforeAll(func() {
			By("creating test namespace")
			cmd := exec.Command("kubectl", "create", "ns", testNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create test namespace")

			By("waiting for controller to be ready")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "control-plane=controller-manager", "-n", namespace, "-o", "jsonpath={.items[0].status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}, 2*time.Minute, time.Second).Should(Succeed())
		})

		AfterAll(func() {
			By("deleting test namespace")
			cmd := exec.Command("kubectl", "delete", "ns", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})

		AfterEach(func() {
			specReport := CurrentSpecReport()
			if specReport.Failed() {
				By("Fetching subscription status for debugging")
				cmd := exec.Command("kubectl", "get", "subscription", subName, "-n", testNamespace, "-o", "yaml")
				output, err := utils.Run(cmd)
				if err == nil {
					_, _ = fmt.Fprintf(GinkgoWriter, "Subscription status:\n %s", output)
				} else {
					_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get subscription status: %s", err)
				}

				By("Fetching controller manager logs")
				cmd = exec.Command("kubectl", "logs", "-l", "control-plane=controller-manager", "-n", namespace)
				logs, err := utils.Run(cmd)
				if err == nil {
					_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", logs)
				} else {
					_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get controller logs: %s", err)
				}
			}
		})

		It("should create BillingPlan and Subscription, then process billing", func() {
			By("Creating a BillingPlan")
			planYAML := fmt.Sprintf(`
apiVersion: billing.cloud-native.io/v1alpha1
kind: BillingPlan
metadata:
  name: %s
  namespace: %s
spec:
  price: "10.00"
  currency: USD
  billingPeriod: monthly
  requeueIntervalSeconds: 30
`, planName, testNamespace)
			tmpFile := filepath.Join(os.TempDir(), "e2e-plan.yaml")
			Expect(os.WriteFile(tmpFile, []byte(planYAML), 0o644)).To(Succeed())
			cmd := exec.Command("kubectl", "apply", "-f", tmpFile)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create BillingPlan")

			By("Waiting for BillingPlan to be available")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "billingplan", planName, "-n", testNamespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring(planName))
			}, 30*time.Second, time.Second).Should(Succeed())

			By("Creating a Subscription")
			subYAML := fmt.Sprintf(`
apiVersion: billing.cloud-native.io/v1alpha1
kind: Subscription
metadata:
  name: %s
  namespace: %s
spec:
  userId: e2e-user1
  planRef: %s
`, subName, testNamespace, planName)
			tmpFile = filepath.Join(os.TempDir(), "e2e-sub.yaml")
			Expect(os.WriteFile(tmpFile, []byte(subYAML), 0o644)).To(Succeed())
			cmd = exec.Command("kubectl", "apply", "-f", tmpFile)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create Subscription")

			By("Waiting for subscription to be activated")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "subscription", subName, "-n", testNamespace, "-o", "jsonpath={.status.state}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Active"))
			}, 60*time.Second, time.Second).Should(Succeed())

			By("Verifying subscription has finalizer")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "subscription", subName, "-n", testNamespace, "-o", "jsonpath={.spec.finalizers}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("billing.cloud-native.io/finalizer"))
			}, 10*time.Second, time.Second).Should(Succeed())

			By("Waiting for billing cycle to complete")
			// Ждём 35 секунд чтобы billing цикл сработал (интервал 30 секунд)
			time.Sleep(35 * time.Second)

			By("Verifying payment was processed")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "subscription", subName, "-n", testNamespace, "-o", "jsonpath={.status.lastPayment}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).ToNot(BeEmpty())
			}, 10*time.Second, time.Second).Should(Succeed())

			By("Verifying metrics show active subscription")
			// Получаем метрики через kubectl port-forward
			cmd = exec.Command("kubectl", "get", "pods", "-n", namespace, "-l", "control-plane=controller-manager", "-o", "jsonpath={.items[0].metadata.name}")
			podName, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			podName = utils.GetNonEmptyLines(podName)[0]

			// Используем kubectl exec для получения метрик
			cmd = exec.Command("kubectl", "exec", podName, "-n", namespace, "--", "wget", "-qO-", "localhost:8080/metrics")
			metricsOutput, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(metricsOutput).To(ContainSubstring("billing_active_subscriptions 1"))
			Expect(metricsOutput).To(ContainSubstring("billing_revenue_total"))

			By("Cleaning up test resources")
			cmd = exec.Command("kubectl", "delete", "subscription", subName, "-n", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
			cmd = exec.Command("kubectl", "delete", "billingplan", planName, "-n", testNamespace, "--ignore-not-found=true")
			_, _ = utils.Run(cmd)
		})
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	// Temporary file to store the token request
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		// Execute kubectl command to create the token
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		// Parse the JSON output to extract the token
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() (string, error) {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	return utils.Run(cmd)
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
