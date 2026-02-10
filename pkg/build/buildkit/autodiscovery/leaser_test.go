// Copyright 2026 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package autodiscovery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestLeaser_ConcurrentMapAccess tests for concurrent map access race conditions.
// This test will fail with -race flag if the leaseCancelByPod map is accessed
// without proper synchronization (mutex).
//
// The race condition being tested:
// - acquireLeaseForAllPods() writes to leaseCancelByPod map (goroutine)
// - releaseAll() reads from leaseCancelByPod map (multiple goroutines)
// - Without mutex protection, concurrent write/read causes a data race
func TestLeaser_ConcurrentMapAccess(t *testing.T) {
	kubeClient := fake.NewSimpleClientset()
	leasablePodsCh := make(chan *corev1.Pod, 20)
	holderName := "test-holder"

	leaser, _, err := newLeaser(kubeClient, leasablePodsCh, holderName)
	require.NoError(t, err, "failed to create leaser")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start the goroutine that writes to the map
	go leaser.acquireLeaseForAllPods(ctx, KubernertesDiscoveryOptions{
		LeasePrefix: "test-",
	})

	// Send multiple pods to trigger concurrent map writes
	for i := range 20 {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "buildkit-" + string(rune('a'+i)),
				Namespace: "tsuru",
			},
		}
		leasablePodsCh <- pod
	}

	// Launch multiple goroutines that read from the map concurrently
	// This creates write/read races if no mutex protection exists
	concurrency := 5
	done := make(chan struct{}, concurrency)

	for g := range concurrency {
		go func(id int) {
			for range 20 {
				leaser.releaseAll()
				time.Sleep(time.Microsecond * 50)
			}
			done <- struct{}{}
		}(g)
	}

	// Wait for all reader goroutines to complete
	for range concurrency {
		<-done
	}
}
