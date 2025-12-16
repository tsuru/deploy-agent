// Copyright 2025 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package autodiscovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/moby/buildkit/client"
	"github.com/tsuru/deploy-agent/pkg/build/buildkit/metrics"
	"github.com/tsuru/deploy-agent/pkg/build/buildkit/scaler"
	pb "github.com/tsuru/deploy-agent/pkg/build/grpc_build_v1"
	"github.com/tsuru/deploy-agent/pkg/build/metadata"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

var (
	noopCleaner = func() {}

	tsuruAppGVR = schema.GroupVersionResource{
		Group:    "tsuru.io",
		Version:  "v1",
		Resource: "apps",
	}
)

type KubernertesDiscoveryOptions struct {
	PodSelector           string
	Namespace             string
	LeasePrefix           string
	Statefulset           string
	Port                  int
	ScalingDisabled       bool
	UseSameNamespaceAsApp bool
	SetTsuruAppLabel      bool
	ScaleGracefulPeriod   time.Duration
	Timeout               time.Duration
}

type K8sDiscoverer struct {
	KubernetesInterface kubernetes.Interface
	DynamicInterface    dynamic.Interface
}

func (d *K8sDiscoverer) Discover(ctx context.Context, opts KubernertesDiscoveryOptions, req *pb.BuildRequest, w io.Writer) (*client.Client, func(), string, error) {
	if req.App == nil {
		return nil, noopCleaner, "", fmt.Errorf("there's only support for discovering BuildKit pods from Tsuru apps")
	}

	ns, err := d.buildkitPodNamespace(ctx, opts, req.App.Name)
	if err != nil {
		return nil, noopCleaner, "", err
	}

	client, cleaner, err := d.discoverBuildKitClientFromApp(ctx, opts, req.App, ns, w)
	if err != nil {
		return nil, noopCleaner, ns, err
	}
	return client, cleaner, ns, nil
}

func (d *K8sDiscoverer) discoverBuildKitClientFromApp(ctx context.Context, opts KubernertesDiscoveryOptions, app *pb.TsuruApp, namespace string, w io.Writer) (*client.Client, func(), error) {
	leaderCtx, leaderCancel := context.WithCancel(ctx)
	cfns := []func(){
		func() {
			klog.V(4).Infoln("Releasing the main leader lease...")
			leaderCancel()
		},
	}

	pod, err := d.discoverBuildKitPod(leaderCtx, opts, namespace, w)
	if err != nil {
		return nil, cleanUps(cfns...), err
	}

	if opts.SetTsuruAppLabel {
		klog.V(4).Infoln("Setting Tsuru app labels in the pod", pod.Name)

		err = setTsuruAppLabelOnBuildKitPod(ctx, d.KubernetesInterface, pod.Name, pod.Namespace, app)
		if err != nil {
			return nil, cleanUps(cfns...), fmt.Errorf("failed to set Tsuru app labels on BuildKit's pod: %w", err)
		}

		cfns = append(cfns, func() {
			klog.V(4).Infoln("Removing Tsuru app labels in the pod", pod.Name)
			nerr := unsetTsuruAppLabelOnBuildKitPod(ctx, d.KubernetesInterface, pod.Name, pod.Namespace)
			if nerr != nil {
				klog.Errorf("failed to unset Tsuru app labels: %s", nerr)
			}
		})
	}

	addr := fmt.Sprintf("tcp://%s:%d", pod.Status.PodIP, opts.Port)

	c, err := client.New(ctx, addr, client.WithFailFast())
	if err != nil {
		return nil, cleanUps(cfns...), err
	}

	cfns = append(cfns, func() {
		klog.V(4).Infoln("Closing connection with BuildKit at", addr)
		c.Close()
	})

	klog.V(4).Infoln("Connecting to BuildKit at", addr)

	return c, cleanUps(cfns...), nil
}

func (d *K8sDiscoverer) discoverBuildKitPod(ctx context.Context, opts KubernertesDiscoveryOptions, namespace string, w io.Writer) (*corev1.Pod, error) {
	deadlineCtx, deadlineCancel := context.WithCancel(ctx)
	defer deadlineCancel()

	metrics.BuildsWaitingForLease.WithLabelValues(namespace).Inc()
	defer metrics.BuildsWaitingForLease.WithLabelValues(namespace).Dec()

	if opts.Statefulset != "" && !opts.ScalingDisabled {
		err := scaler.MayUpscale(ctx, d.KubernetesInterface, namespace, opts.Statefulset, w)
		if err != nil {
			return nil, fmt.Errorf("failed trying upscale BuildKit statefulset(%s - %s): %w", namespace, opts.Statefulset, err)
		}
	}

	watchCtx, watchCancel := context.WithCancel(deadlineCtx)
	defer watchCancel()

	podWatcher, err := d.KubernetesInterface.CoreV1().Pods(namespace).Watch(watchCtx, metav1.ListOptions{
		LabelSelector: opts.PodSelector,
		Watch:         true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create pod watcher: %w", err)
	}

	notifier, leasablePodsCh := newPodNotifier(podWatcher)
	go notifier.notify(watchCtx, isPodReady)

	leaser, leasedPodsCh, err := newLeaser(d.KubernetesInterface, leasablePodsCh)
	if err != nil {
		return nil, fmt.Errorf("failed to create pod leaser: %w", err)
	}
	go leaser.acquireLeaseForAllPods(deadlineCtx, opts)

	for {
		select {
		case <-time.After(opts.Timeout):
			go leaser.releaseAll()
			return nil, fmt.Errorf("max deadline of %s exceeded to discover BuildKit pod", opts.Timeout)
		case leasedPod, ok := <-leasedPodsCh:
			if !ok {
				go leaser.releaseAll()
				return nil, fmt.Errorf("leased pods channel was closed before acquiring any lease")
			}
			go leaser.releaseAll(releaseOptions{except: leasedPod.Name})
			return leasedPod, nil
		}
	}
}

func (d *K8sDiscoverer) buildkitPodNamespace(ctx context.Context, opts KubernertesDiscoveryOptions, app string) (string, error) {
	if !opts.UseSameNamespaceAsApp {
		return opts.Namespace, nil
	}

	klog.V(4).Infof("Discovering the namespace where app %s is running on...", app)

	tsuruApp, err := d.DynamicInterface.Resource(tsuruAppGVR).Namespace(metadata.TsuruAppNamespace).Get(ctx, app, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	// See more about App resource at: https://github.com/tsuru/tsuru/blob/main/provision/kubernetes/pkg/apis/tsuru/v1/types.go#L24
	ns, found, err := unstructured.NestedString(tsuruApp.Object, "spec", "namespaceName")
	if err != nil {
		return "", err
	}

	if !found {
		return "", fmt.Errorf("failed to fetch namespace in the App resource")
	}

	klog.V(4).Infof("App %s is running on namespace %s...", app, ns)

	return ns, nil
}

func isPodReady(pod *corev1.Pod) bool {
	var ready bool
	for _, c := range pod.Status.Conditions {
		if c.Type != corev1.PodReady {
			continue
		}

		ready = c.Status == corev1.ConditionTrue
	}

	return pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" && ready
}

func setTsuruAppLabelOnBuildKitPod(ctx context.Context, cs kubernetes.Interface, pod, ns string, app *pb.TsuruApp) error {
	changes := []any{
		map[string]any{
			"op":    "replace",
			"path":  fmt.Sprintf("/metadata/labels/%s", normalizeAppLabelForJSONPatch(metadata.TsuruAppNameLabelKey)),
			"value": app.Name,
		},
		map[string]any{
			"op":    "replace",
			"path":  fmt.Sprintf("/metadata/labels/%s", normalizeAppLabelForJSONPatch(metadata.TsuruIsBuildLabelKey)),
			"value": strconv.FormatBool(true),
		},
		map[string]any{
			"op":    "replace",
			"path":  fmt.Sprintf("/metadata/annotations/%s", normalizeAppLabelForJSONPatch(metadata.DeployAgentLastBuildEndingTimeLabelKey)),
			"value": "", // set annotation value to empty rather than removing it, since it might not exist at first run
		},
		map[string]any{
			"op":    "replace",
			"path":  fmt.Sprintf("/metadata/annotations/%s", normalizeAppLabelForJSONPatch(metadata.DeployAgentLastBuildStartingLabelKey)),
			"value": strconv.FormatInt(time.Now().Unix(), 10),
		},
	}

	if app.Team != "" {
		changes = append(changes, map[string]any{
			"op":    "replace",
			"path":  fmt.Sprintf("/metadata/labels/%s", normalizeAppLabelForJSONPatch(metadata.TsuruAppTeamLabelKey)),
			"value": app.Team,
		})
	}

	patch, err := json.Marshal(changes)
	if err != nil {
		return err
	}

	_, err = cs.CoreV1().Pods(ns).Patch(ctx, pod, types.JSONPatchType, patch, metav1.PatchOptions{})
	return err
}

func unsetTsuruAppLabelOnBuildKitPod(ctx context.Context, cs kubernetes.Interface, pod, ns string) error {
	patch, err := json.Marshal([]any{
		map[string]any{
			"op":   "remove",
			"path": fmt.Sprintf("/metadata/labels/%s", normalizeAppLabelForJSONPatch(metadata.TsuruAppNameLabelKey)),
		},
		map[string]any{
			"op":   "remove",
			"path": fmt.Sprintf("/metadata/labels/%s", normalizeAppLabelForJSONPatch(metadata.TsuruAppTeamLabelKey)),
		},
		map[string]any{
			"op":   "remove",
			"path": fmt.Sprintf("/metadata/labels/%s", normalizeAppLabelForJSONPatch(metadata.TsuruIsBuildLabelKey)),
		},
		map[string]any{
			"op":    "replace",
			"path":  fmt.Sprintf("/metadata/annotations/%s", normalizeAppLabelForJSONPatch(metadata.DeployAgentLastBuildEndingTimeLabelKey)),
			"value": strconv.FormatInt(time.Now().Unix(), 10),
		},
	})
	if err != nil {
		return err
	}

	_, err = cs.CoreV1().Pods(ns).Patch(ctx, pod, types.JSONPatchType, patch, metav1.PatchOptions{})
	return err
}

func normalizeAppLabelForJSONPatch(s string) string {
	// Replaces ~ and / by ~0 and ~1, respectively
	// See: https://datatracker.ietf.org/doc/html/rfc6902/#appendix-A.14
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

func cleanUps(fns ...func()) func() {
	return func() {
		for i := range fns {
			fn := fns[(len(fns) - i - 1)]
			if fn == nil {
				continue
			}

			fn()
		}
	}
}
