/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package platform

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// Plain unit tests of the image pre-puller: a SessionManager goes through
// renderSessionManagerValues, the session-manager chart is rendered with
// the in-memory Helm client, and the tests look at the image-puller
// DaemonSet's pod.

// renderImagePullerPod renders the session-manager chart for obj and
// returns the image-puller DaemonSet's pod spec.
func renderImagePullerPod(t *testing.T, obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig) corev1.PodSpec {
	t.Helper()

	values := renderSessionManagerValues(obj, cfg, testOperatorNamespace)
	for _, w := range renderWorkloads(t, vendoredcharts.SessionManager, values) {
		if w.Kind == "DaemonSet" && w.Metadata.Name == "image-puller" {
			return w.Spec.Template.Spec
		}
	}
	t.Fatal("no image-puller DaemonSet in the rendered session-manager chart")
	return corev1.PodSpec{}
}

// sessionManagerAppVersion is the application version of the embedded
// session-manager chart, the tag of the Educates images it defaults.
func sessionManagerAppVersion(t *testing.T) string {
	t.Helper()

	chrt, err := vendoredcharts.SessionManager()
	if err != nil {
		t.Fatalf("load session-manager chart: %v", err)
	}
	return chrt.Metadata.AppVersion
}

// relocatedClusterConfig is the test cluster config with Educates images
// relocated under registry.example.com/mirror.
func relocatedClusterConfig() *configv1alpha1.EducatesClusterConfig {
	cfg := valuesTestClusterConfig()
	cfg.Status.ImageRegistry = &configv1alpha1.ImageRegistry{Prefix: "registry.example.com/mirror"}
	return cfg
}

// mountPathOf returns where c mounts the volume named volume, or "" when
// it does not mount it.
func mountPathOf(c corev1.Container, volume string) string {
	for _, m := range c.VolumeMounts {
		if m.Name == volume {
			return m.MountPath
		}
	}
	return ""
}

func TestSessionManagerChart_ImagePullerRunsTheHelperInPlaceOfThePulledImages(t *testing.T) {
	obj := &platformv1alpha1.SessionManager{
		Spec: platformv1alpha1.SessionManagerSpec{
			ImagePrePuller: &platformv1alpha1.ImagePrePuller{Enabled: true},
		},
	}
	version := sessionManagerAppVersion(t)
	helperImage := "registry.example.com/mirror/educates-image-puller:" + version

	pod := renderImagePullerPod(t, obj, relocatedClusterConfig())

	// One shared emptyDir volume carries the helper to the pulled images.
	var shared string
	for _, v := range pod.Volumes {
		if v.EmptyDir != nil {
			if shared != "" {
				t.Fatalf("pod has emptyDir volumes %q and %q, want one", shared, v.Name)
			}
			shared = v.Name
		}
	}
	if shared == "" {
		t.Fatalf("pod volumes %v hold no emptyDir", pod.Volumes)
	}

	if len(pod.InitContainers) == 0 {
		t.Fatal("pod has no init containers")
	}

	// The first init container copies the helper into the volume.
	copier := pod.InitContainers[0]
	if copier.Image != helperImage {
		t.Errorf("first init container image = %q, want %q", copier.Image, helperImage)
	}
	copyDir := mountPathOf(copier, shared)
	if copyDir == "" {
		t.Errorf("first init container does not mount the shared volume %q", shared)
	}
	if want := []string{"/image-puller", "copy", copyDir}; !reflect.DeepEqual(copier.Command, want) {
		t.Errorf("first init container command = %q, want %q", copier.Command, want)
	}

	// Every other init container pulls one image and runs the helper's
	// exit role from the volume, nothing from the image itself.
	var pulled []string
	for _, c := range pod.InitContainers[1:] {
		pulled = append(pulled, c.Image)
		dir := mountPathOf(c, shared)
		if dir == "" {
			t.Errorf("init container %q (%s) does not mount the shared volume %q", c.Name, c.Image, shared)
			continue
		}
		if want := []string{dir + "/image-puller", "exit"}; !reflect.DeepEqual(c.Command, want) {
			t.Errorf("init container %q (%s) command = %q, want %q", c.Name, c.Image, c.Command, want)
		}
	}
	wantPulled := []string{
		"registry.example.com/mirror/educates-training-portal:" + version,
		"registry.example.com/mirror/educates-base-environment:" + version,
	}
	if !reflect.DeepEqual(pulled, wantPulled) {
		t.Errorf("pulled images = %q, want %q", pulled, wantPulled)
	}

	// The long-running container is the helper's wait role.
	if len(pod.Containers) != 1 {
		t.Fatalf("pod has %d containers, want 1", len(pod.Containers))
	}
	main := pod.Containers[0]
	if main.Image != helperImage {
		t.Errorf("main container image = %q, want %q", main.Image, helperImage)
	}
	if want := []string{"/image-puller", "wait"}; !reflect.DeepEqual(main.Command, want) {
		t.Errorf("main container command = %q, want %q", main.Command, want)
	}

	nonRoot, uid := true, int64(1001)
	wantPodSecurity := &corev1.PodSecurityContext{RunAsNonRoot: &nonRoot, RunAsUser: &uid}
	if !reflect.DeepEqual(pod.SecurityContext, wantPodSecurity) {
		t.Errorf("pod security context = %+v, want %+v", pod.SecurityContext, wantPodSecurity)
	}
}

func TestSessionManagerChart_ImagePullerOverrideReachesTheHelperImage(t *testing.T) {
	const override = "localhost:5001/educates-image-puller:latest"
	const pauseOverride = "localhost:5001/educates-pause-container:latest"
	obj := &platformv1alpha1.SessionManager{
		Spec: platformv1alpha1.SessionManagerSpec{
			ImagePrePuller: &platformv1alpha1.ImagePrePuller{Enabled: true},
			Images: &platformv1alpha1.Images{
				Overrides: []platformv1alpha1.ImageOverride{
					{Name: "image-puller", Image: override},
					// No longer special: it lands in the inventory as an
					// unused name.
					{Name: "pause-container", Image: pauseOverride},
				},
			},
		},
	}

	pod := renderImagePullerPod(t, obj, valuesTestClusterConfig())

	if len(pod.InitContainers) == 0 || len(pod.Containers) != 1 {
		t.Fatalf("pod has %d init containers and %d containers, want the copy init container and one container",
			len(pod.InitContainers), len(pod.Containers))
	}
	for _, c := range []corev1.Container{pod.InitContainers[0], pod.Containers[0]} {
		if c.Image != override {
			t.Errorf("container %q image = %q, want the override %q", c.Name, c.Image, override)
		}
		if c.ImagePullPolicy != corev1.PullAlways {
			t.Errorf("container %q pull policy = %q, want %q for a latest tag", c.Name, c.ImagePullPolicy, corev1.PullAlways)
		}
	}
	for _, c := range append(pod.InitContainers, pod.Containers...) {
		if c.Image == pauseOverride {
			t.Errorf("container %q runs the pause-container override %q", c.Name, pauseOverride)
		}
	}
}
