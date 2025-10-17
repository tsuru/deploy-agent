// Copyright 2025 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package autodiscovery

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/tsuru/deploy-agent/pkg/build/grpc_build_v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	fakeDynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	kuberntesTesting "k8s.io/client-go/testing"
)

func TestK8sDiscoverer_Discover(t *testing.T) {
	buildKitPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "tsuru",
			Labels: map[string]string{
				"app": "test-app",
			},
			Annotations: map[string]string{
				"foo": "bar",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "127.0.0.1",
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
	fakeClient := fake.NewSimpleClientset(buildKitPod)

	fakeClient.PrependWatchReactor("*", func(action kuberntesTesting.Action) (handled bool, ret watch.Interface, err error) {
		watcher := watch.NewFake()

		go func() {
			time.Sleep(time.Millisecond * 100)
			watcher.Add(buildKitPod)
		}()
		return true, watcher, nil
	})

	fakeDynamicClient := fakeDynamic.NewSimpleDynamicClient(runtime.NewScheme())

	discoverer := K8sDiscoverer{
		KubernetesInterface: fakeClient,
		DynamicInterface:    fakeDynamicClient,
	}

	_, _, err := discoverer.Discover(
		context.TODO(),
		KubernertesDiscoveryOptions{
			PodSelector:      "app=test-app",
			Namespace:        "tsuru",
			Timeout:          time.Second * 2,
			SetTsuruAppLabel: true,
		},
		&grpc_build_v1.BuildRequest{
			App: &grpc_build_v1.TsuruApp{
				Name: "test-app",
				Team: "test-team",
			},
		},
		os.Stdout,
	)
	assert.NoError(t, err)

	existingPod, err := fakeClient.CoreV1().Pods("tsuru").Get(context.TODO(), "test-app", metav1.GetOptions{})
	assert.NoError(t, err)

	assert.Equal(t, map[string]string{
		"app":               "test-app",
		"tsuru.io/app-name": "test-app",
		"tsuru.io/app-team": "test-team",
		"tsuru.io/is-build": "true",
	}, existingPod.Labels)

	assert.Equal(t, "", existingPod.Annotations["deploy-agent.tsuru.io/last-build-ending-time"])
	assert.NotEqual(t, "", existingPod.Annotations["deploy-agent.tsuru.io/last-build-starting-time"])
}
