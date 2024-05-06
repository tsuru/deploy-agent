// Copyright 2024 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package downscaler

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/tsuru/deploy-agent/pkg/build/metadata"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

func StartWorker(clientset *kubernetes.Clientset, podSelector, buildkitStefulset string) {
	ctx := context.Background()

	go func() {
		for {
			err := Run(ctx, clientset, podSelector, buildkitStefulset)
			if err != nil {
				klog.Errorf("failed to run downscaler tick: %s", err.Error())
			}
			time.Sleep(time.Minute * 5)
		}
	}()
}

func Run(ctx context.Context, clientset *kubernetes.Clientset, podSelector, buildkitStefulset string) (err error) {
	defer func() {
		recoverErr := recover()
		err = fmt.Errorf("panic: %s", recoverErr)
	}()

	buildKitPods, err := clientset.CoreV1().Pods("*").List(ctx, v1.ListOptions{
		LabelSelector: podSelector,
	})

	if err != nil {
		return err
	}

	maxEndtimeByNS := map[string]int64{}

	for _, pod := range buildKitPods.Items {
		if pod.Annotations[metadata.DeployAgentLastBuildEndingTimeLabelKey] == "" {
			maxEndtimeByNS[pod.Namespace] = -1 // mark that namespace has least one pod of buildkit running
			continue
		}

		maxUsage, err := strconv.ParseInt(pod.Annotations[metadata.DeployAgentLastBuildEndingTimeLabelKey], 10, 64)
		if err != nil {
			klog.Errorf("failed to parseint: %s", err.Error())
			continue
		}

		if maxEndtimeByNS[pod.Namespace] == -1 {
			continue
		}

		if maxEndtimeByNS[pod.Namespace] < maxUsage {
			maxEndtimeByNS[pod.Namespace] = maxUsage
		}
	}

	now := time.Now().Unix()
	gracefulPeriod := int64(60 * 30)
	zero := int32(0)

	for ns, maxEndtime := range maxEndtimeByNS {
		if maxEndtime == -1 {
			continue
		}

		if now-maxEndtime < gracefulPeriod {
			continue
		}

		statefulset, err := clientset.AppsV1().StatefulSets(ns).Get(ctx, buildkitStefulset, v1.GetOptions{})

		if err != nil {
			klog.Errorf("failed to get statefullsets from ns: %s, err: %s", ns, err.Error())
			continue
		}

		if statefulset.Spec.Replicas != nil {
			statefulset.Annotations[metadata.DeployAgentLastReplicasAnnotationKey] = fmt.Sprintf("%d", *statefulset.Spec.Replicas)
		}

		statefulset.Spec.Replicas = &zero

		_, err = clientset.AppsV1().StatefulSets(ns).Update(ctx, statefulset, v1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update statefullsets from ns: %s, err: %s", ns, err.Error())
			continue
		}
	}

	return nil
}
