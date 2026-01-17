// Copyright 2017 The etcd-operator Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package k8sutil

import (
	_ "embed"
	"fmt"
	"testing"

	api "github.com/coreos/etcd-operator/pkg/apis/etcd/v1beta2"
	"github.com/coreos/etcd-operator/pkg/util/etcdutil"
	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	yaml "sigs.k8s.io/yaml"
)

func TestDefaultBusyboxImageName(t *testing.T) {
	policy := &api.PodPolicy{}
	image := imageNameBusybox(policy)
	expected := defaultBusyboxImage
	if image != expected {
		t.Errorf("expect image=%s, get=%s", expected, image)
	}
}

func TestDefaultNilBusyboxImageName(t *testing.T) {
	image := imageNameBusybox(nil)
	expected := defaultBusyboxImage
	if image != expected {
		t.Errorf("expect image=%s, get=%s", expected, image)
	}
}

func TestSetBusyboxImageName(t *testing.T) {
	policy := &api.PodPolicy{
		BusyboxImage: "myRepo/busybox:1.3.2",
	}
	image := imageNameBusybox(policy)
	expected := "myRepo/busybox:1.3.2"
	if image != expected {
		t.Errorf("expect image=%s, get=%s", expected, image)
	}
}

var (
	//go:embed testdata/pod_basic.yaml
	podBasicYAML []byte

	//go:embed testdata/pod_peer_tls.yaml
	podPeerTLSYAML []byte

	//go:embed testdata/pod_client_tls.yaml
	podClientTLSYAML []byte

	//go:embed testdata/pod_both_tls.yaml
	podBothTLSYAML []byte

	//go:embed testdata/pod_custom_busybox.yaml
	podCustomBusyboxYAML []byte

	//go:embed testdata/pod_security_context.yaml
	podSecurityContextYAML []byte
)

// TestMember is a wrapper for etcdutil.Member with URL methods for testing
type TestMember struct {
	etcdutil.Member
	ClusterName string
}

func (m *TestMember) PeerURL() string {
	scheme := "http"
	if m.SecurePeer {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s.%s.svc:2380", scheme, m.Name, m.ClusterName)
}

func (m *TestMember) ListenPeerURL() string {
	scheme := "http"
	if m.SecurePeer {
		scheme = "https"
	}
	return fmt.Sprintf("%s://0.0.0.0:2380", scheme)
}

func (m *TestMember) ListenClientURL() string {
	scheme := "http"
	if m.SecureClient {
		scheme = "https"
	}
	return fmt.Sprintf("%s://0.0.0.0:2379", scheme)
}

func (m *TestMember) ClientURL() string {
	scheme := "http"
	if m.SecureClient {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s.%s.svc:2379", scheme, m.Name, m.ClusterName)
}

func (m *TestMember) Addr() string {
	return fmt.Sprintf("%s.%s.svc", m.Name, m.ClusterName)
}

func loadExpectedPod(yamlData []byte) *v1.Pod {
	pod := &v1.Pod{}
	if err := yaml.Unmarshal(yamlData, pod); err != nil {
		panic(fmt.Sprintf("Failed to unmarshal YAML: %v", err))
	}
	return pod
}

func TestNewEtcdPod_Basic(t *testing.T) {
	expected := loadExpectedPod(podBasicYAML)

	member := &TestMember{
		Member: etcdutil.Member{
			Namespace:    "default",
			Name:         "test-etcd-0",
			SecurePeer:   false,
			SecureClient: false,
		},
		ClusterName: "test-etcd",
	}

	cs := api.ClusterSpec{
		Repository: "quay.io/coreos/etcd",
		Version:    "3.3.10",
	}

	// Use the TestMember as a regular *etcdutil.Member
	var m *etcdutil.Member = &member.Member
	pod, err := newEtcdPod(m, []string{"test-etcd-0=http://test-etcd-0.test-etcd.default.svc:2380"},
		"test-etcd", "new", "test-token", cs)
	if err != nil {
		t.Fatal(err)
	}

	if !apiequality.Semantic.DeepEqual(expected, pod) {
		expectedStr, _ := yaml.Marshal(expected.Spec.Containers)
		actualStr, _ := yaml.Marshal(pod.Spec.Containers)
		if diff := cmp.Diff(expectedStr, actualStr); diff != "" {
			t.Errorf("Summary diff:\n%s", diff)
		}
	}
}

func TestNewEtcdPod_PeerTLS(t *testing.T) {
	expected := loadExpectedPod(podPeerTLSYAML)

	member := &TestMember{
		Member: etcdutil.Member{
			Name:         "test-etcd-0",
			Namespace:    "default",
			SecurePeer:   true,
			SecureClient: false,
		},
		ClusterName: "test-etcd",
	}

	cs := api.ClusterSpec{
		Repository: "quay.io/coreos/etcd",
		Version:    "3.3.10",
		TLS: &api.TLSPolicy{
			Static: &api.StaticTLS{
				Member: &api.MemberSecret{
					PeerSecret:   "peer-secret",
					ServerSecret: "",
				},
			},
		},
	}

	var m *etcdutil.Member = &member.Member
	pod, err := newEtcdPod(m, []string{"test-etcd-0=https://test-etcd-0.test-etcd.default.svc:2380"},
		"test-etcd", "existing", "", cs)
	if err != nil {
		t.Fatal(err)
	}

	if !apiequality.Semantic.DeepEqual(expected, pod) {
		expectedStr, _ := yaml.Marshal(expected.Spec.Containers)
		actualStr, _ := yaml.Marshal(pod.Spec.Containers)
		if diff := cmp.Diff(expectedStr, actualStr); diff != "" {
			t.Errorf("Summary diff:\n%s", diff)
		}
	}
}

func TestNewEtcdPod_ClientTLS(t *testing.T) {
	expected := loadExpectedPod(podClientTLSYAML)

	member := &TestMember{
		Member: etcdutil.Member{
			Name:         "test-etcd-0",
			Namespace:    "default",
			SecurePeer:   false,
			SecureClient: true,
		},
		ClusterName: "test-etcd",
	}

	cs := api.ClusterSpec{
		Repository: "quay.io/coreos/etcd",
		Version:    "3.3.10",
		TLS: &api.TLSPolicy{
			Static: &api.StaticTLS{
				Member: &api.MemberSecret{
					PeerSecret:   "",
					ServerSecret: "server-secret",
				},
				OperatorSecret: "operator-secret",
			},
		},
	}

	var m *etcdutil.Member = &member.Member
	pod, err := newEtcdPod(m, []string{"test-etcd-0=http://test-etcd-0.test-etcd.default.svc:2380"},
		"test-etcd", "new", "test-token", cs)
	if err != nil {
		t.Fatal(err)
	}

	if !apiequality.Semantic.DeepEqual(expected, pod) {
		expectedStr, _ := yaml.Marshal(expected.Spec.Containers)
		actualStr, _ := yaml.Marshal(pod.Spec.Containers)
		if diff := cmp.Diff(expectedStr, actualStr); diff != "" {
			t.Errorf("Summary diff:\n%s", diff)
		}
	}
}

func TestNewEtcdPod_BothTLS(t *testing.T) {
	expected := loadExpectedPod(podBothTLSYAML)

	member := &TestMember{
		Member: etcdutil.Member{
			Name:         "test-etcd-0",
			Namespace:    "default",
			SecurePeer:   true,
			SecureClient: true,
		},
		ClusterName: "test-etcd",
	}

	cs := api.ClusterSpec{
		Repository: "quay.io/coreos/etcd",
		Version:    "3.3.10",
		TLS: &api.TLSPolicy{
			Static: &api.StaticTLS{
				Member: &api.MemberSecret{
					PeerSecret:   "peer-secret",
					ServerSecret: "server-secret",
				},
				OperatorSecret: "operator-secret",
			},
		},
		Pod: &api.PodPolicy{
			DNSTimeoutInSecond: 30,
		},
	}

	var m *etcdutil.Member = &member.Member
	pod, err := newEtcdPod(m, []string{"test-etcd-0=https://test-etcd-0.test-etcd.default.svc:2380"},
		"test-etcd", "new", "test-token", cs)
	if err != nil {
		t.Fatal(err)
	}

	if !apiequality.Semantic.DeepEqual(expected, pod) {
		expectedStr, _ := yaml.Marshal(expected.Spec.Containers)
		actualStr, _ := yaml.Marshal(pod.Spec.Containers)
		if diff := cmp.Diff(expectedStr, actualStr); diff != "" {
			t.Errorf("Summary diff:\n%s", diff)
		}
	}
}

func TestNewEtcdPod_CustomBusybox(t *testing.T) {
	expected := loadExpectedPod(podCustomBusyboxYAML)

	member := &TestMember{
		Member: etcdutil.Member{
			Name:         "test-etcd-0",
			Namespace:    "default",
			SecurePeer:   false,
			SecureClient: false,
		},
		ClusterName: "test-etcd",
	}

	cs := api.ClusterSpec{
		Repository: "quay.io/coreos/etcd",
		Version:    "3.3.10",
		Pod: &api.PodPolicy{
			BusyboxImage: "custom/busybox:latest",
		},
	}

	var m *etcdutil.Member = &member.Member
	pod, err := newEtcdPod(m, []string{"test-etcd-0=http://test-etcd-0.test-etcd.default.svc:2380"},
		"test-etcd", "new", "test-token", cs)
	if err != nil {
		t.Fatal(err)
	}

	if !apiequality.Semantic.DeepEqual(expected, pod) {
		expectedStr, _ := yaml.Marshal(expected.Spec.Containers)
		actualStr, _ := yaml.Marshal(pod.Spec.Containers)
		if diff := cmp.Diff(expectedStr, actualStr); diff != "" {
			t.Errorf("Summary diff:\n%s", diff)
		}
	}
}

func TestNewEtcdPod_SecurityContext(t *testing.T) {
	expected := loadExpectedPod(podSecurityContextYAML)

	member := &TestMember{
		Member: etcdutil.Member{
			Name:         "test-etcd-0",
			Namespace:    "default",
			SecurePeer:   false,
			SecureClient: false,
		},
		ClusterName: "test-etcd",
	}

	cs := api.ClusterSpec{
		Repository: "quay.io/coreos/etcd",
		Version:    "3.3.10",
		Pod: &api.PodPolicy{
			SecurityContext: &v1.PodSecurityContext{
				RunAsUser:  func(i int64) *int64 { return &i }(1000),
				RunAsGroup: func(i int64) *int64 { return &i }(1000),
			},
		},
	}

	var m *etcdutil.Member = &member.Member
	pod, err := newEtcdPod(m, []string{"test-etcd-0=http://test-etcd-0.test-etcd.default.svc:2380"},
		"test-etcd", "existing", "", cs)
	if err != nil {
		t.Fatal(err)
	}

	if !apiequality.Semantic.DeepEqual(expected, pod) {
		expectedStr, _ := yaml.Marshal(expected.Spec.Containers)
		actualStr, _ := yaml.Marshal(pod.Spec.Containers)
		if diff := cmp.Diff(expectedStr, actualStr); diff != "" {
			t.Errorf("Summary diff:\n%s", diff)
		}
	}
}

func TestMaxProcsMutator_NoResources(t *testing.T) {
	mutator := &maxProcsMutator{
		cs: api.ClusterSpec{
			Pod: &api.PodPolicy{
				Resources: v1.ResourceRequirements{},
			},
		},
	}

	pod := &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: etcdContainerName,
					Env: []v1.EnvVar{
						{Name: "OTHER_ENV", Value: "value"},
					},
				},
			},
		},
	}

	// Should not mutate when no CPU resources defined
	err := mutator.Mutate(pod)
	if err != nil {
		t.Fatalf("Mutate should not return error for no resources: %v", err)
	}

	// Verify no GOMAXPROCS was added
	container := pod.Spec.Containers[0]
	for _, env := range container.Env {
		if env.Name == "GOMAXPROCS" {
			t.Error("GOMAXPROCS should not be added when no CPU resources defined")
		}
	}
}

func TestMaxProcsMutator_OnlyRequestsIntCPU(t *testing.T) {
	mutator := &maxProcsMutator{
		cs: api.ClusterSpec{
			Pod: &api.PodPolicy{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceCPU: resource.MustParse("2"),
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: etcdContainerName,
					Env:  []v1.EnvVar{},
				},
			},
		},
	}

	err := mutator.Mutate(pod)
	if err != nil {
		t.Fatalf("Mutate failed: %v", err)
	}

	// Verify GOMAXPROCS was added with value 2
	container := pod.Spec.Containers[0]
	found := false
	for _, env := range container.Env {
		if env.Name == "GOMAXPROCS" && env.Value == "2" {
			found = true
			break
		}
	}
	if !found {
		t.Error("GOMAXPROCS=2 should be added when CPU request is 2")
	}
}

func TestMaxProcsMutator_OnlyLimitsIntCPU(t *testing.T) {
	mutator := &maxProcsMutator{
		cs: api.ClusterSpec{
			Pod: &api.PodPolicy{
				Resources: v1.ResourceRequirements{
					Limits: v1.ResourceList{
						v1.ResourceCPU: resource.MustParse("4"),
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: etcdContainerName,
					Env: []v1.EnvVar{
						{Name: "EXISTING_ENV", Value: "value"},
					},
				},
			},
		},
	}

	err := mutator.Mutate(pod)
	if err != nil {
		t.Fatalf("Mutate failed: %v", err)
	}

	// Verify GOMAXPROCS was added with value 4
	container := pod.Spec.Containers[0]
	found := false
	for _, env := range container.Env {
		if env.Name == "GOMAXPROCS" && env.Value == "4" {
			found = true
			break
		}
	}
	if !found {
		t.Error("GOMAXPROCS=4 should be added when CPU limit is 4")
	}
}

func TestMaxProcsMutator_RequestsAndLimitsIntCPU_PrefersLimits(t *testing.T) {
	mutator := &maxProcsMutator{
		cs: api.ClusterSpec{
			Pod: &api.PodPolicy{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceCPU: resource.MustParse("2"),
					},
					Limits: v1.ResourceList{
						v1.ResourceCPU: resource.MustParse("8"),
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: etcdContainerName,
					Env:  []v1.EnvVar{},
				},
			},
		},
	}

	err := mutator.Mutate(pod)
	if err != nil {
		t.Fatalf("Mutate failed: %v", err)
	}

	// Verify GOMAXPROCS uses limits (8) not requests (2)
	container := pod.Spec.Containers[0]
	found := false
	for _, env := range container.Env {
		if env.Name == "GOMAXPROCS" {
			if env.Value == "8" {
				found = true
			} else {
				t.Errorf("GOMAXPROCS should be 8 (from limits), got %s", env.Value)
			}
			break
		}
	}
	if !found {
		t.Error("GOMAXPROCS should be added when CPU resources defined")
	}
}

func TestMaxProcsMutator_RequestsFloatCPU(t *testing.T) {
	testCases := []struct {
		name        string
		cpuString   string
		expected    string
		description string
	}{
		{
			name:        "500m CPU",
			cpuString:   "500m",
			expected:    "1", // 500m ceils to 1
			description: "500 milliCPU should ceil to 1",
		},
		{
			name:        "1500m CPU",
			cpuString:   "1500m",
			expected:    "2", // 1500m ceils to 2
			description: "1.5 CPU should ceil to 2",
		},
		{
			name:        "800m CPU",
			cpuString:   "800m",
			expected:    "1", // 800m ceils to 1
			description: "800 milliCPU should ceil to 1",
		},
		{
			name:        "2300m CPU",
			cpuString:   "2300m",
			expected:    "3", // 2300m ceils to 3
			description: "3 CPU should ceil to 2",
		},
		{
			name:        "0.5 CPU",
			cpuString:   "0.5",
			expected:    "1", // 0.5 ceils to 1
			description: "0.5 CPU should ceil to 1",
		},
		{
			name:        "1.2 CPU",
			cpuString:   "1.2",
			expected:    "2", // 1.2 ceils to 2
			description: "1.2 CPU should ceil to 2",
		},
		{
			name:        "1.7 CPU",
			cpuString:   "1.7",
			expected:    "2", // 1.7 ceils to 2
			description: "1.7 CPU should ceil to 2",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mutator := &maxProcsMutator{
				cs: api.ClusterSpec{
					Pod: &api.PodPolicy{
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU: resource.MustParse(tc.cpuString),
							},
						},
					},
				},
			}

			pod := &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: etcdContainerName,
							Env:  []v1.EnvVar{},
						},
					},
				},
			}

			err := mutator.Mutate(pod)
			if err != nil {
				t.Fatalf("Mutate failed for %s: %v", tc.description, err)
			}

			container := pod.Spec.Containers[0]
			found := false
			for _, env := range container.Env {
				if env.Name == "GOMAXPROCS" {
					if env.Value != tc.expected {
						t.Errorf("%s: expected GOMAXPROCS=%s, got %s", tc.description, tc.expected, env.Value)
					}
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: GOMAXPROCS should be added", tc.description)
			}
		})
	}
}

func TestMaxProcsMutator_RequestsAndLimitsFloatCPU_PrefersLimits(t *testing.T) {
	mutator := &maxProcsMutator{
		cs: api.ClusterSpec{
			Pod: &api.PodPolicy{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceCPU: resource.MustParse("1.2"),
					},
					Limits: v1.ResourceList{
						v1.ResourceCPU: resource.MustParse("3.7"),
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: etcdContainerName,
					Env:  []v1.EnvVar{},
				},
			},
		},
	}

	err := mutator.Mutate(pod)
	if err != nil {
		t.Fatalf("Mutate failed: %v", err)
	}

	// Should use limits (3.7 -> ceils to 4) not requests (1.2 -> ceils to 1)
	container := pod.Spec.Containers[0]
	found := false
	for _, env := range container.Env {
		if env.Name == "GOMAXPROCS" && env.Value == "4" {
			found = true
			break
		}
	}
	if !found {
		t.Error("GOMAXPROCS=4 should be added (3.7 CPU limit ceils to 4)")
	}
}

func TestMaxProcsMutator_UpdatesExistingGOMAXPROCS(t *testing.T) {
	mutator := &maxProcsMutator{
		cs: api.ClusterSpec{
			Pod: &api.PodPolicy{
				Resources: v1.ResourceRequirements{
					Limits: v1.ResourceList{
						v1.ResourceCPU: resource.MustParse("4"),
					},
				},
			},
		},
	}

	pod := &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: etcdContainerName,
					Env: []v1.EnvVar{
						{Name: "GOMAXPROCS", Value: "1"}, // Existing value
						{Name: "OTHER_ENV", Value: "test"},
					},
				},
			},
		},
	}

	err := mutator.Mutate(pod)
	if err != nil {
		t.Fatalf("Mutate failed: %v", err)
	}

	// Verify GOMAXPROCS was updated from 1 to 4
	container := pod.Spec.Containers[0]
	gomaxprocsFound := false
	otherEnvFound := false
	for _, env := range container.Env {
		if env.Name == "GOMAXPROCS" {
			if env.Value != "4" {
				t.Errorf("GOMAXPROCS should be updated to 4, got %s", env.Value)
			}
			gomaxprocsFound = true
		}
		if env.Name == "OTHER_ENV" && env.Value == "test" {
			otherEnvFound = true
		}
	}
	if !gomaxprocsFound {
		t.Error("GOMAXPROCS should exist after mutation")
	}
	if !otherEnvFound {
		t.Error("OTHER_ENV should remain unchanged")
	}
}

func TestMaxProcsMutator_ZeroCPU(t *testing.T) {
	testCases := []struct {
		name     string
		requests string
		limits   string
	}{
		{
			name:     "zero requests",
			requests: "0",
			limits:   "",
		},
		{
			name:     "zero limits",
			requests: "",
			limits:   "0",
		},
		{
			name:     "zero both",
			requests: "0",
			limits:   "0",
		},
		{
			name:     "negative requests",
			requests: "-1",
			limits:   "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resources := v1.ResourceRequirements{}

			if tc.requests != "" {
				resources.Requests = v1.ResourceList{
					v1.ResourceCPU: resource.MustParse(tc.requests),
				}
			}
			if tc.limits != "" {
				resources.Limits = v1.ResourceList{
					v1.ResourceCPU: resource.MustParse(tc.limits),
				}
			}

			mutator := &maxProcsMutator{
				cs: api.ClusterSpec{
					Pod: &api.PodPolicy{
						Resources: resources,
					},
				},
			}

			pod := &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: etcdContainerName,
							Env:  []v1.EnvVar{},
						},
					},
				},
			}

			err := mutator.Mutate(pod)
			if err != nil {
				t.Fatalf("Mutate should not fail for zero CPU: %v", err)
			}

			// Should not add GOMAXPROCS for zero/negative CPU
			container := pod.Spec.Containers[0]
			for _, env := range container.Env {
				if env.Name == "GOMAXPROCS" {
					t.Errorf("GOMAXPROCS should not be added for zero/negative CPU in case %s", tc.name)
				}
			}
		})
	}
}

func TestSoftMemLimitMutator_PrivateFormatFunction(t *testing.T) {
	// Create a test instance to access the private method
	mutator := &softMemLimitMutator{}

	testCases := []struct {
		bytes    int64
		expected string
	}{
		{1073741824, "1GiB"},                // Exactly 1GiB
		{2147483648, "2GiB"},                // Exactly 2GiB
		{536870912, "512MiB"},               // Exactly 512MiB
		{104857600, "100MiB"},               // Exactly 100MiB
		{1073741824 - 1, "1024MiB"},         // Just under 1GiB -> 1024MiB
		{1073741824 + 536870912, "1536MiB"}, // 1.5GiB -> 1536MiB
		{1024 * 1024, "1MiB"},               // Exactly 1MiB
		{1024*1024 - 1, "1MiB"},             // Just under 1MiB -> rounds to 1MiB
		{1024*1024 + 512*1024, "2MiB"},      // 1.5MiB -> rounds to 2MiB
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%d bytes", tc.bytes), func(t *testing.T) {
			actual := mutator.formatGOMEMLIMIT(tc.bytes)
			if actual != tc.expected {
				t.Errorf("formatGOMEMLIMIT(%d) = %s, expected %s",
					tc.bytes, actual, tc.expected)
			}
		})
	}
}

// Alternative: Test through the public Mutate method
func TestSoftMemLimitMutator_FormatThroughMutate(t *testing.T) {
	testCases := []struct {
		memory   string
		expected string
	}{
		{"1Gi", "922MiB"},   // 90% of 1GiB = ~922MiB
		{"2Gi", "1843MiB"},  // 90% of 2GiB = ~1843MiB
		{"4Gi", "3686MiB"},  // 90% of 4GiB = ~3686MiB
		{"100Mi", "90MiB"},  // 90% of 100MiB = 90MiB
		{"512Mi", "461MiB"}, // 90% of 512MiB = ~461MiB
	}

	for _, tc := range testCases {
		t.Run(tc.memory, func(t *testing.T) {
			mutator := &softMemLimitMutator{
				cs: api.ClusterSpec{
					Pod: &api.PodPolicy{
						Resources: v1.ResourceRequirements{
							Limits: v1.ResourceList{
								v1.ResourceMemory: resource.MustParse(tc.memory),
							},
						},
					},
				},
			}

			pod := &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: etcdContainerName,
							Env:  []v1.EnvVar{},
						},
					},
				},
			}

			err := mutator.Mutate(pod)
			if err != nil {
				t.Fatalf("Mutate failed: %v", err)
			}

			container := pod.Spec.Containers[0]
			for _, env := range container.Env {
				if env.Name == "GOMEMLIMIT" {
					if env.Value != tc.expected {
						t.Errorf("For memory %s: expected GOMEMLIMIT=%s, got %s",
							tc.memory, tc.expected, env.Value)
					}
					return
				}
			}
			t.Errorf("GOMEMLIMIT not found for memory %s", tc.memory)
		})
	}
}
