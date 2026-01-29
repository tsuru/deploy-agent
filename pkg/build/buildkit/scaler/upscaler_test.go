// Copyright 2026 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package scaler

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsuru/deploy-agent/pkg/build/metadata"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

func TestMayScaleStatefulsetSkipScale(t *testing.T) {
	ctx := context.Background()

	cli := fake.NewSimpleClientset(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "buildkit",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To(int32(10)),
		},
	})

	buf := bytes.Buffer{}

	err := MayUpscale(ctx, cli, "default", "buildkit", &buf)

	assert.Equal(t, "", buf.String())
	assert.NoError(t, err)
}
func TestMayScaleStatefulsetScale(t *testing.T) {
	ctx := context.Background()

	cli := fake.NewSimpleClientset(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "buildkit",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To(int32(0)),
		},
	})

	buf := bytes.Buffer{}

	err := MayUpscale(ctx, cli, "default", "buildkit", &buf)

	assert.Equal(t, "There is no buildkits available, scaling to one replica\n", buf.String())
	assert.NoError(t, err)

	rs, err := cli.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{})
	assert.NoError(t, err)

	require.Len(t, rs.Items, 1)
	assert.Equal(t, int32(1), *rs.Items[0].Spec.Replicas)
}

func TestMayScaleStatefulsetScaleFromPreviousReplicas(t *testing.T) {
	ctx := context.Background()

	cli := fake.NewSimpleClientset(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "buildkit",
			Namespace: "default",

			Annotations: map[string]string{
				metadata.DeployAgentLastReplicasAnnotationKey: "3",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To(int32(0)),
		},
	})

	buf := bytes.Buffer{}

	err := MayUpscale(ctx, cli, "default", "buildkit", &buf)

	assert.Equal(t, "There is no buildkits available, scaling to one replica\n", buf.String())
	assert.NoError(t, err)

	rs, err := cli.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{})
	assert.NoError(t, err)

	require.Len(t, rs.Items, 1)
	assert.Equal(t, int32(3), *rs.Items[0].Spec.Replicas)
}
