// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package oob

import (
	"fmt"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"testing"
	"time"

	metaldhcpv1alpha1 "github.com/SAP-cloud-infrastructure/metaldhcp/api/v1alpha1"
	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/kubernetes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	. "sigs.k8s.io/controller-runtime/pkg/envtest/komega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	pollingInterval      = 50 * time.Millisecond
	eventuallyTimeout    = 3 * time.Second
	consistentlyDuration = 1 * time.Second
)

var (
	cfg      *rest.Config
	crClient client.Client
	testEnv  *envtest.Environment
)

func envtestK8sVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == "k8s.io/api" {
				var minor int
				fmt.Sscanf(dep.Version, "v0.%d.", &minor)
				if minor > 0 {
					return fmt.Sprintf("1.%d.0", minor)
				}
			}
		}
	}
	return "1.30.0"
}

func TestOOB(t *testing.T) {
	SetDefaultConsistentlyPollingInterval(pollingInterval)
	SetDefaultEventuallyPollingInterval(pollingInterval)
	SetDefaultEventuallyTimeout(eventuallyTimeout)
	SetDefaultConsistentlyDuration(consistentlyDuration)
	RegisterFailHandler(Fail)

	RunSpecs(t, "OOB Plugin Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,

		// BinaryAssetsDirectory is only used when KUBEBUILDER_ASSETS is unset
		// (e.g. running the suite directly rather than via `make test`). The
		// Makefile target provisions the binaries and exports KUBEBUILDER_ASSETS,
		// which takes precedence over this path.
		BinaryAssetsDirectory: filepath.Join("..", "..", "bin", "k8s",
			fmt.Sprintf("%s-%s-%s", envtestK8sVersion(), runtime.GOOS, runtime.GOARCH)),
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	DeferCleanup(testEnv.Stop)

	Expect(metaldhcpv1alpha1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())

	crClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(crClient).NotTo(BeNil())

	SetClient(crClient)
	kubernetes.SetClient(&crClient)
})

// newNamespace creates a uniquely named namespace for a single spec and schedules
// its deletion.
func newNamespace(ctx SpecContext) *corev1.Namespace {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "oob-test-"},
	}
	Expect(crClient.Create(ctx, ns)).To(Succeed(), "failed to create test namespace")
	DeferCleanup(crClient.Delete, ns)
	return ns
}
