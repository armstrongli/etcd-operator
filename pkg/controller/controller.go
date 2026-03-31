// Copyright 2016 The etcd-operator Authors
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

package controller

import (
	"context"
	"fmt"
	"time"

	api "github.com/coreos/etcd-operator/pkg/apis/etcd/v1beta2"
	"github.com/coreos/etcd-operator/pkg/cluster"
	"github.com/coreos/etcd-operator/pkg/generated/clientset/versioned"
	"github.com/coreos/etcd-operator/pkg/util/k8sutil"
	"github.com/sirupsen/logrus"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	kwatch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

var initRetryWaitTime = 30 * time.Second

type Event struct {
	Type   kwatch.EventType
	Object *api.EtcdCluster
}

type Config struct {
	Namespace      string
	ClusterWide    bool
	ServiceAccount string
	KubeCli        kubernetes.Interface
	KubeExtCli     apiextensionsclient.Interface
	EtcdCRCli      versioned.Interface
	CreateCRD      bool
}

func New(cfg Config) *Controller {
	return &Controller{
		logger: logrus.WithField("pkg", "controller"),

		Config:   cfg,
		clusters: make(map[string]*cluster.Cluster),
	}
}

type Controller struct {
	logger *logrus.Entry
	Config

	clusters map[string]*cluster.Cluster
}

func (c *Controller) Run(ctx context.Context) error {
	// TODO: get rid of this init code. CRD and storage class will be managed outside of operator.
	for {
		err := c.initResource(ctx)
		if err == nil {
			break
		}
		c.logger.Warnf("initialization failed: %v, retry in %v...", err, initRetryWaitTime)
		time.Sleep(initRetryWaitTime)
	}

	if err := c.run(ctx); err != nil {
		c.logger.Errorf("error running etcd informer factory: %v", err)
		return err
	}
	return nil
}

func (c *Controller) initResource(ctx context.Context) error {
	if c.Config.CreateCRD {
		err := c.initCRD(ctx)
		if err != nil {
			return fmt.Errorf("fail to init CRD: %v", err)
		}
	}
	return nil
}

func (c *Controller) initCRD(ctx context.Context) error {
	err := k8sutil.CreateCRD(ctx, c.KubeExtCli, api.EtcdClusterCRDName, api.EtcdClusterResourceKind, api.EtcdClusterResourcePlural, "etcd")
	if err != nil {
		return fmt.Errorf("failed to create CRD: %v", err)
	}
	return k8sutil.WaitCRDReady(ctx, c.KubeExtCli, api.EtcdClusterCRDName)
}
