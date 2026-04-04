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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
)

const (
	defaultEtcdClusterReconcileTimeout = 1 * time.Minute
	defaultEtcdClusterCacheTimeout     = 15 * time.Minute // for large clusters, cache sync takes long time
)

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
	c._etcdClusterInterface = &etcdClusterConsumer{
		_etcdClusterLister: etcdClusterFactory.Etcd().V1beta2().EtcdClusters().Lister(),
	}
	c._etcdClusterSyncFunc = etcdClusterFactory.Etcd().V1beta2().EtcdClusters().Informer().HasSynced
	etcdClusterFactory.Start(ctx.Done())
	if err := c.startWorkers(ctx); err != nil {
		c.logger.Errorf("error starting workers: %v", err)
		return err
	}
	<-ctx.Done()
	return nil
}

func (c *Controller) onAddEtcdCluster(obj interface{}) {
	etcdCluster, ok := obj.(*api.EtcdCluster)
	if !ok {
		c.logger.Warningf("got none EtcdCluster type: %+v", obj)
		return
	}
	key, err := cache.MetaNamespaceKeyFunc(etcdCluster)
	if err != nil {
		c.logger.Warningf("failed to get cache key: %v", err)
		return
	}
	c.clusterQueue.Add(key)
}

func (c *Controller) onUpdateEtcdCluster(oldObj, newObj interface{}) {
	// TODO add compare logic to ignore some update scenarios
	etcdCluster, ok := newObj.(*api.EtcdCluster)
	if !ok {
		return
	}
	key, err := cache.MetaNamespaceKeyFunc(etcdCluster)
	if err != nil {
		c.logger.Warningf("failed to get cache key: %v", err)
		return
	}
	c.clusterQueue.Add(key)
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
	key, err := cache.MetaNamespaceKeyFunc(clus)
	if err != nil {
		c.logger.Warningf("failed to get cache key: %v", err)
		return
	}
	c.clusterQueue.Add(key)
}

func (c *Controller) startWorkers(ctx context.Context) error {
	ctx4cache, cancel := context.WithTimeout(ctx, defaultEtcdClusterCacheTimeout)
	defer cancel()
	c.logger.Info("waiting for cache ready")
	if !cache.WaitForCacheSync(ctx4cache.Done(), c._etcdClusterSyncFunc) {
		c.logger.Errorf("timeout waiting for cache reacy. timeout after %s", defaultEtcdClusterCacheTimeout)
		return fmt.Errorf("timeout waiting for cache ready")
	}
	workerCount := c.WorkerCount
	if workerCount == 0 {
		workerCount = 10
		c.logger.Infof("worker count is 0, set up default worker count: %d", workerCount)
	}
	c.logger.Infof("starting %d workers", workerCount)
	for i := 0; i < workerCount; i++ {
		go c.runWorker(ctx)
	}
	return nil
}

func (c *Controller) runWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		key, queueShutdown := c.clusterQueue.Get()
		if queueShutdown {
			return
		}
		if err := c.processEtcdClusterItem(ctx, key.(string)); err != nil {
			c.clusterQueue.AddRateLimited(key)
		} else {
			c.clusterQueue.Forget(key)
		}
		c.clusterQueue.Done(key)
	}
}

func (c *Controller) processEtcdClusterItem(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, defaultEtcdClusterReconcileTimeout)
	defer cancel()
	// TODO set up context on reconcile process to guarantee reconcilation in control
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.logger.Warningf("can't split key[%s], ignore it with error: %s", key, err)
		return nil
	}
	_logger := c.logger.WithField("key", key)
	_logger.Info("start processing etcd cluster")
	etcdCluster, err := c._etcdClusterInterface.GetEtcdCluster(ns, name)
	if err != nil {
		if errors.IsNotFound(err) {
			// process etcd deletion logic. all etcd cluster instances(service/pod/volume) are managed through
			// object owner reference. no specific logic is required. clean the trace from cache
			c.clustersLock.Lock()
			if _, ok := c.clusters[key]; !ok {
				_logger.Warningf("unsafe state. cluster (%s) was never created but we received event", key)
				return nil
			}
			c.clusters[key].Delete()
			delete(c.clusters, key)
			c.clustersLock.Unlock()
			clustersDeleted.Inc()
			clustersTotal.Dec()
			return nil
		}
		_logger.Errorf("failed to get etcdcluster from cache: %v", err)
		return err
	}
	// reconcile etcd cluster idempotently
	if !c.isOperatorManaged(etcdCluster) {
		_logger.Infof("not controller managed etcd cluster, ignore")
		return nil
	}
	if etcdCluster.Status.IsFailed() {
		clustersFailed.Inc()
		_logger.Warningf("etcd cluster is at Failed state. please reach etcd admin to fix it manually before further process")
		return nil
	}

	etcdCluster.SetDefaults()

	if err := etcdCluster.Spec.Validate(); err != nil {
		_logger.Errorf("invalid cluster spec. please fix the following problem with the cluster spec: %v", err)
		return nil
	}

	c.clustersLock.Lock()
	defer c.clustersLock.Unlock()
	if _, ok := c.clusters[key]; !ok {
		// add new cluster trace and catch up etcd cluster
		nc := cluster.New(c.makeClusterConfig(), etcdCluster)
		if nc == nil {
			// TODO record warning event on the error
			return fmt.Errorf("cluster name cannot be more than %v characters long, please delete the CR\n", k8sutil.MaxNameLength)
		}

		nc.Start(context.TODO())
		c.clusters[key] = nc

		clustersCreated.Inc()
		clustersTotal.Inc()
		return nil
	}
	// reconcile existing etcd cluster
	c.clusters[key].Update(etcdCluster)
	clustersModified.Inc()
	return nil
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
