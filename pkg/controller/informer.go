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
	"github.com/coreos/etcd-operator/pkg/cluster"
	"github.com/coreos/etcd-operator/pkg/generated/informers/externalversions"
	"github.com/coreos/etcd-operator/pkg/util/k8sutil"
	kwatch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

// TODO: get rid of this once we use workqueue
var pt *panicTimer

func init() {
	pt = newPanicTimer(time.Minute, "unexpected long blocking (> 1 Minute) when handling cluster event")
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

// handleClusterEvent returns true if cluster is ignored (not managed) by this instance.
func (c *Controller) handleClusterEvent(event *Event) (bool, error) {
	clus := event.Object

	if !c.isOperatorManaged(clus) {
		return true, nil
	}

	if clus.Status.IsFailed() {
		clustersFailed.Inc()
		if event.Type == kwatch.Deleted {
			delete(c.clusters, getNamespacedName(clus))
			return false, nil
		}
		return false, fmt.Errorf("ignore failed cluster (%s). Please delete its CR", clus.Name)
	}

	clus.SetDefaults()

	if err := clus.Spec.Validate(); err != nil {
		return false, fmt.Errorf("invalid cluster spec. please fix the following problem with the cluster spec: %v", err)
	}

	switch event.Type {
	case kwatch.Added:
		if _, ok := c.clusters[getNamespacedName(clus)]; ok {
			return false, fmt.Errorf("unsafe state. cluster (%s) was created before but we received event (%s)", clus.Name, event.Type)
		}

		nc := cluster.New(c.makeClusterConfig(), clus)
		if nc == nil {
			return false, fmt.Errorf("cluster name cannot be more than %v characters long, please delete the CR\n", k8sutil.MaxNameLength)
		}
		nc.Start(context.TODO())
		c.clusters[getNamespacedName(clus)] = nc

		clustersCreated.Inc()
		clustersTotal.Inc()

	case kwatch.Modified:
		if _, ok := c.clusters[getNamespacedName(clus)]; !ok {
			return false, fmt.Errorf("unsafe state. cluster (%s) was never created but we received event (%s)", clus.Name, event.Type)
		}
		c.clusters[getNamespacedName(clus)].Update(clus)
		clustersModified.Inc()

	case kwatch.Deleted:
		if _, ok := c.clusters[getNamespacedName(clus)]; !ok {
			return false, fmt.Errorf("unsafe state. cluster (%s) was never created but we received event (%s)", clus.Name, event.Type)
		}
		c.clusters[getNamespacedName(clus)].Delete()
		delete(c.clusters, getNamespacedName(clus))
		clustersDeleted.Inc()
		clustersTotal.Dec()
	}
	return false, nil
}

func (c *Controller) isOperatorManaged(clus *api.EtcdCluster) bool {
	if v, ok := clus.Annotations[k8sutil.AnnotationScope]; ok {
		if c.Config.ClusterWide {
			return v == k8sutil.AnnotationClusterWide
		}
	} else {
		if !c.Config.ClusterWide {
			return true
		}
	}
	return false
}

func (c *Controller) makeClusterConfig() cluster.Config {
	return cluster.Config{
		ServiceAccount: c.Config.ServiceAccount,
		KubeCli:        c.Config.KubeCli,
		EtcdCRCli:      c.Config.EtcdCRCli,
	}
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

func getNamespacedName(c *api.EtcdCluster) string {
	return fmt.Sprintf("%s%c%s", c.Namespace, '/', c.Name)
}
