// Copyright 2025 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package scaler

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

func StartWorker(clientset *kubernetes.Clientset, podSelector, statefulSet string, graceful time.Duration) {
	ctx := context.Background()

	go func() {
		for {
			err := runDownscaler(ctx, clientset, podSelector, statefulSet, graceful)
			if err != nil {
				klog.Errorf("failed to run downscaler tick: %s", err.Error())
			}
			time.Sleep(time.Minute * 5)
		}
	}()
}

func runDownscaler(ctx context.Context, clientset kubernetes.Interface, podSelector, statefulSet string, graceful time.Duration) (err error) {
	defer func() {
		recoverErr := recover()
		if recoverErr != nil {
			err = fmt.Errorf("panic: %s", recoverErr)
		}
	}()

	buildKitPods, err := clientset.CoreV1().Pods("").List(ctx, v1.ListOptions{
		LabelSelector: podSelector,
	})
	if err != nil {
		return err
	}

	maxEndtimeByNS := map[string]int64{}

	for _, pod := range buildKitPods.Items {
		usageAt := int64(-1)

		lastBuildStart := pod.Annotations[metadata.DeployAgentLastBuildStartingLabelKey]
		lastBuildEnd := pod.Annotations[metadata.DeployAgentLastBuildEndingTimeLabelKey]

		// pod re-scheduled and lost starting and ending annotations
		if lastBuildStart == "" && lastBuildEnd == "" {
			usageAt = pod.CreationTimestamp.Time.Unix()
		}

		if lastBuildStart != "" && lastBuildEnd != "" {
			var parseErr error
			usageAt, parseErr = strconv.ParseInt(pod.Annotations[metadata.DeployAgentLastBuildEndingTimeLabelKey], 10, 64)
			if parseErr != nil {
				klog.Errorf("failed to parseint: %s", parseErr.Error())
				continue
			}
		}

		if usageAt == -1 {
			maxEndtimeByNS[pod.Namespace] = usageAt
			continue
		}

		if maxEndtimeByNS[pod.Namespace] == -1 {
			continue
		}

		if maxEndtimeByNS[pod.Namespace] < usageAt {
			maxEndtimeByNS[pod.Namespace] = usageAt
		}
	}

	now := time.Now().Unix()
	gracefulPeriod := int64(graceful.Seconds())
	zero := int32(0)

	for ns, maxEndtime := range maxEndtimeByNS {
		if maxEndtime == -1 {
			continue
		}

		if now-maxEndtime < gracefulPeriod {
			continue
		}

		statefulset, err := clientset.AppsV1().StatefulSets(ns).Get(ctx, statefulSet, v1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get statefulsets from ns: %s, err: %s", ns, err.Error())
			continue
		}

		if statefulset.Spec.Replicas != nil {
			if *statefulset.Spec.Replicas == 0 {
				continue
			}
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
