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
	"strings"
	"testing"

	api "github.com/coreos/etcd-operator/pkg/apis/etcd/v1beta2"
	"github.com/coreos/etcd-operator/pkg/util/etcdutil"
	v1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
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
	pod := newEtcdPod(m, []string{"test-etcd-0=http://test-etcd-0.test-etcd.svc:2380"},
		"test-etcd", "new", "test-token", cs)

	comparePods(t, expected, pod)
}

func TestNewEtcdPod_PeerTLS(t *testing.T) {
	expected := loadExpectedPod(podPeerTLSYAML)

	member := &TestMember{
		Member: etcdutil.Member{
			Name:         "test-etcd-0",
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
	pod := newEtcdPod(m, []string{"test-etcd-0=https://test-etcd-0.test-etcd.svc:2380"},
		"test-etcd", "existing", "", cs)

	comparePods(t, expected, pod)
}

func TestNewEtcdPod_ClientTLS(t *testing.T) {
	expected := loadExpectedPod(podClientTLSYAML)

	member := &TestMember{
		Member: etcdutil.Member{
			Name:         "test-etcd-0",
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
	pod := newEtcdPod(m, []string{"test-etcd-0=http://test-etcd-0.test-etcd.svc:2380"},
		"test-etcd", "new", "test-token", cs)

	comparePods(t, expected, pod)
}

func TestNewEtcdPod_BothTLS(t *testing.T) {
	expected := loadExpectedPod(podBothTLSYAML)

	member := &TestMember{
		Member: etcdutil.Member{
			Name:         "test-etcd-0",
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
	pod := newEtcdPod(m, []string{"test-etcd-0=https://test-etcd-0.test-etcd.svc:2380"},
		"test-etcd", "new", "test-token", cs)

	comparePods(t, expected, pod)
}

func TestNewEtcdPod_CustomBusybox(t *testing.T) {
	expected := loadExpectedPod(podCustomBusyboxYAML)

	member := &TestMember{
		Member: etcdutil.Member{
			Name:         "test-etcd-0",
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
	pod := newEtcdPod(m, []string{"test-etcd-0=http://test-etcd-0.test-etcd.svc:2380"},
		"test-etcd", "new", "test-token", cs)

	comparePods(t, expected, pod)
}

func TestNewEtcdPod_SecurityContext(t *testing.T) {
	expected := loadExpectedPod(podSecurityContextYAML)

	member := &TestMember{
		Member: etcdutil.Member{
			Name:         "test-etcd-0",
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
	pod := newEtcdPod(m, []string{"test-etcd-0=http://test-etcd-0.test-etcd.svc:2380"},
		"test-etcd", "existing", "", cs)

	comparePods(t, expected, pod)
}

// comparePods compares critical fields between expected and actual pods
func comparePods(t *testing.T, expected, actual *v1.Pod) {
	// Metadata
	if expected.Name != actual.Name {
		t.Errorf("Name mismatch: expected %s, got %s", expected.Name, actual.Name)
	}

	// Labels
	checkLabels(t, expected.Labels, actual.Labels)

	// Annotations
	if expected.Annotations[etcdVersionAnnotationKey] != actual.Annotations[etcdVersionAnnotationKey] {
		t.Errorf("etcd version annotation mismatch: expected %s, got %s",
			expected.Annotations[etcdVersionAnnotationKey],
			actual.Annotations[etcdVersionAnnotationKey])
	}

	// Pod spec
	if expected.Spec.Hostname != actual.Spec.Hostname {
		t.Errorf("Hostname mismatch: expected %s, got %s", expected.Spec.Hostname, actual.Spec.Hostname)
	}
	if expected.Spec.Subdomain != actual.Spec.Subdomain {
		t.Errorf("Subdomain mismatch: expected %s, got %s", expected.Spec.Subdomain, actual.Spec.Subdomain)
	}

	// Init containers
	if len(expected.Spec.InitContainers) != len(actual.Spec.InitContainers) {
		t.Errorf("Init container count mismatch: expected %d, got %d",
			len(expected.Spec.InitContainers), len(actual.Spec.InitContainers))
	} else if len(expected.Spec.InitContainers) > 0 {
		checkInitContainer(t, &expected.Spec.InitContainers[0], &actual.Spec.InitContainers[0])
	}

	// Containers
	if len(expected.Spec.Containers) != len(actual.Spec.Containers) {
		t.Errorf("Container count mismatch: expected %d, got %d",
			len(expected.Spec.Containers), len(actual.Spec.Containers))
		return
	}

	checkContainer(t, &expected.Spec.Containers[0], &actual.Spec.Containers[0])

	// Volumes
	checkVolumes(t, expected.Spec.Volumes, actual.Spec.Volumes)

	// Security Context
	checkSecurityContext(t, expected.Spec.SecurityContext, actual.Spec.SecurityContext)
}

func checkLabels(t *testing.T, expected, actual map[string]string) {
	for k, v := range expected {
		if actual[k] != v {
			t.Errorf("Label %s mismatch: expected %s, got %s", k, v, actual[k])
		}
	}

	// Check for required labels
	requiredLabels := []string{"app", "etcd_node", "etcd_cluster"}
	for _, label := range requiredLabels {
		if _, ok := actual[label]; !ok {
			t.Errorf("Missing required label: %s", label)
		}
	}
}

func checkInitContainer(t *testing.T, expected, actual *v1.Container) {
	if expected.Image != actual.Image {
		t.Errorf("Init container image mismatch: expected %s, got %s", expected.Image, actual.Image)
	}

	// Check command contains DNS lookup
	if len(actual.Command) > 0 {
		cmd := strings.Join(actual.Command, " ")
		if !strings.Contains(cmd, "nslookup") {
			t.Error("Init container command should contain nslookup")
		}
	}
}

func checkContainer(t *testing.T, expected, actual *v1.Container) {
	if expected.Image != actual.Image {
		t.Errorf("Container image mismatch: expected %s, got %s", expected.Image, actual.Image)
	}

	// Compare command
	if len(expected.Command) > 0 && len(actual.Command) > 0 {
		expCmd := strings.Join(expected.Command, " ")
		actCmd := strings.Join(actual.Command, " ")

		// Check for essential etcd arguments
		essentialArgs := []string{
			"--data-dir=" + dataDir,
			"--name=",
			"--listen-peer-urls=",
			"--listen-client-urls=",
			"--advertise-client-urls=",
			"--initial-cluster=",
		}

		for _, arg := range essentialArgs {
			if !strings.Contains(actCmd, arg) {
				t.Errorf("Missing essential etcd argument: %s", arg)
			}
		}

		// Check TLS flags if they should be present
		if strings.Contains(expCmd, "--peer-client-cert-auth=true") {
			if !strings.Contains(actCmd, "--peer-client-cert-auth=true") {
				t.Error("Missing peer TLS flags in command")
			}
		}

		if strings.Contains(expCmd, "--client-cert-auth=true") {
			if !strings.Contains(actCmd, "--client-cert-auth=true") {
				t.Error("Missing client TLS flags in command")
			}
		}
	}

	// Check probes
	if expected.LivenessProbe != nil && actual.LivenessProbe == nil {
		t.Error("Missing liveness probe")
	}
	if expected.ReadinessProbe != nil && actual.ReadinessProbe == nil {
		t.Error("Missing readiness probe")
	}

	// Check volume mounts
	checkVolumeMounts(t, expected.VolumeMounts, actual.VolumeMounts)
}

func checkVolumes(t *testing.T, expected, actual []v1.Volume) {
	if len(expected) != len(actual) {
		t.Errorf("Volume count mismatch: expected %d, got %d", len(expected), len(actual))
		return
	}

	// Check for required volumes
	if !apiequality.Semantic.DeepEqual(expected, actual) {
		t.Error("volume is not equal", "expected", expected, "actual", actual)
	}
}

func checkVolumeMounts(t *testing.T, expected, actual []v1.VolumeMount) {
	// Check for required volume mounts
	hasDataMount := false
	for _, vm := range actual {
		if vm.Name == etcdVolumeName && vm.MountPath == etcdVolumeMountDir {
			hasDataMount = true
		}
	}
	if !hasDataMount {
		t.Error("Missing etcd data volume mount")
	}
}

func checkSecurityContext(t *testing.T, expected, actual *v1.PodSecurityContext) {
	if expected == nil && actual == nil {
		return
	}

	if expected != nil && actual == nil {
		t.Error("Expected security context but got nil")
		return
	}

	if expected != nil && actual != nil {
		if expected.RunAsUser != nil && actual.RunAsUser != nil {
			if *expected.RunAsUser != *actual.RunAsUser {
				t.Errorf("RunAsUser mismatch: expected %d, got %d", *expected.RunAsUser, *actual.RunAsUser)
			}
		}

		if expected.RunAsGroup != nil && actual.RunAsGroup != nil {
			if *expected.RunAsGroup != *actual.RunAsGroup {
				t.Errorf("RunAsGroup mismatch: expected %d, got %d", *expected.RunAsGroup, *actual.RunAsGroup)
			}
		}
	}
}
