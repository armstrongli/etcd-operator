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

package controller

import (
	"context"
	"fmt"
	"time"

	api "github.com/coreos/etcd-operator/pkg/apis/etcd/v1beta2"
	"github.com/coreos/etcd-operator/pkg/generated/informers/externalversions"
	kwatch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

// TODO: get rid of this once we use workqueue
var pt *panicTimer

func init() {
	pt = newPanicTimer(time.Minute, "unexpected long blocking (> 1 Minute) when handling cluster event")
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

func (c *Controller) run(ctx context.Context) error {
	facOptions := []externalversions.SharedInformerOption{}
	if !c.Config.ClusterWide {
		facOptions = append(facOptions, externalversions.WithNamespace(c.Config.Namespace))
	}

	etcdClusterFactory := externalversions.NewSharedInformerFactoryWithOptions(
		c.Config.EtcdCRCli,
		0,
		facOptions...,
	)
	_, err := etcdClusterFactory.Etcd().V1beta2().EtcdClusters().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onAddEtcdCluster,
		UpdateFunc: c.onUpdateEtcdCluster,
		DeleteFunc: c.onDeleteEtcdCluster,
	})
	if err != nil {
		c.logger.Errorf("error adding event handler etcdcluster object: %v", err)
		return err
	}
	etcdClusterFactory.Start(ctx.Done())
	<-ctx.Done()
	return nil
}

func (c *Controller) onAddEtcdCluster(obj interface{}) {
	c.syncEtcdCluster(obj.(*api.EtcdCluster))
}

func (c *Controller) onUpdateEtcdCluster(oldObj, newObj interface{}) {
	c.syncEtcdCluster(newObj.(*api.EtcdCluster))
}

func (c *Controller) onDeleteEtcdCluster(obj interface{}) {
	clus, ok := obj.(*api.EtcdCluster)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			c.logger.Warningf("unknown object from EtcdCluster delete event: %#v", obj)
			return
		}
		clus, ok = tombstone.Obj.(*api.EtcdCluster)
		if !ok {
			c.logger.Warningf("Tombstone contained object that is not an EtcdCluster: %#v", obj)
			return
		}
	}
	ev := &Event{
		Type:   kwatch.Deleted,
		Object: clus,
	}

	pt.start()
	_, err := c.handleClusterEvent(ev)
	if err != nil {
		c.logger.Warningf("fail to handle event: %v", err)
	}
	pt.stop()
}

func (c *Controller) syncEtcdCluster(clus *api.EtcdCluster) {
	ev := &Event{
		Type:   kwatch.Added,
		Object: clus,
	}
	// re-watch or restart could give ADD event.
	// If for an ADD event the cluster spec is invalid then it is not added to the local cache
	// so modifying that cluster will result in another ADD event
	if _, ok := c.clusters[getNamespacedName(clus)]; ok {
		ev.Type = kwatch.Modified
	}

	pt.start()
	_, err := c.handleClusterEvent(ev)
	if err != nil {
		c.logger.Warningf("fail to handle event: %v", err)
	}
	pt.stop()
}
