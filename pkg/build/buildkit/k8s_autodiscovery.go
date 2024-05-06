// Copyright 2023 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package buildkit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/moby/buildkit/client"
	pb "github.com/tsuru/deploy-agent/pkg/build/grpc_build_v1"
	"github.com/tsuru/deploy-agent/pkg/build/metadata"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog"
)

var (
	noopFunc = func() {}

	tsuruAppGVR = schema.GroupVersionResource{
		Group:    "tsuru.io",
		Version:  "v1",
		Resource: "apps",
	}
)

type k8sDiscoverer struct {
	cs  *kubernetes.Clientset
	dcs dynamic.Interface
}

func (d *k8sDiscoverer) Discover(ctx context.Context, opts KubernertesDiscoveryOptions, req *pb.BuildRequest, w io.Writer) (*client.Client, func(), error) {
	if req.App == nil {
		return nil, noopFunc, fmt.Errorf("there's only support for discovering BuildKit pods from Tsuru apps")
	}

	return d.discoverBuildKitClientFromApp(ctx, opts, req.App.Name, w)
}

func (d *k8sDiscoverer) discoverBuildKitClientFromApp(ctx context.Context, opts KubernertesDiscoveryOptions, app string, w io.Writer) (*client.Client, func(), error) {
	leaderCtx, leaderCancel := context.WithCancel(ctx)
	cfns := []func(){
		func() {
			klog.V(4).Infoln("Releasing the main leader lease...")
			leaderCancel()
		},
	}

	pod, err := d.discoverBuildKitPod(leaderCtx, opts, app, w)
	if err != nil {
		return nil, cleanUps(cfns...), err
	}

	if opts.SetTsuruAppLabel {
		klog.V(4).Infoln("Setting Tsuru app labels in the pod", pod.Name)

		err = setTsuruAppLabelOnBuildKitPod(ctx, d.cs, pod.Name, pod.Namespace, app)
		if err != nil {
			return nil, cleanUps(cfns...), fmt.Errorf("failed to set Tsuru app labels on BuildKit's pod: %w", err)
		}

		cfns = append(cfns, func() {
			klog.V(4).Infoln("Removing Tsuru app labels in the pod", pod.Name)
			nerr := unsetTsuruAppLabelOnBuildKitPod(ctx, d.cs, pod.Name, pod.Namespace)
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

func (d *k8sDiscoverer) discoverBuildKitPod(ctx context.Context, opts KubernertesDiscoveryOptions, app string, w io.Writer) (*corev1.Pod, error) {
	deadlineCtx, deadlineCancel := context.WithCancel(ctx)
	defer deadlineCancel()

	ns, err := d.buildkitPodNamespace(deadlineCtx, opts, app)
	if err != nil {
		return nil, err
	}

	errCh := make(chan error, 1)
	defer close(errCh)

	pods := make(chan *corev1.Pod)
	defer close(pods)

	watchCtx, watchCancel := context.WithCancel(deadlineCtx)
	defer watchCancel() // watch cancellation must happen before than closing the pods channel

	go func() {
		nerr := watchBuildKitPods(watchCtx, d.cs, opts, ns, pods, w)
		if nerr != nil {
			errCh <- nerr
		}
	}()

	selected := make(chan *corev1.Pod, 1)
	defer close(selected)

	leaseCancelByPod := make(map[string]context.CancelFunc)

	go func() {
		for pod := range pods {
			if _, found := leaseCancelByPod[pod.Name]; found {
				continue
			}

			leaseCtx, leaseCancel := context.WithCancel(ctx)
			leaseCancelByPod[pod.Name] = leaseCancel

			go acquireLeaseForPod(leaseCtx, d.cs, selected, pod, errCh, opts)
		}
	}()

	releaseLeaderLock := func(except string) {
		for name, leaseCancel := range leaseCancelByPod {
			if except == name {
				continue
			}

			klog.V(4).Infof("Releasing lock for %s pod", name)
			leaseCancel()
		}
	}

	var pod *corev1.Pod

	select {
	case <-time.After(opts.Timeout):
		releaseLeaderLock("") // release all lease locksa
		return nil, fmt.Errorf("max deadline exceeded to discover BuildKit pod")

	case err = <-errCh:
		releaseLeaderLock("") // release all lease locks
		return nil, err

	case pod = <-selected:
	}

	defer releaseLeaderLock(pod.Name) // release all lease locks except from the chosen pod

	return pod, nil
}

func (d *k8sDiscoverer) buildkitPodNamespace(ctx context.Context, opts KubernertesDiscoveryOptions, app string) (string, error) {
	if !opts.UseSameNamespaceAsApp {
		return opts.Namespace, nil
	}

	klog.V(4).Infof("Discovering the namespace where app %s is running on...", app)

	tsuruApp, err := d.dcs.Resource(tsuruAppGVR).Namespace(metadata.TsuruAppNamespace).Get(ctx, app, metav1.GetOptions{})
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

func watchBuildKitPods(ctx context.Context, cs *kubernetes.Clientset, opts KubernertesDiscoveryOptions, ns string, pods chan<- *corev1.Pod, writer io.Writer) error {
	if opts.ScaleStatefulset != "" {
		stfullset, err := cs.AppsV1().StatefulSets(ns).Get(ctx, opts.ScaleStatefulset, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if stfullset.Spec.Replicas == nil || *stfullset.Spec.Replicas == 0 {
			fmt.Fprintln(writer, "There is no buildkits available, scaling to one replica")
			wantedReplicas := int32(1)
			stfullset.Spec.Replicas = &wantedReplicas

			_, err := cs.AppsV1().StatefulSets(ns).Update(ctx, stfullset, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		}
	}

	w, err := cs.CoreV1().Pods(ns).Watch(ctx, metav1.ListOptions{
		LabelSelector: opts.PodSelector,
		Watch:         true,
	})
	if err != nil {
		return err
	}

	defer w.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case e := <-w.ResultChan():
			if e.Type != watch.Added && e.Type != watch.Modified {
				continue
			}

			pod := e.Object.(*corev1.Pod)
			if isPodReady(pod) {
				pods <- pod
			}

			klog.V(4).Infof("Pod %s/%s is not ready yet", pod.Namespace, pod.Name)
		}
	}
}

func acquireLeaseForPod(ctx context.Context, cs *kubernetes.Clientset, ch chan<- *corev1.Pod, pod *corev1.Pod, errCh chan<- error, opts KubernertesDiscoveryOptions) {
	podname := os.Getenv("POD_NAME")
	if podname == "" {
		hostname, err := os.Hostname()
		if err != nil {
			errCh <- err
		}

		podname = hostname
	}

	uniqueHolderName := fmt.Sprintf("%s-%d", podname, time.Now().Unix())

	klog.V(4).Infof("Attempting to acquire the lease for pod %s/%s under holder name %q...", pod.Namespace, pod.Name, uniqueHolderName)

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", strings.TrimRight(opts.LeasePrefix, "-"), pod.Name),
				Namespace: pod.Namespace,
			},
			Client: cs.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: uniqueHolderName,
			},
		},
		ReleaseOnCancel: true,
		LeaseDuration:   5 * time.Second,
		RenewDeadline:   2 * time.Second,
		RetryPeriod:     500 * time.Millisecond,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(c context.Context) {
				select {
				case ch <- pod:
					klog.V(4).Infof("Selected BuildKit pod: %s/%s", pod.Namespace, pod.Name)

				case <-ctx.Done():
					klog.V(4).Infof("Received context cancelation: %s/%s", pod.Namespace, pod.Name)
				}
			},
			OnStoppedLeading: noopFunc,
		},
	})

	klog.V(4).Infof("Shutting off the lease for %s/%s pod", pod.Namespace, pod.Name)
}

func setTsuruAppLabelOnBuildKitPod(ctx context.Context, cs *kubernetes.Clientset, pod, ns, app string) error {
	patch, err := json.Marshal([]any{
		map[string]any{
			"op":    "replace",
			"path":  fmt.Sprintf("/metadata/labels/%s", normalizeAppLabelForJSONPatch(metadata.TsuruAppNameLabelKey)),
			"value": app,
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
	})
	if err != nil {
		return err
	}

	_, err = cs.CoreV1().Pods(ns).Patch(ctx, pod, types.JSONPatchType, patch, metav1.PatchOptions{})
	return err
}

func unsetTsuruAppLabelOnBuildKitPod(ctx context.Context, cs *kubernetes.Clientset, pod, ns string) error {
	patch, err := json.Marshal([]any{
		map[string]any{
			"op":   "remove",
			"path": fmt.Sprintf("/metadata/labels/%s", normalizeAppLabelForJSONPatch(metadata.TsuruAppNameLabelKey)),
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
