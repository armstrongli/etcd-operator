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
	pod := newEtcdPod(m, []string{"test-etcd-0=http://test-etcd-0.test-etcd.default.svc:2380"},
		"test-etcd", "new", "test-token", cs)

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
	pod := newEtcdPod(m, []string{"test-etcd-0=https://test-etcd-0.test-etcd.default.svc:2380"},
		"test-etcd", "existing", "", cs)

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
	pod := newEtcdPod(m, []string{"test-etcd-0=http://test-etcd-0.test-etcd.default.svc:2380"},
		"test-etcd", "new", "test-token", cs)

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
	pod := newEtcdPod(m, []string{"test-etcd-0=https://test-etcd-0.test-etcd.default.svc:2380"},
		"test-etcd", "new", "test-token", cs)

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
	pod := newEtcdPod(m, []string{"test-etcd-0=http://test-etcd-0.test-etcd.default.svc:2380"},
		"test-etcd", "new", "test-token", cs)

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
	pod := newEtcdPod(m, []string{"test-etcd-0=http://test-etcd-0.test-etcd.default.svc:2380"},
		"test-etcd", "existing", "", cs)

	if !apiequality.Semantic.DeepEqual(expected, pod) {
		expectedStr, _ := yaml.Marshal(expected.Spec.Containers)
		actualStr, _ := yaml.Marshal(pod.Spec.Containers)
		if diff := cmp.Diff(expectedStr, actualStr); diff != "" {
			t.Errorf("Summary diff:\n%s", diff)
		}
	}
}
