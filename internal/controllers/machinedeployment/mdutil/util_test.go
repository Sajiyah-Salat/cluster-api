/*
Copyright 2018 The Kubernetes Authors.

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

package mdutil

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/klog/v2/klogr"
	"k8s.io/utils/pointer"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

func newDControllerRef(md *clusterv1.MachineDeployment) *metav1.OwnerReference {
	isController := true
	return &metav1.OwnerReference{
		APIVersion: "clusters/v1alpha",
		Kind:       "MachineDeployment",
		Name:       md.GetName(),
		UID:        md.GetUID(),
		Controller: &isController,
	}
}

// generateMS creates a machine set, with the input deployment's template as its template.
func generateMS(md clusterv1.MachineDeployment) clusterv1.MachineSet {
	template := md.Spec.Template.DeepCopy()
	return clusterv1.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			UID:             randomUID(),
			Name:            names.SimpleNameGenerator.GenerateName("machineset"),
			Labels:          template.Labels,
			OwnerReferences: []metav1.OwnerReference{*newDControllerRef(&md)},
		},
		Spec: clusterv1.MachineSetSpec{
			Replicas: new(int32),
			Template: *template,
			Selector: metav1.LabelSelector{MatchLabels: template.Labels},
		},
	}
}

func randomUID() types.UID {
	return types.UID(strconv.FormatInt(rand.Int63(), 10)) //nolint:gosec
}

// generateDeployment creates a deployment, with the input image as its template.
func generateDeployment(image string) clusterv1.MachineDeployment {
	machineLabels := map[string]string{"name": image}
	return clusterv1.MachineDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        image,
			Annotations: make(map[string]string),
		},
		Spec: clusterv1.MachineDeploymentSpec{
			Replicas: pointer.Int32(3),
			Selector: metav1.LabelSelector{MatchLabels: machineLabels},
			Template: clusterv1.MachineTemplateSpec{
				ObjectMeta: clusterv1.ObjectMeta{
					Labels: machineLabels,
				},
				Spec: clusterv1.MachineSpec{
					NodeDrainTimeout: &metav1.Duration{Duration: 10 * time.Second},
				},
			},
		},
	}
}

func TestMachineSetsByDecreasingReplicas(t *testing.T) {
	t0 := time.Now()
	t1 := t0.Add(1 * time.Minute)
	msAReplicas1T0 := &clusterv1.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.Time{Time: t0},
			Name:              "ms-a",
		},
		Spec: clusterv1.MachineSetSpec{
			Replicas: pointer.Int32(1),
		},
	}

	msAAReplicas3T0 := &clusterv1.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.Time{Time: t0},
			Name:              "ms-aa",
		},
		Spec: clusterv1.MachineSetSpec{
			Replicas: pointer.Int32(3),
		},
	}

	msBReplicas1T0 := &clusterv1.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.Time{Time: t0},
			Name:              "ms-b",
		},
		Spec: clusterv1.MachineSetSpec{
			Replicas: pointer.Int32(1),
		},
	}

	msAReplicas1T1 := &clusterv1.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.Time{Time: t1},
			Name:              "ms-a",
		},
		Spec: clusterv1.MachineSetSpec{
			Replicas: pointer.Int32(1),
		},
	}

	tests := []struct {
		name        string
		machineSets []*clusterv1.MachineSet
		want        []*clusterv1.MachineSet
	}{
		{
			name:        "machine set with higher replicas should be lower in the list",
			machineSets: []*clusterv1.MachineSet{msAReplicas1T0, msAAReplicas3T0},
			want:        []*clusterv1.MachineSet{msAAReplicas3T0, msAReplicas1T0},
		},
		{
			name:        "MachineSet created earlier should be lower in the list if replicas are the same",
			machineSets: []*clusterv1.MachineSet{msAReplicas1T1, msAReplicas1T0},
			want:        []*clusterv1.MachineSet{msAReplicas1T0, msAReplicas1T1},
		},
		{
			name:        "MachineSet with lower name should be lower in the list if the replicas and creationTimestamp are same",
			machineSets: []*clusterv1.MachineSet{msBReplicas1T0, msAReplicas1T0},
			want:        []*clusterv1.MachineSet{msAReplicas1T0, msBReplicas1T0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// sort the machine sets and verify the sorted list
			g := NewWithT(t)
			sort.Sort(MachineSetsByDecreasingReplicas(tt.machineSets))
			g.Expect(tt.machineSets).To(Equal(tt.want))
		})
	}
}

func TestEqualMachineTemplate(t *testing.T) {
	machineTemplate := &clusterv1.MachineTemplateSpec{
		ObjectMeta: clusterv1.ObjectMeta{
			Labels:      map[string]string{"l1": "v1"},
			Annotations: map[string]string{"a1": "v1"},
		},
		Spec: clusterv1.MachineSpec{
			NodeDrainTimeout:        &metav1.Duration{Duration: 10 * time.Second},
			NodeDeletionTimeout:     &metav1.Duration{Duration: 10 * time.Second},
			NodeVolumeDetachTimeout: &metav1.Duration{Duration: 10 * time.Second},
			InfrastructureRef: corev1.ObjectReference{
				Name:       "infra1",
				Namespace:  "default",
				Kind:       "InfrastructureMachine",
				APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: &corev1.ObjectReference{
					Name:       "bootstrap1",
					Namespace:  "default",
					Kind:       "BootstrapConfig",
					APIVersion: "bootstrap.cluster.x-k8s.io/v1beta1",
				},
			},
		},
	}

	machineTemplateWithEmptyLabels := machineTemplate.DeepCopy()
	machineTemplateWithEmptyLabels.Labels = map[string]string{}

	machineTemplateWithDifferentLabels := machineTemplate.DeepCopy()
	machineTemplateWithDifferentLabels.Labels = map[string]string{"l2": "v2"}

	machineTemplateWithEmptyAnnotations := machineTemplate.DeepCopy()
	machineTemplateWithEmptyAnnotations.Annotations = map[string]string{}

	machineTemplateWithDifferentAnnotations := machineTemplate.DeepCopy()
	machineTemplateWithDifferentAnnotations.Annotations = map[string]string{"a2": "v2"}

	machineTemplateWithDifferentInPlaceMutableSpecFields := machineTemplate.DeepCopy()
	machineTemplateWithDifferentInPlaceMutableSpecFields.Spec.NodeDrainTimeout = &metav1.Duration{Duration: 20 * time.Second}
	machineTemplateWithDifferentInPlaceMutableSpecFields.Spec.NodeDeletionTimeout = &metav1.Duration{Duration: 20 * time.Second}
	machineTemplateWithDifferentInPlaceMutableSpecFields.Spec.NodeVolumeDetachTimeout = &metav1.Duration{Duration: 20 * time.Second}

	machineTemplateWithDifferentInfraRef := machineTemplate.DeepCopy()
	machineTemplateWithDifferentInfraRef.Spec.InfrastructureRef.Name = "infra2"

	machineTemplateWithDifferentInfraRefAPIVersion := machineTemplate.DeepCopy()
	machineTemplateWithDifferentInfraRefAPIVersion.Spec.InfrastructureRef.APIVersion = "infrastructure.cluster.x-k8s.io/v1beta2"

	machineTemplateWithDifferentBootstrapConfigRef := machineTemplate.DeepCopy()
	machineTemplateWithDifferentBootstrapConfigRef.Spec.Bootstrap.ConfigRef.Name = "bootstrap2"

	machineTemplateWithDifferentBootstrapConfigRefAPIVersion := machineTemplate.DeepCopy()
	machineTemplateWithDifferentBootstrapConfigRefAPIVersion.Spec.Bootstrap.ConfigRef.APIVersion = "bootstrap.cluster.x-k8s.io/v1beta2"

	tests := []struct {
		Name           string
		Former, Latter *clusterv1.MachineTemplateSpec
		Expected       bool
	}{
		{
			Name:     "Same spec, except latter does not have labels",
			Former:   machineTemplate,
			Latter:   machineTemplateWithEmptyLabels,
			Expected: true,
		},
		{
			Name:     "Same spec, except latter has different labels",
			Former:   machineTemplate,
			Latter:   machineTemplateWithDifferentLabels,
			Expected: true,
		},
		{
			Name:     "Same spec, except latter does not have annotations",
			Former:   machineTemplate,
			Latter:   machineTemplateWithEmptyAnnotations,
			Expected: true,
		},
		{
			Name:     "Same spec, except latter has different annotations",
			Former:   machineTemplate,
			Latter:   machineTemplateWithDifferentAnnotations,
			Expected: true,
		},
		{
			Name:     "Spec changes, latter has different in-place mutable spec fields",
			Former:   machineTemplate,
			Latter:   machineTemplateWithDifferentInPlaceMutableSpecFields,
			Expected: true,
		},
		{
			Name:     "Spec changes, latter has different InfrastructureRef",
			Former:   machineTemplate,
			Latter:   machineTemplateWithDifferentInfraRef,
			Expected: false,
		},
		{
			Name:     "Spec changes, latter has different Bootstrap.ConfigRef",
			Former:   machineTemplate,
			Latter:   machineTemplateWithDifferentBootstrapConfigRef,
			Expected: false,
		},
		{
			Name:     "Same spec, except latter has different InfrastructureRef APIVersion",
			Former:   machineTemplate,
			Latter:   machineTemplateWithDifferentInfraRefAPIVersion,
			Expected: true,
		},
		{
			Name:     "Same spec, except latter has different Bootstrap.ConfigRef APIVersion",
			Former:   machineTemplate,
			Latter:   machineTemplateWithDifferentBootstrapConfigRefAPIVersion,
			Expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			g := NewWithT(t)

			runTest := func(t1, t2 *clusterv1.MachineTemplateSpec) {
				// Run
				equal := EqualMachineTemplate(t1, t2)
				g.Expect(equal).To(Equal(test.Expected))
				g.Expect(t1.Labels).NotTo(BeNil())
				g.Expect(t2.Labels).NotTo(BeNil())
			}

			runTest(test.Former, test.Latter)
			// Test the same case in reverse order
			runTest(test.Latter, test.Former)
		})
	}
}

func TestFindNewMachineSet(t *testing.T) {
	deployment := generateDeployment("nginx")

	matchingMS := generateMS(deployment)

	matchingMSHigherReplicas := generateMS(deployment)
	matchingMSHigherReplicas.Spec.Replicas = pointer.Int32(2)

	matchingMSDiffersInPlaceMutableFields := generateMS(deployment)
	matchingMSDiffersInPlaceMutableFields.Spec.Template.Spec.NodeDrainTimeout = &metav1.Duration{Duration: 20 * time.Second}

	oldMS := generateMS(deployment)
	oldMS.Spec.Template.Spec.InfrastructureRef.Name = "changed-infra-ref"

	tests := []struct {
		Name       string
		deployment clusterv1.MachineDeployment
		msList     []*clusterv1.MachineSet
		expected   *clusterv1.MachineSet
	}{
		{
			Name:       "Get the MachineSet with the MachineTemplate that matches the intent of the MachineDeployment",
			deployment: deployment,
			msList:     []*clusterv1.MachineSet{&oldMS, &matchingMS},
			expected:   &matchingMS,
		},
		{
			Name:       "Get the MachineSet with the higher replicas if multiple MachineSets match the desired intent on the MachineDeployment",
			deployment: deployment,
			msList:     []*clusterv1.MachineSet{&oldMS, &matchingMS, &matchingMSHigherReplicas},
			expected:   &matchingMSHigherReplicas,
		},
		{
			Name:       "Get the MachineSet with the MachineTemplate that matches the desired intent on the MachineDeployment, except differs in in-place mutable fields",
			deployment: deployment,
			msList:     []*clusterv1.MachineSet{&oldMS, &matchingMSDiffersInPlaceMutableFields},
			expected:   &matchingMSDiffersInPlaceMutableFields,
		},
		{
			Name:       "Get nil if no MachineSet matches the desired intent of the MachineDeployment",
			deployment: deployment,
			msList:     []*clusterv1.MachineSet{&oldMS},
			expected:   nil,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			g := NewWithT(t)

			ms := FindNewMachineSet(&test.deployment, test.msList)
			g.Expect(ms).To(Equal(test.expected))
		})
	}
}

func TestFindOldMachineSets(t *testing.T) {
	deployment := generateDeployment("nginx")

	newMS := generateMS(deployment)
	newMS.Name = "aa"
	newMS.Spec.Replicas = pointer.Int32(1)

	newMSHigherReplicas := generateMS(deployment)
	newMSHigherReplicas.Spec.Replicas = pointer.Int32(2)

	newMSHigherName := generateMS(deployment)
	newMSHigherName.Spec.Replicas = pointer.Int32(1)
	newMSHigherName.Name = "ab"

	oldDeployment := generateDeployment("nginx")
	oldDeployment.Spec.Template.Spec.InfrastructureRef.Name = "changed-infra-ref"
	oldMS := generateMS(oldDeployment)

	tests := []struct {
		Name            string
		deployment      clusterv1.MachineDeployment
		msList          []*clusterv1.MachineSet
		expected        []*clusterv1.MachineSet
		expectedRequire []*clusterv1.MachineSet
	}{
		{
			Name:            "Get old MachineSets",
			deployment:      deployment,
			msList:          []*clusterv1.MachineSet{&newMS, &oldMS},
			expected:        []*clusterv1.MachineSet{&oldMS},
			expectedRequire: nil,
		},
		{
			Name:            "Get old MachineSets with no new MachineSet",
			deployment:      deployment,
			msList:          []*clusterv1.MachineSet{&oldMS},
			expected:        []*clusterv1.MachineSet{&oldMS},
			expectedRequire: nil,
		},
		{
			Name:            "Get old MachineSets with two new MachineSets, only the MachineSet with higher replicas is seen as new MachineSet",
			deployment:      deployment,
			msList:          []*clusterv1.MachineSet{&oldMS, &newMS, &newMSHigherReplicas},
			expected:        []*clusterv1.MachineSet{&oldMS, &newMS},
			expectedRequire: []*clusterv1.MachineSet{&newMS},
		},
		{
			Name:            "Get old MachineSets with two new MachineSets, when replicas are matching only the MachineSet with lower name is seen as new MachineSet",
			deployment:      deployment,
			msList:          []*clusterv1.MachineSet{&oldMS, &newMS, &newMSHigherName},
			expected:        []*clusterv1.MachineSet{&oldMS, &newMSHigherName},
			expectedRequire: []*clusterv1.MachineSet{&newMSHigherName},
		},
		{
			Name:            "Get empty old MachineSets",
			deployment:      deployment,
			msList:          []*clusterv1.MachineSet{&newMS},
			expected:        []*clusterv1.MachineSet{},
			expectedRequire: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			g := NewWithT(t)

			requireMS, allMS := FindOldMachineSets(&test.deployment, test.msList)
			g.Expect(allMS).To(ConsistOf(test.expected))
			// MSs are getting filtered correctly by ms.spec.replicas
			g.Expect(requireMS).To(ConsistOf(test.expectedRequire))
		})
	}
}

func TestGetReplicaCountForMachineSets(t *testing.T) {
	ms1 := generateMS(generateDeployment("foo"))
	*(ms1.Spec.Replicas) = 1
	ms1.Status.Replicas = 2
	ms2 := generateMS(generateDeployment("bar"))
	*(ms2.Spec.Replicas) = 5
	ms2.Status.Replicas = 3

	tests := []struct {
		Name           string
		Sets           []*clusterv1.MachineSet
		ExpectedCount  int32
		ExpectedActual int32
		ExpectedTotal  int32
	}{
		{
			Name:           "1:2 Replicas",
			Sets:           []*clusterv1.MachineSet{&ms1},
			ExpectedCount:  1,
			ExpectedActual: 2,
			ExpectedTotal:  2,
		},
		{
			Name:           "6:5 Replicas",
			Sets:           []*clusterv1.MachineSet{&ms1, &ms2},
			ExpectedCount:  6,
			ExpectedActual: 5,
			ExpectedTotal:  7,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			g := NewWithT(t)

			g.Expect(GetReplicaCountForMachineSets(test.Sets)).To(Equal(test.ExpectedCount))
			g.Expect(GetActualReplicaCountForMachineSets(test.Sets)).To(Equal(test.ExpectedActual))
			g.Expect(TotalMachineSetsReplicaSum(test.Sets)).To(Equal(test.ExpectedTotal))
		})
	}
}

func TestResolveFenceposts(t *testing.T) {
	tests := []struct {
		maxSurge          string
		maxUnavailable    string
		desired           int32
		expectSurge       int32
		expectUnavailable int32
		expectError       bool
	}{
		{
			maxSurge:          "0%",
			maxUnavailable:    "0%",
			desired:           0,
			expectSurge:       0,
			expectUnavailable: 1,
			expectError:       false,
		},
		{
			maxSurge:          "39%",
			maxUnavailable:    "39%",
			desired:           10,
			expectSurge:       4,
			expectUnavailable: 3,
			expectError:       false,
		},
		{
			maxSurge:          "oops",
			maxUnavailable:    "39%",
			desired:           10,
			expectSurge:       0,
			expectUnavailable: 0,
			expectError:       true,
		},
		{
			maxSurge:          "55%",
			maxUnavailable:    "urg",
			desired:           10,
			expectSurge:       0,
			expectUnavailable: 0,
			expectError:       true,
		},
		{
			maxSurge:          "5",
			maxUnavailable:    "1",
			desired:           7,
			expectSurge:       0,
			expectUnavailable: 0,
			expectError:       true,
		},
	}

	for _, test := range tests {
		t.Run("maxSurge="+test.maxSurge, func(t *testing.T) {
			g := NewWithT(t)

			maxSurge := intstr.FromString(test.maxSurge)
			maxUnavail := intstr.FromString(test.maxUnavailable)
			surge, unavail, err := ResolveFenceposts(&maxSurge, &maxUnavail, test.desired)
			if test.expectError {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
			g.Expect(surge).To(Equal(test.expectSurge))
			g.Expect(unavail).To(Equal(test.expectUnavailable))
		})
	}
}

func TestNewMSNewReplicas(t *testing.T) {
	tests := []struct {
		Name          string
		strategyType  clusterv1.MachineDeploymentStrategyType
		depReplicas   int32
		newMSReplicas int32
		maxSurge      int
		expected      int32
	}{
		{
			"can not scale up - to newMSReplicas",
			clusterv1.RollingUpdateMachineDeploymentStrategyType,
			1, 5, 1, 5,
		},
		{
			"scale up - to depReplicas",
			clusterv1.RollingUpdateMachineDeploymentStrategyType,
			6, 2, 10, 6,
		},
	}
	newDeployment := generateDeployment("nginx")
	newRC := generateMS(newDeployment)
	rs5 := generateMS(newDeployment)
	*(rs5.Spec.Replicas) = 5

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			g := NewWithT(t)

			*(newDeployment.Spec.Replicas) = test.depReplicas
			newDeployment.Spec.Strategy = &clusterv1.MachineDeploymentStrategy{Type: test.strategyType}
			newDeployment.Spec.Strategy.RollingUpdate = &clusterv1.MachineRollingUpdateDeployment{
				MaxUnavailable: func(i int) *intstr.IntOrString {
					x := intstr.FromInt(i)
					return &x
				}(1),
				MaxSurge: func(i int) *intstr.IntOrString {
					x := intstr.FromInt(i)
					return &x
				}(test.maxSurge),
			}
			*(newRC.Spec.Replicas) = test.newMSReplicas
			ms, err := NewMSNewReplicas(&newDeployment, []*clusterv1.MachineSet{&rs5}, *newRC.Spec.Replicas)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(ms).To(Equal(test.expected))
		})
	}
}

func TestDeploymentComplete(t *testing.T) {
	deployment := func(desired, current, updated, available, maxUnavailable, maxSurge int32) *clusterv1.MachineDeployment {
		return &clusterv1.MachineDeployment{
			Spec: clusterv1.MachineDeploymentSpec{
				Replicas: &desired,
				Strategy: &clusterv1.MachineDeploymentStrategy{
					RollingUpdate: &clusterv1.MachineRollingUpdateDeployment{
						MaxUnavailable: func(i int) *intstr.IntOrString { x := intstr.FromInt(i); return &x }(int(maxUnavailable)),
						MaxSurge:       func(i int) *intstr.IntOrString { x := intstr.FromInt(i); return &x }(int(maxSurge)),
					},
					Type: clusterv1.RollingUpdateMachineDeploymentStrategyType,
				},
			},
			Status: clusterv1.MachineDeploymentStatus{
				Replicas:          current,
				UpdatedReplicas:   updated,
				AvailableReplicas: available,
			},
		}
	}

	tests := []struct {
		name string

		md *clusterv1.MachineDeployment

		expected bool
	}{
		{
			name: "not complete: min but not all machines become available",

			md:       deployment(5, 5, 5, 4, 1, 0),
			expected: false,
		},
		{
			name: "not complete: min availability is not honored",

			md:       deployment(5, 5, 5, 3, 1, 0),
			expected: false,
		},
		{
			name: "complete",

			md:       deployment(5, 5, 5, 5, 0, 0),
			expected: true,
		},
		{
			name: "not complete: all machines are available but not updated",

			md:       deployment(5, 5, 4, 5, 0, 0),
			expected: false,
		},
		{
			name: "not complete: still running old machines",

			// old machine set: spec.replicas=1, status.replicas=1, status.availableReplicas=1
			// new machine set: spec.replicas=1, status.replicas=1, status.availableReplicas=0
			md:       deployment(1, 2, 1, 1, 0, 1),
			expected: false,
		},
		{
			name: "not complete: one replica deployment never comes up",

			md:       deployment(1, 1, 1, 0, 1, 1),
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			g.Expect(DeploymentComplete(test.md, &test.md.Status)).To(Equal(test.expected))
		})
	}
}

func TestMaxUnavailable(t *testing.T) {
	deployment := func(replicas int32, maxUnavailable intstr.IntOrString) clusterv1.MachineDeployment {
		return clusterv1.MachineDeployment{
			Spec: clusterv1.MachineDeploymentSpec{
				Replicas: func(i int32) *int32 { return &i }(replicas),
				Strategy: &clusterv1.MachineDeploymentStrategy{
					RollingUpdate: &clusterv1.MachineRollingUpdateDeployment{
						MaxSurge:       func(i int) *intstr.IntOrString { x := intstr.FromInt(i); return &x }(int(1)),
						MaxUnavailable: &maxUnavailable,
					},
					Type: clusterv1.RollingUpdateMachineDeploymentStrategyType,
				},
			},
		}
	}
	tests := []struct {
		name       string
		deployment clusterv1.MachineDeployment
		expected   int32
	}{
		{
			name:       "maxUnavailable less than replicas",
			deployment: deployment(10, intstr.FromInt(5)),
			expected:   int32(5),
		},
		{
			name:       "maxUnavailable equal replicas",
			deployment: deployment(10, intstr.FromInt(10)),
			expected:   int32(10),
		},
		{
			name:       "maxUnavailable greater than replicas",
			deployment: deployment(5, intstr.FromInt(10)),
			expected:   int32(5),
		},
		{
			name:       "maxUnavailable with replicas is 0",
			deployment: deployment(0, intstr.FromInt(10)),
			expected:   int32(0),
		},
		{
			name:       "maxUnavailable less than replicas with percents",
			deployment: deployment(10, intstr.FromString("50%")),
			expected:   int32(5),
		},
		{
			name:       "maxUnavailable equal replicas with percents",
			deployment: deployment(10, intstr.FromString("100%")),
			expected:   int32(10),
		},
		{
			name:       "maxUnavailable greater than replicas with percents",
			deployment: deployment(5, intstr.FromString("100%")),
			expected:   int32(5),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			g.Expect(MaxUnavailable(test.deployment)).To(Equal(test.expected))
		})
	}
}

// TestAnnotationUtils is a set of simple tests for annotation related util functions.
func TestAnnotationUtils(t *testing.T) {
	// Setup
	tDeployment := generateDeployment("nginx")
	tDeployment.Spec.Replicas = pointer.Int32(1)
	tMS := generateMS(tDeployment)

	// Test Case 1:  Check if annotations are set properly
	t.Run("SetReplicasAnnotations", func(t *testing.T) {
		g := NewWithT(t)

		g.Expect(SetReplicasAnnotations(&tMS, 10, 11)).To(BeTrue())
		g.Expect(tMS.Annotations).To(HaveKeyWithValue(clusterv1.DesiredReplicasAnnotation, "10"))
		g.Expect(tMS.Annotations).To(HaveKeyWithValue(clusterv1.MaxReplicasAnnotation, "11"))
	})

	// Test Case 2:  Check if annotations reflect deployments state
	tMS.Annotations[clusterv1.DesiredReplicasAnnotation] = "1"
	tMS.Status.AvailableReplicas = 1
	tMS.Spec.Replicas = new(int32)
	*tMS.Spec.Replicas = 1

	t.Run("IsSaturated", func(t *testing.T) {
		g := NewWithT(t)

		g.Expect(IsSaturated(&tDeployment, &tMS)).To(BeTrue())
	})
}

func TestComputeMachineSetAnnotations(t *testing.T) {
	deployment := generateDeployment("nginx")
	deployment.Spec.Replicas = pointer.Int32(3)
	maxSurge := intstr.FromInt(1)
	maxUnavailable := intstr.FromInt(0)
	deployment.Spec.Strategy = &clusterv1.MachineDeploymentStrategy{
		Type: clusterv1.RollingUpdateMachineDeploymentStrategyType,
		RollingUpdate: &clusterv1.MachineRollingUpdateDeployment{
			MaxSurge:       &maxSurge,
			MaxUnavailable: &maxUnavailable,
		},
	}
	deployment.Annotations = map[string]string{
		corev1.LastAppliedConfigAnnotation: "last-applied-configuration",
		"key1":                             "value1",
	}

	tests := []struct {
		name       string
		deployment *clusterv1.MachineDeployment
		oldMSs     []*clusterv1.MachineSet
		ms         *clusterv1.MachineSet
		want       map[string]string
		wantErr    bool
	}{
		{
			name:       "Calculating annotations for a new MachineSet",
			deployment: &deployment,
			oldMSs:     nil,
			ms:         nil,
			want: map[string]string{
				"key1":                              "value1",
				clusterv1.RevisionAnnotation:        "1",
				clusterv1.DesiredReplicasAnnotation: "3",
				clusterv1.MaxReplicasAnnotation:     "4",
			},
			wantErr: false,
		},
		{
			name:       "Calculating annotations for a new MachineSet - old MSs exist",
			deployment: &deployment,
			oldMSs:     []*clusterv1.MachineSet{machineSetWithRevisionAndHistory("1", "")},
			ms:         nil,
			want: map[string]string{
				"key1":                              "value1",
				clusterv1.RevisionAnnotation:        "2",
				clusterv1.DesiredReplicasAnnotation: "3",
				clusterv1.MaxReplicasAnnotation:     "4",
			},
			wantErr: false,
		},
		{
			name:       "Calculating annotations for a existing MachineSet",
			deployment: &deployment,
			oldMSs:     nil,
			ms:         machineSetWithRevisionAndHistory("1", ""),
			want: map[string]string{
				"key1":                              "value1",
				clusterv1.RevisionAnnotation:        "1",
				clusterv1.DesiredReplicasAnnotation: "3",
				clusterv1.MaxReplicasAnnotation:     "4",
			},
			wantErr: false,
		},
		{
			name:       "Calculating annotations for a existing MachineSet - old MSs exist",
			deployment: &deployment,
			oldMSs: []*clusterv1.MachineSet{
				machineSetWithRevisionAndHistory("1", ""),
				machineSetWithRevisionAndHistory("2", ""),
			},
			ms: machineSetWithRevisionAndHistory("1", ""),
			want: map[string]string{
				"key1":                              "value1",
				clusterv1.RevisionAnnotation:        "3",
				clusterv1.RevisionHistoryAnnotation: "1",
				clusterv1.DesiredReplicasAnnotation: "3",
				clusterv1.MaxReplicasAnnotation:     "4",
			},
			wantErr: false,
		},
		{
			name:       "Calculating annotations for a existing MachineSet - old MSs exist - existing revision is greater",
			deployment: &deployment,
			oldMSs: []*clusterv1.MachineSet{
				machineSetWithRevisionAndHistory("1", ""),
				machineSetWithRevisionAndHistory("2", ""),
			},
			ms: machineSetWithRevisionAndHistory("4", ""),
			want: map[string]string{
				"key1":                              "value1",
				clusterv1.RevisionAnnotation:        "4",
				clusterv1.DesiredReplicasAnnotation: "3",
				clusterv1.MaxReplicasAnnotation:     "4",
			},
			wantErr: false,
		},
		{
			name:       "Calculating annotations for a existing MachineSet - old MSs exist - ms already has revision history",
			deployment: &deployment,
			oldMSs: []*clusterv1.MachineSet{
				machineSetWithRevisionAndHistory("3", ""),
				machineSetWithRevisionAndHistory("4", ""),
			},
			ms: machineSetWithRevisionAndHistory("2", "1"),
			want: map[string]string{
				"key1":                              "value1",
				clusterv1.RevisionAnnotation:        "5",
				clusterv1.RevisionHistoryAnnotation: "1,2",
				clusterv1.DesiredReplicasAnnotation: "3",
				clusterv1.MaxReplicasAnnotation:     "4",
			},
			wantErr: false,
		},
	}

	log := klogr.New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			got, err := ComputeMachineSetAnnotations(log, tt.deployment, tt.oldMSs, tt.ms)
			if tt.wantErr {
				g.Expect(err).ShouldNot(BeNil())
			} else {
				g.Expect(err).Should(BeNil())
				g.Expect(got).Should(Equal(tt.want))
			}
		})
	}
}

func machineSetWithRevisionAndHistory(revision string, revisionHistory string) *clusterv1.MachineSet {
	ms := &clusterv1.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				clusterv1.RevisionAnnotation: revision,
			},
		},
	}
	if revisionHistory != "" {
		ms.Annotations[clusterv1.RevisionHistoryAnnotation] = revisionHistory
	}
	return ms
}

func TestReplicasAnnotationsNeedUpdate(t *testing.T) {
	desiredReplicas := fmt.Sprintf("%d", int32(10))
	maxReplicas := fmt.Sprintf("%d", int32(20))

	tests := []struct {
		name       string
		machineSet *clusterv1.MachineSet
		expected   bool
	}{
		{
			name: "test Annotations nil",
			machineSet: &clusterv1.MachineSet{
				ObjectMeta: metav1.ObjectMeta{Name: "hello", Namespace: metav1.NamespaceDefault},
				Spec: clusterv1.MachineSetSpec{
					Selector: metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
				},
			},
			expected: true,
		},
		{
			name: "test desiredReplicas update",
			machineSet: &clusterv1.MachineSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "hello",
					Namespace:   metav1.NamespaceDefault,
					Annotations: map[string]string{clusterv1.DesiredReplicasAnnotation: "8", clusterv1.MaxReplicasAnnotation: maxReplicas},
				},
				Spec: clusterv1.MachineSetSpec{
					Selector: metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
				},
			},
			expected: true,
		},
		{
			name: "test maxReplicas update",
			machineSet: &clusterv1.MachineSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "hello",
					Namespace:   metav1.NamespaceDefault,
					Annotations: map[string]string{clusterv1.DesiredReplicasAnnotation: desiredReplicas, clusterv1.MaxReplicasAnnotation: "16"},
				},
				Spec: clusterv1.MachineSetSpec{
					Selector: metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
				},
			},
			expected: true,
		},
		{
			name: "test needn't update",
			machineSet: &clusterv1.MachineSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "hello",
					Namespace:   metav1.NamespaceDefault,
					Annotations: map[string]string{clusterv1.DesiredReplicasAnnotation: desiredReplicas, clusterv1.MaxReplicasAnnotation: maxReplicas},
				},
				Spec: clusterv1.MachineSetSpec{
					Selector: metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
				},
			},
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			g.Expect(ReplicasAnnotationsNeedUpdate(test.machineSet, 10, 20)).To(Equal(test.expected))
		})
	}
}
