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

package platform

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// staleCRDFieldManager stands in for Helm, which created the runtime CRDs
// from the subcharts' crds/ on a release's first install.
const staleCRDFieldManager = "helm"

// chartCRDs parses every CRD the chart ships in its crds/ directory, each
// document of a file holding several included.
func chartCRDs(chrt *chart.Chart) []*apiextensionsv1.CustomResourceDefinition {
	GinkgoHelper()
	objs := chrt.CRDObjects()
	out := make([]*apiextensionsv1.CustomResourceDefinition, 0, len(objs))
	for _, obj := range objs {
		reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(obj.File.Data)))
		for {
			doc, err := reader.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			Expect(err).NotTo(HaveOccurred(), "read %s", obj.Filename)
			crd := &apiextensionsv1.CustomResourceDefinition{}
			Expect(yaml.Unmarshal(doc, crd)).To(Succeed(), "parse %s", obj.Filename)
			if crd.Name == "" {
				continue
			}
			out = append(out, crd)
		}
	}
	Expect(out).NotTo(BeEmpty(), "chart %s ships no CRDs", chrt.Name())
	return out
}

// writeStaleCRDs puts every CRD the chart ships on the cluster with each
// version's schema reduced to a bare object, the state of a cluster
// installed from an older release: the API server prunes every field the
// stale schema does not declare.
func writeStaleCRDs(chrt *chart.Chart) {
	GinkgoHelper()
	for _, crd := range chartCRDs(chrt) {
		stale := crd.DeepCopy()
		for i := range stale.Spec.Versions {
			stale.Spec.Versions[i].Schema = &apiextensionsv1.CustomResourceValidation{
				OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object"},
			}
		}

		live := &apiextensionsv1.CustomResourceDefinition{}
		err := k8sClient.Get(ctx, client.ObjectKey{Name: stale.Name}, live)
		if apierrors.IsNotFound(err) {
			Expect(k8sClient.Create(ctx, stale, client.FieldOwner(staleCRDFieldManager))).To(Succeed())
			continue
		}
		Expect(err).NotTo(HaveOccurred())
		live.Spec = stale.Spec
		Expect(k8sClient.Update(ctx, live, client.FieldOwner(staleCRDFieldManager))).To(Succeed())
	}
}

// expectChartCRDsApplied waits until every CRD the chart ships carries the
// chart's schema for each of its versions.
func expectChartCRDsApplied(chrt *chart.Chart) {
	GinkgoHelper()
	for _, want := range chartCRDs(chrt) {
		Eventually(func(g Gomega) {
			live := &apiextensionsv1.CustomResourceDefinition{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: want.Name}, live)).To(Succeed())
			g.Expect(live.Spec.Versions).To(HaveLen(len(want.Spec.Versions)))
			for i := range want.Spec.Versions {
				g.Expect(live.Spec.Versions[i].Name).To(Equal(want.Spec.Versions[i].Name))
				g.Expect(live.Spec.Versions[i].Schema).To(Equal(want.Spec.Versions[i].Schema),
					"schema of %s %s", want.Name, want.Spec.Versions[i].Name)
			}
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
	}
}
