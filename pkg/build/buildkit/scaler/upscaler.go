package scaler

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/tsuru/deploy-agent/pkg/build/metadata"
	"k8s.io/client-go/kubernetes"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func MayUpscale(ctx context.Context, cs kubernetes.Interface, ns, statefulset string, w io.Writer) error {
	stfullset, err := cs.AppsV1().StatefulSets(ns).Get(ctx, statefulset, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if stfullset.Spec.Replicas != nil && *stfullset.Spec.Replicas > 0 {
		return nil
	}

	wantedReplicas := int32(1)

	if lastReplicas := stfullset.Annotations[metadata.DeployAgentLastReplicasAnnotationKey]; lastReplicas != "" {
		replicas, err := strconv.ParseInt(lastReplicas, 10, 32)
		if err != nil {
			return err
		}
		wantedReplicas = int32(replicas)
	}

	fmt.Fprintln(w, "There is no buildkits available, scaling to one replica")
	stfullset.Spec.Replicas = &wantedReplicas

	_, err = cs.AppsV1().StatefulSets(ns).Update(ctx, stfullset, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return nil
}
