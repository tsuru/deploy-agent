// Copyright 2025 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package autodiscovery

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsuru/deploy-agent/pkg/build/grpc_build_v1"
	"github.com/tsuru/deploy-agent/pkg/build/metadata"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	fakeDynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	kuberntesTesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"
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

	_, _, _, err := discoverer.Discover(
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

func TestK8sDiscoverer_DiscoverWithoutApp(t *testing.T) {
	discoverer := K8sDiscoverer{
		KubernetesInterface: fake.NewSimpleClientset(),
		DynamicInterface:    fakeDynamic.NewSimpleDynamicClient(runtime.NewScheme()),
	}

	_, cleanup, _, err := discoverer.Discover(
		context.TODO(),
		KubernertesDiscoveryOptions{
			Namespace: "tsuru",
		},
		&grpc_build_v1.BuildRequest{
			App: nil,
		},
		os.Stdout,
	)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "there's only support for discovering BuildKit pods from Tsuru apps")
	cleanup()
}

func TestK8sDiscoverer_DiscoverWithTimeout(t *testing.T) {
	buildKitPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "tsuru",
			Labels: map[string]string{
				"app": "test-app",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			PodIP: "127.0.0.1",
		},
	}
	fakeClient := fake.NewSimpleClientset(buildKitPod)

	fakeClient.PrependWatchReactor("*", func(action kuberntesTesting.Action) (handled bool, ret watch.Interface, err error) {
		watcher := watch.NewFake()
		return true, watcher, nil
	})

	fakeDynamicClient := fakeDynamic.NewSimpleDynamicClient(runtime.NewScheme())

	discoverer := K8sDiscoverer{
		KubernetesInterface: fakeClient,
		DynamicInterface:    fakeDynamicClient,
	}

	_, _, _, err := discoverer.Discover(
		context.TODO(),
		KubernertesDiscoveryOptions{
			PodSelector:      "app=test-app",
			Namespace:        "tsuru",
			Timeout:          time.Millisecond * 100,
			SetTsuruAppLabel: false,
		},
		&grpc_build_v1.BuildRequest{
			App: &grpc_build_v1.TsuruApp{
				Name: "test-app",
			},
		},
		os.Stdout,
	)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max deadline of 100ms exceeded to discover BuildKit pod")
}

func TestK8sDiscoverer_DiscoverWithStatefulsetInitialUpscale(t *testing.T) {
	t.Run("Should upscale to one replica if there are no replicas on statefulset spec", func(t *testing.T) {
		buildKitPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "buildkit-0",
				Namespace: "tsuru",
				Labels: map[string]string{
					"app": "buildkit",
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

		statefulset := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "buildkit",
				Namespace: "tsuru",
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: ptr.To(int32(0)),
			},
		}

		fakeClient := fake.NewSimpleClientset(buildKitPod, statefulset)

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

		var buf bytes.Buffer
		_, _, _, err := discoverer.Discover(
			context.TODO(),
			KubernertesDiscoveryOptions{
				PodSelector:      "app=buildkit",
				Namespace:        "tsuru",
				Timeout:          time.Second * 2,
				Statefulset:      "buildkit",
				SetTsuruAppLabel: false,
			},
			&grpc_build_v1.BuildRequest{
				App: &grpc_build_v1.TsuruApp{
					Name: "test-app",
				},
			},
			&buf,
		)

		assert.NoError(t, err)
		assert.Contains(t, buf.String(), "There is no buildkits available, scaling to one replica")

		sts, err := fakeClient.AppsV1().StatefulSets("tsuru").Get(context.TODO(), "buildkit", metav1.GetOptions{})
		assert.NoError(t, err)
		assert.Equal(t, int32(1), *sts.Spec.Replicas)
	})

	t.Run("Should not upscale if scaling is disabled", func(t *testing.T) {
		buildKitPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "buildkit-0",
				Namespace: "tsuru",
				Labels: map[string]string{
					"app": "buildkit",
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

		statefulset := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "buildkit",
				Namespace: "tsuru",
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: ptr.To(int32(0)),
			},
		}

		fakeClient := fake.NewSimpleClientset(buildKitPod, statefulset)

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

		var buf bytes.Buffer
		_, _, _, err := discoverer.Discover(
			context.TODO(),
			KubernertesDiscoveryOptions{
				PodSelector:      "app=buildkit",
				Namespace:        "tsuru",
				Timeout:          time.Second * 2,
				Statefulset:      "buildkit",
				ScalingDisabled:  true,
				SetTsuruAppLabel: false,
			},
			&grpc_build_v1.BuildRequest{
				App: &grpc_build_v1.TsuruApp{
					Name: "test-app",
				},
			},
			&buf,
		)

		assert.NoError(t, err)
		assert.NotContains(t, buf.String(), "There is no buildkits available, scaling to one replica")

		sts, err := fakeClient.AppsV1().StatefulSets("tsuru").Get(context.TODO(), "buildkit", metav1.GetOptions{})
		assert.NoError(t, err)
		assert.Equal(t, int32(0), *sts.Spec.Replicas)
	})
}

func TestK8sDiscoverer_BuildkitPodNamespace(t *testing.T) {
	t.Run("use provided namespace when UseSameNamespaceAsApp is false", func(t *testing.T) {
		opts := KubernertesDiscoveryOptions{
			Namespace:             "default",
			UseSameNamespaceAsApp: false,
		}

		discoverer := K8sDiscoverer{
			KubernetesInterface: fake.NewSimpleClientset(),
			DynamicInterface:    fakeDynamic.NewSimpleDynamicClient(runtime.NewScheme()),
		}

		ns, err := discoverer.buildkitPodNamespace(context.TODO(), opts, "test-app")

		assert.NoError(t, err)
		assert.Equal(t, "default", ns)
	})

	t.Run("discover namespace from tsuru app", func(t *testing.T) {
		tsuruApp := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "tsuru.io/v1",
				"kind":       "App",
				"metadata": map[string]interface{}{
					"name":      "test-app",
					"namespace": "tsuru",
				},
				"spec": map[string]interface{}{
					"namespaceName": "custom-namespace",
				},
			},
		}

		opts := KubernertesDiscoveryOptions{
			UseSameNamespaceAsApp: true,
		}

		discoverer := K8sDiscoverer{
			KubernetesInterface: fake.NewSimpleClientset(),
			DynamicInterface:    fakeDynamic.NewSimpleDynamicClient(runtime.NewScheme(), tsuruApp),
		}

		ns, err := discoverer.buildkitPodNamespace(context.TODO(), opts, "test-app")

		assert.NoError(t, err)
		assert.Equal(t, "custom-namespace", ns)
	})

	t.Run("error when tsuru app not found", func(t *testing.T) {
		opts := KubernertesDiscoveryOptions{
			UseSameNamespaceAsApp: true,
		}

		discoverer := K8sDiscoverer{
			KubernetesInterface: fake.NewSimpleClientset(),
			DynamicInterface:    fakeDynamic.NewSimpleDynamicClient(runtime.NewScheme()),
		}

		_, err := discoverer.buildkitPodNamespace(context.TODO(), opts, "nonexistent-app")

		assert.Error(t, err)
	})
}

func TestIsPodReady(t *testing.T) {
	t.Run("pod is ready", func(t *testing.T) {
		pod := &corev1.Pod{
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

		result := isPodReady(pod)
		assert.True(t, result)
	})

	t.Run("pod is not ready - condition false", func(t *testing.T) {
		pod := &corev1.Pod{
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				PodIP: "127.0.0.1",
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionFalse,
					},
				},
			},
		}

		result := isPodReady(pod)
		assert.False(t, result)
	})

	t.Run("pod is not running", func(t *testing.T) {
		pod := &corev1.Pod{
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				PodIP: "127.0.0.1",
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		result := isPodReady(pod)
		assert.False(t, result)
	})

	t.Run("pod has no IP", func(t *testing.T) {
		pod := &corev1.Pod{
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				PodIP: "",
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		result := isPodReady(pod)
		assert.False(t, result)
	})

	t.Run("pod has no ready condition", func(t *testing.T) {
		pod := &corev1.Pod{
			Status: corev1.PodStatus{
				Phase:      corev1.PodRunning,
				PodIP:      "127.0.0.1",
				Conditions: []corev1.PodCondition{},
			},
		}

		result := isPodReady(pod)
		assert.False(t, result)
	})
}

func TestNormalizeAppLabelForJSONPatch(t *testing.T) {
	t.Run("string with forward slash", func(t *testing.T) {
		result := normalizeAppLabelForJSONPatch("tsuru.io/app-name")
		assert.Equal(t, "tsuru.io~1app-name", result)
	})

	t.Run("string with tilde", func(t *testing.T) {
		result := normalizeAppLabelForJSONPatch("test~app")
		assert.Equal(t, "test~0app", result)
	})

	t.Run("string with both tilde and slash", func(t *testing.T) {
		result := normalizeAppLabelForJSONPatch("test~/app")
		assert.Equal(t, "test~0~1app", result)
	})

	t.Run("string without special characters", func(t *testing.T) {
		result := normalizeAppLabelForJSONPatch("simple-label")
		assert.Equal(t, "simple-label", result)
	})

	t.Run("empty string", func(t *testing.T) {
		result := normalizeAppLabelForJSONPatch("")
		assert.Equal(t, "", result)
	})
}

func TestSetTsuruAppLabelOnBuildKitPod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "buildkit-0",
			Namespace: "tsuru",
			Labels: map[string]string{
				metadata.TsuruAppNameLabelKey: "",
				metadata.TsuruAppTeamLabelKey: "",
				metadata.TsuruIsBuildLabelKey: "",
			},
			Annotations: map[string]string{
				metadata.DeployAgentLastBuildEndingTimeLabelKey: "",
				metadata.DeployAgentLastBuildStartingLabelKey:   "",
			},
		},
	}

	fakeClient := fake.NewSimpleClientset(pod)

	app := &grpc_build_v1.TsuruApp{
		Name: "test-app",
		Team: "test-team",
	}

	err := setTsuruAppLabelOnBuildKitPod(context.TODO(), fakeClient, pod.Name, pod.Namespace, app)
	require.NoError(t, err)

	actions := fakeClient.Actions()
	require.Len(t, actions, 1)
	patchAction := actions[0].(kuberntesTesting.PatchAction)

	assert.Equal(t, "buildkit-0", patchAction.GetName())
	assert.Equal(t, types.JSONPatchType, patchAction.GetPatchType())

	var patchOps []map[string]interface{}
	err = json.Unmarshal(patchAction.GetPatch(), &patchOps)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(patchOps), 4, "should have at least 4 patch operations")

	foundAppName := false
	foundIsBuild := false
	foundEndingTime := false
	foundStartingTime := false

	for _, op := range patchOps {
		switch op["path"] {
		case "/metadata/labels/tsuru.io~1app-name":
			assert.Equal(t, "replace", op["op"])
			assert.Equal(t, "test-app", op["value"])
			foundAppName = true
		case "/metadata/labels/tsuru.io~1is-build":
			assert.Equal(t, "replace", op["op"])
			assert.Equal(t, "true", op["value"])
			foundIsBuild = true
		case "/metadata/annotations/deploy-agent.tsuru.io~1last-build-ending-time":
			assert.Equal(t, "replace", op["op"])
			assert.Equal(t, "", op["value"])
			foundEndingTime = true
		case "/metadata/annotations/deploy-agent.tsuru.io~1last-build-starting-time":
			assert.Equal(t, "replace", op["op"])
			assert.NotEmpty(t, op["value"])
			foundStartingTime = true
		}
	}

	assert.True(t, foundAppName, "app name label should be set")
	assert.True(t, foundIsBuild, "is-build label should be set")
	assert.True(t, foundEndingTime, "ending time annotation should be set")
	assert.True(t, foundStartingTime, "starting time annotation should be set")
}

func TestUnsetTsuruAppLabelOnBuildKitPod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "buildkit-0",
			Namespace: "tsuru",
			Labels: map[string]string{
				metadata.TsuruAppNameLabelKey: "test-app",
				metadata.TsuruAppTeamLabelKey: "test-team",
				metadata.TsuruIsBuildLabelKey: "true",
			},
			Annotations: map[string]string{
				metadata.DeployAgentLastBuildEndingTimeLabelKey: "",
			},
		},
	}

	fakeClient := fake.NewSimpleClientset(pod)

	err := unsetTsuruAppLabelOnBuildKitPod(context.TODO(), fakeClient, pod.Name, pod.Namespace)
	require.NoError(t, err)

	actions := fakeClient.Actions()
	require.Len(t, actions, 1)
	patchAction := actions[0].(kuberntesTesting.PatchAction)

	assert.Equal(t, "buildkit-0", patchAction.GetName())
	assert.Equal(t, types.JSONPatchType, patchAction.GetPatchType())

	var patchOps []map[string]interface{}
	err = json.Unmarshal(patchAction.GetPatch(), &patchOps)
	require.NoError(t, err)

	assert.Len(t, patchOps, 4, "should have 4 patch operations")

	foundRemoveAppName := false
	foundRemoveTeam := false
	foundRemoveBuild := false
	foundReplaceEndingTime := false

	for _, op := range patchOps {
		switch op["path"] {
		case "/metadata/labels/tsuru.io~1app-name":
			assert.Equal(t, "remove", op["op"])
			foundRemoveAppName = true
		case "/metadata/labels/tsuru.io~1app-team":
			assert.Equal(t, "remove", op["op"])
			foundRemoveTeam = true
		case "/metadata/labels/tsuru.io~1is-build":
			assert.Equal(t, "remove", op["op"])
			foundRemoveBuild = true
		case "/metadata/annotations/deploy-agent.tsuru.io~1last-build-ending-time":
			assert.Equal(t, "replace", op["op"])
			assert.NotEmpty(t, op["value"])
			foundReplaceEndingTime = true
		}
	}

	assert.True(t, foundRemoveAppName, "app name label should be removed")
	assert.True(t, foundRemoveTeam, "team label should be removed")
	assert.True(t, foundRemoveBuild, "is-build label should be removed")
	assert.True(t, foundReplaceEndingTime, "ending time annotation should be replaced")
}

func TestCleanUps(t *testing.T) {
	var callOrder []int

	fn1 := func() { callOrder = append(callOrder, 1) }
	fn2 := func() { callOrder = append(callOrder, 2) }
	fn3 := func() { callOrder = append(callOrder, 3) }

	cleanup := cleanUps(fn1, fn2, fn3)
	cleanup()

	assert.Equal(t, []int{3, 2, 1}, callOrder, "cleanup functions should be called in reverse order")
}

func TestCleanUpsWithNil(t *testing.T) {
	var called bool

	fn1 := func() { called = true }

	cleanup := cleanUps(fn1, nil)
	cleanup()

	assert.True(t, called, "non-nil cleanup function should be called")
}
