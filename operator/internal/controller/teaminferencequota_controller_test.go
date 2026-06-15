/*
Copyright 2026 KubeFisher.

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

	"github.com/m2khosravi/kubefisher/pkg/promclient"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	quotav1alpha1 "github.com/m2khosravi/kubefisher/operator/api/v1alpha1"
)

var _ = Describe("TeamInferenceQuota Controller", func() {
	const (
		ns   = "default"
		name = "tiq-reconcile-test"
	)
	ctx := context.Background()

	BeforeEach(func() {
		_ = client.IgnoreNotFound(k8sClient.Delete(ctx, &quotav1alpha1.TeamInferenceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		}))
	})

	newQuota := func() *quotav1alpha1.TeamInferenceQuota {
		a := int32(80)
		return &quotav1alpha1.TeamInferenceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: quotav1alpha1.TeamInferenceQuotaSpec{
				DailyTokenBudget:    100,
				MonthlyCostLimitUSD: "500.00",
				AlertThresholdPct:   &a,
				EnforcementMode:     quotav1alpha1.EnforcementModeEnforce,
			},
		}
	}

	It("patches status Active when spend is below thresholds", func() {
		srv := prometheusQueryTestServer(50, 10)
		defer srv.Close()
		pc, err := promclient.NewClient(srv.URL)
		Expect(err).NotTo(HaveOccurred())

		obj := newQuota()
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		defer func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed()) }()

		r := &TeamInferenceQuotaReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Prom:     pc,
			Recorder: record.NewFakeRecorder(10),
		}
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}})
		Expect(err).NotTo(HaveOccurred())

		var got quotav1alpha1.TeamInferenceQuota
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(quotav1alpha1.QuotaPhaseActive))
		Expect(got.Status.TokensUsedToday).To(BeNumerically("==", 50))
		Expect(got.Status.CostUsedThisMonth).To(Equal("$10.00"))
	})

	It("patches status Exceeded when token spend meets budget", func() {
		srv := prometheusQueryTestServer(100, 0)
		defer srv.Close()
		pc, err := promclient.NewClient(srv.URL)
		Expect(err).NotTo(HaveOccurred())

		obj := newQuota()
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		defer func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed()) }()

		r := &TeamInferenceQuotaReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Prom:     pc,
			Recorder: record.NewFakeRecorder(10),
		}
		res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(ctrl.Result{RequeueAfter: requeueInterval}.RequeueAfter))

		var got quotav1alpha1.TeamInferenceQuota
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(quotav1alpha1.QuotaPhaseExceeded))
	})
})
