// Copyright 2026 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package autodiscovery

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog"
)

type podNotifier struct {
	podWatcher watch.Interface
	pods       chan<- *corev1.Pod
}

func newPodNotifier(podWatcher watch.Interface) (*podNotifier, <-chan *corev1.Pod) {
	pods := make(chan *corev1.Pod)
	return &podNotifier{podWatcher: podWatcher, pods: pods}, pods
}

type filterCondition func(pod *corev1.Pod) bool

func (n *podNotifier) notify(ctx context.Context, conditions ...filterCondition) {
	defer close(n.pods)
	defer n.podWatcher.Stop() // watch cancellation must happen before than closing the pods channel

	for {
		select {
		case e, ok := <-n.podWatcher.ResultChan():
			if !ok {
				klog.Error("Pod watcher channel closed unexpectedly")
				return
			}
			if e.Type != watch.Added && e.Type != watch.Modified {
				continue
			}

			pod := e.Object.(*corev1.Pod)
			if applyConditions(pod, conditions...) {
				n.pods <- pod
			} else {
				klog.V(4).Infof("Pod %s/%s is not ready yet", pod.Namespace, pod.Name)
			}
		case <-ctx.Done():
			return
		}
	}
}

func applyConditions(pod *corev1.Pod, conditions ...filterCondition) bool {
	for _, condition := range conditions {
		if !condition(pod) {
			return false
		}
	}
	return true
}
