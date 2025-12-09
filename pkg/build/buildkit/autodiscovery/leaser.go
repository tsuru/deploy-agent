// Copyright 2025 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package autodiscovery

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog"
)

type leaser struct {
	kubernetesInterface kubernetes.Interface
	leasablePodsCh      <-chan *corev1.Pod
	leasedPodsCh        chan<- *corev1.Pod
	leaseAcquiringWg    *sync.WaitGroup
	leaseCancelByPod    map[string]context.CancelFunc
	holderName          string
}

func newLeaser(kubernetesInterface kubernetes.Interface, leasablePodsCh <-chan *corev1.Pod) (*leaser, <-chan *corev1.Pod, error) {
	holderName := os.Getenv("POD_NAME")
	if holderName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, nil, err
		}
		holderName = hostname
	}
	leasedPodsCh := make(chan *corev1.Pod, 1)
	return &leaser{
		kubernetesInterface: kubernetesInterface,
		leasablePodsCh:      leasablePodsCh,
		leasedPodsCh:        leasedPodsCh,
		leaseAcquiringWg:    &sync.WaitGroup{},
		leaseCancelByPod:    make(map[string]context.CancelFunc),
		holderName:          holderName,
	}, leasedPodsCh, nil
}

type releaseOptions struct {
	except string
}

func (l *leaser) releaseAll(opts ...releaseOptions) {
	var opt releaseOptions
	if len(opts) == 0 {
		opt = releaseOptions{}
	} else {
		opt = opts[0]
	}
	l.leaseAcquiringWg.Wait()
	for name, leaseCancel := range l.leaseCancelByPod {
		if opt.except == name {
			continue
		}
		klog.V(4).Infof("Releasing lock for %s pod", name)
		leaseCancel()
	}
}

// acquireLeaseForAllPods tries to acquire leases for all pods received on leasablePodsCh.
// it is a blocking call and only returns after leasablePodsCh is closed and all lease acquisition.
// it should probably be used in a separate goroutine.
func (l *leaser) acquireLeaseForAllPods(ctx context.Context, opts KubernertesDiscoveryOptions) {
	// NOTE:(ravilock) the usage of WaitGroup here is to ensure that we only close the leasedPodsCh
	// after all goroutines that might write to it are done. i.e. The goroutines that acquire leases for a buildkit pod.
	for leasablePod := range l.leasablePodsCh {
		if _, found := l.leaseCancelByPod[leasablePod.Name]; found {
			continue
		}

		leaseCtx, leaseCancel := context.WithCancel(ctx)
		l.leaseCancelByPod[leasablePod.Name] = leaseCancel

		l.leaseAcquiringWg.Add(1)
		go func() {
			defer l.leaseAcquiringWg.Done()
			l.acquireLeaseForPod(leaseCtx, leasablePod, opts)
		}()
	}
	l.leaseAcquiringWg.Wait()
	close(l.leasedPodsCh)
}

// acquireLeaseForPod tries to acquire a lease for the given pod.
// it is a blocking call and only returns after the lease is lost or the given context is canceled.
// it should always be used in a separate goroutine.
func (l *leaser) acquireLeaseForPod(ctx context.Context, pod *corev1.Pod, opts KubernertesDiscoveryOptions) {
	uniqueHolderName := fmt.Sprintf("%s-%d", l.holderName, time.Now().Unix())
	klog.V(4).Infof("Attempting to acquire the lease for pod %s/%s under holder name %q...", pod.Namespace, pod.Name, uniqueHolderName)
	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", strings.TrimRight(opts.LeasePrefix, "-"), pod.Name),
				Namespace: pod.Namespace,
			},
			Client: l.kubernetesInterface.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: uniqueHolderName,
			},
		},
		ReleaseOnCancel: true,
		LeaseDuration:   5 * time.Second,
		RenewDeadline:   2 * time.Second,
		RetryPeriod:     500 * time.Millisecond,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(_ context.Context) {
				select {
				case l.leasedPodsCh <- pod:
					klog.V(4).Infof("Selected BuildKit pod: %s/%s", pod.Namespace, pod.Name)

				case <-ctx.Done():
					klog.V(4).Infof("Received context cancellation: %s/%s", pod.Namespace, pod.Name)
				}
			},
			OnStoppedLeading: func() {},
		},
	})
	klog.V(4).Infof("Shutting off the lease for %s/%s pod", pod.Namespace, pod.Name)
}
