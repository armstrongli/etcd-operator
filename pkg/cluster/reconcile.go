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

package cluster

import (
	"context"
	"errors"
	"fmt"

	api "github.com/coreos/etcd-operator/pkg/apis/etcd/v1beta2"
	"github.com/coreos/etcd-operator/pkg/util/constants"
	"github.com/coreos/etcd-operator/pkg/util/etcdutil"
	"github.com/coreos/etcd-operator/pkg/util/k8sutil"
	rpctypes "go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ErrLostQuorum indicates that the etcd cluster lost its quorum.
var ErrLostQuorum = errors.New("lost quorum")

// reconcile reconciles cluster current state to desired state specified by spec.
// - it tries to reconcile the cluster to desired size.
// - if the cluster needs for upgrade, it tries to upgrade old member one by one.
func (c *Cluster) reconcile(pods []*v1.Pod) error {
	c.logger.Infoln("Start reconciling")
	defer c.logger.Infoln("Finish reconciling")

	defer func() {
		memberCount, _ := c.members.Size()
		// TODO add learner count in cluster status
		c.status.Size = memberCount
	}()

	sp := c.cluster.Spec
	running := podsToMemberSet(pods, c.isSecureClient())
	memberCount, _ := c.members.Size()
	// TODO add learner reconcile
	if !running.IsEqual(c.members) || memberCount != sp.Size {
		return c.reconcileMembers(running)
	}
	c.status.ClearCondition(api.ClusterConditionScaling)

	if needUpgrade(pods, sp) {
		c.status.UpgradeVersionTo(sp.Version)

		m := pickOneOldMember(pods, sp.Version)
		return c.upgradeOneMember(m.Name)
	}
	c.status.ClearCondition(api.ClusterConditionUpgrading)

	c.status.SetVersion(sp.Version)
	c.status.SetReadyCondition()

	return nil
}

// reconcileMembers reconciles
// - running pods on k8s and cluster membership
// - cluster membership and expected size of etcd cluster
// Steps:
// 1. Remove all pods from running set that does not belong to member set.
// 2. L consist of remaining pods of runnings
// 3. If L = members, the current state matches the membership state. END.
// 4. If len(L) < len(members)/2 + 1, return quorum lost error.
// 5. Add one missing member. END.
func (c *Cluster) reconcileMembers(running etcdutil.MemberSet) error {
	c.logger.Infof("running members: %s", running)
	c.logger.Infof("cluster membership: %s", c.members)

	unknownMembers := running.Diff(c.members)
	memberCount, learnerCount := unknownMembers.Size()
	if memberCount+learnerCount > 0 {
		c.logger.Infof("removing unexpected pods: %v", unknownMembers)
		for _, m := range unknownMembers {
			if err := c.removePod(m.Name); err != nil {
				return err
			}
		}
	}
	L := running.Diff(unknownMembers)
	LmemberCount, LlearnerCount := L.Size()
	cMemberCount, cLearnerCount := c.members.Size()

	if LmemberCount+LlearnerCount == cMemberCount+cLearnerCount {
		return c.resize()
	}

	if LmemberCount < cMemberCount/2+1 {
		return ErrLostQuorum
	}

	c.logger.Infof("removing one dead member")
	// remove dead members that doesn't have any running pods before doing resizing.
	return c.removeDeadMember(c.members.Diff(L).PickOne())
}

func (c *Cluster) resize() error {
	cMemberCount, cLearnerCount := c.members.Size()
	if cMemberCount == c.cluster.Spec.Size {
		return nil
	}

	if cLearnerCount > 0 {
		toPromoteCount := c.cluster.Spec.Size - cMemberCount
		return c.promoteMembers(toPromoteCount)
	}

	if cMemberCount < c.cluster.Spec.Size {
		return c.addOneMember()
	}

	return c.removeOneMember()
}

func (c *Cluster) addOneMember() error {
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	memberCount, learnerCount := c.members.Size()
	c.status.SetScalingUpCondition(memberCount+learnerCount, c.cluster.Spec.Size)

	tlsCfg, err := c.getTLSConfig(context.TODO())
	if err != nil {
		return err
	}

	cfg := clientv3.Config{
		Endpoints:   c.members.ClientURLs(),
		DialTimeout: constants.DefaultDialTimeout,
		TLS:         tlsCfg,
	}
	etcdcli, err := clientv3.New(cfg)
	if err != nil {
		return fmt.Errorf("add one member failed: creating etcd client failed %v", err)
	}
	defer etcdcli.Close()

	newMember := c.newMember()
	newMemberId, err := func(ctx context.Context) (memberId uint64, err error) {
		ctx, cancel := context.WithTimeout(context.Background(), constants.DefaultRequestTimeout)
		resp, err := etcdcli.MemberAddAsLearner(ctx, []string{newMember.PeerURL()})
		cancel()
		if err != nil {
			return 0, err
		}
		return resp.Member.ID, nil
	}(ctx)
	if err != nil {
		return fmt.Errorf("fail to add new member (%s): %v", newMember.Name, err)
	}
	newMember.ID = newMemberId
	newMember.IsLearner = true
	c.members.Add(newMember)

	if err := c.createPod(c.members, newMember, "existing"); err != nil {
		return fmt.Errorf("fail to create member's pod (%s): %v", newMember.Name, err)
	}
	c.logger.Infof("added member (%s)", newMember.Name)
	_, err = c.eventsCli.Create(ctx, k8sutil.NewMemberAddEvent(newMember.Name, c.cluster), metav1.CreateOptions{})
	if err != nil {
		c.logger.Warningf("failed to create new member add event: %v", err)
	}
	return nil
}

func (c *Cluster) removeOneMember() error {
	memberCount, _ := c.members.Size()
	c.status.SetScalingDownCondition(memberCount, c.cluster.Spec.Size)

	return c.removeMember(c.members.PickOne())
}

func (c *Cluster) removeDeadMember(toRemove *etcdutil.Member) error {
	c.logger.Infof("removing dead member %q", toRemove.Name)
	_, err := c.eventsCli.Create(context.TODO(), k8sutil.ReplacingDeadMemberEvent(toRemove.Name, c.cluster), metav1.CreateOptions{})
	if err != nil {
		c.logger.Warningf("failed to create replacing dead member event: %v", err)
	}

	return c.removeMember(toRemove)
}

func (c *Cluster) removeMember(toRemove *etcdutil.Member) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("remove member (%s) failed: %v", toRemove.Name, err)
		}
	}()

	tlsCfg, err := c.getTLSConfig(context.TODO())
	if err != nil {
		return err
	}
	err = etcdutil.RemoveMember(c.members.ClientURLs(), tlsCfg, toRemove.ID)
	if err != nil {
		switch err {
		case rpctypes.ErrMemberNotFound:
			c.logger.Infof("etcd member (%v) has been removed", toRemove.Name)
		default:
			return err
		}
	}
	c.members.Remove(toRemove.Name)
	_, err = c.eventsCli.Create(context.TODO(), k8sutil.MemberRemoveEvent(toRemove.Name, c.cluster), metav1.CreateOptions{})
	if err != nil {
		c.logger.Errorf("failed to create remove member event: %v", err)
	}
	if err := c.removePod(toRemove.Name); err != nil {
		return err
	}
	if c.isPodPVEnabled() {
		err = c.removePVC(k8sutil.PVCNameFromMember(toRemove.Name))
		if err != nil {
			return err
		}
	}
	c.logger.Infof("removed member (%v) with ID (%d)", toRemove.Name, toRemove.ID)
	return nil
}

func (c *Cluster) promoteMembers(toPromoteCount int) error {
	// promote one etcd learner to member
	c.logger.Infof("promoting %d members from learner", toPromoteCount)
	promoteErrors := make([]error, 0, toPromoteCount)
	for _, learner := range c.members.Learners() {
		if err := c.promoteMember(learner); err != nil {
			c.logger.Warningf("eror promoting learner[%s]", learner.Name)
			promoteErrors = append(promoteErrors, err)
			continue
		}
		toPromoteCount--
		if toPromoteCount <= 0 {
			break
		}
	}
	if toPromoteCount > 0 {
		c.logger.Errorf("not enough learners promoted, there could be errors: %v", promoteErrors)
		return fmt.Errorf("%+v", promoteErrors)
	}
	return nil
}

func (c *Cluster) promoteMember(toPromote etcdutil.Member) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("promote member (%s) failed: %v", toPromote.Name, err)
		}
	}()

	tlsCfg, err := c.getTLSConfig(context.TODO())
	if err != nil {
		return err
	}
	err = etcdutil.PromoteMember(c.members.ClientURLs(), tlsCfg, toPromote.ID)
	if err != nil {
		switch err {
		case rpctypes.ErrMemberNotFound:
			c.logger.Infof("etcd member (%v) has been removed", toPromote.Name)
		default:
			c.logger.Errorf("error promoting member(%s: %x): %v", toPromote.Name, toPromote.ID, err)
			return err
		}
	}
	toPromote.IsLearner = false
	c.members.Add(&toPromote)
	_, err = c.eventsCli.Create(context.TODO(), k8sutil.MemberPromoteEvent(toPromote.Name, c.cluster), metav1.CreateOptions{})
	if err != nil {
		c.logger.Warningf("failed to create promote member event: %v", err)
	}
	c.logger.Infof("promote member (%v) with ID (%d)", toPromote.Name, toPromote.ID)
	return nil
}

func (c *Cluster) removePVC(pvcName string) error {
	err := c.config.KubeCli.CoreV1().PersistentVolumeClaims(c.cluster.Namespace).Delete(context.TODO(), pvcName, metav1.DeleteOptions{})
	if err != nil && !k8sutil.IsKubernetesResourceNotFoundError(err) {
		return fmt.Errorf("remove pvc (%s) failed: %v", pvcName, err)
	}
	return nil
}

func needUpgrade(pods []*v1.Pod, cs api.ClusterSpec) bool {
	return len(pods) == cs.Size && pickOneOldMember(pods, cs.Version) != nil
}

func pickOneOldMember(pods []*v1.Pod, newVersion string) *etcdutil.Member {
	for _, pod := range pods {
		if k8sutil.GetEtcdVersion(pod) == newVersion {
			continue
		}
		return &etcdutil.Member{Name: pod.Name, Namespace: pod.Namespace}
	}
	return nil
}
