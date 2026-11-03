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
	"context"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// Plain unit tests of the image pre-puller: a SessionManager goes through
// renderSessionManagerValues, the session-manager chart is rendered with
// the in-memory Helm client, and the tests look at the image-puller
// DaemonSet's pod.

// renderImagePullerDaemonSet renders the session-manager chart for obj
// and returns the image-puller DaemonSet, or nil when the chart renders
// none.
func renderImagePullerDaemonSet(t *testing.T, obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig) *renderedWorkload {
	t.Helper()

	values := renderSessionManagerValues(obj, cfg, testOperatorNamespace)
	for _, w := range renderWorkloads(t, vendoredcharts.SessionManager, values) {
		if w.Kind == "DaemonSet" && w.Metadata.Name == "image-puller" {
			return &w
		}
	}
	return nil
}

// renderImagePullerPod renders the session-manager chart for obj and
// returns the image-puller DaemonSet's pod spec.
func renderImagePullerPod(t *testing.T, obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig) corev1.PodSpec {
	t.Helper()

	w := renderImagePullerDaemonSet(t, obj, cfg)
	if w == nil {
		t.Fatal("no image-puller DaemonSet in the rendered session-manager chart")
	}
	return w.Spec.Template.Spec
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

// pulledImages returns the images the image-puller pod pulls, in order:
// every init container after the one that copies the helper.
func pulledImages(pod corev1.PodSpec) []string {
	var images []string
	if len(pod.InitContainers) > 1 {
		for _, c := range pod.InitContainers[1:] {
			images = append(images, c.Image)
		}
	}
	return images
}

// prePuller is a SessionManager with the image pre-puller enabled,
// pulling entries, with the default images included unless
// includeDefaults says otherwise.
func prePuller(includeDefaults *bool, entries ...platformv1alpha1.PrePulledImage) *platformv1alpha1.SessionManager {
	return &platformv1alpha1.SessionManager{
		Spec: platformv1alpha1.SessionManagerSpec{
			ImagePrePuller: &platformv1alpha1.ImagePrePuller{
				Enabled:              true,
				IncludeDefaultImages: includeDefaults,
				Images:               entries,
			},
		},
	}
}

func TestSessionManagerChart_ImagePullerPullsNamedImagesAfterTheDefaults(t *testing.T) {
	version := sessionManagerAppVersion(t)
	obj := prePuller(nil,
		platformv1alpha1.PrePulledImage{Name: "jdk21-environment"},
		platformv1alpha1.PrePulledImage{Name: "vcluster-internal-contour"},
	)

	got := pulledImages(renderImagePullerPod(t, obj, relocatedClusterConfig()))

	want := []string{
		"registry.example.com/mirror/educates-training-portal:" + version,
		"registry.example.com/mirror/educates-base-environment:" + version,
		"registry.example.com/mirror/educates-jdk21-environment:" + version,
		// An upstream pin, which the registry prefix does not relocate.
		"ghcr.io/projectcontour/contour:v1.30.2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pulled images = %q, want %q", got, want)
	}
}

func TestSessionManagerChart_ImagePullerDropsTheDefaultImages(t *testing.T) {
	version := sessionManagerAppVersion(t)
	obj := prePuller(new(false), platformv1alpha1.PrePulledImage{Name: "jdk21-environment"})

	got := pulledImages(renderImagePullerPod(t, obj, relocatedClusterConfig()))

	want := []string{"registry.example.com/mirror/educates-jdk21-environment:" + version}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pulled images = %q, want %q", got, want)
	}
}

func TestSessionManagerChart_ImagePullerResolvesNamesThroughTheImageOverrides(t *testing.T) {
	const (
		portalOverride = "mirror.example.com/portal:pinned"
		jdkOverride    = "mirror.example.com/jdk21:pinned"
		customImage    = "mirror.example.com/workshops/custom-base:1.0"
	)
	version := sessionManagerAppVersion(t)
	obj := prePuller(nil,
		platformv1alpha1.PrePulledImage{Name: "jdk21-environment"},
		// A name only the overrides hold.
		platformv1alpha1.PrePulledImage{Name: "custom-base"},
	)
	obj.Spec.Images = &platformv1alpha1.Images{
		Overrides: []platformv1alpha1.ImageOverride{
			{Name: "training-portal", Image: portalOverride},
			{Name: "jdk21-environment", Image: jdkOverride},
			{Name: "custom-base", Image: customImage},
		},
	}

	got := pulledImages(renderImagePullerPod(t, obj, relocatedClusterConfig()))

	want := []string{
		portalOverride,
		"registry.example.com/mirror/educates-base-environment:" + version,
		jdkOverride,
		customImage,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pulled images = %q, want %q", got, want)
	}
}

func TestSessionManagerChart_ImagePullerPullsImageReferencesAsWritten(t *testing.T) {
	const (
		// Already under the registry prefix, which must not be added again.
		mirrored = "registry.example.com/mirror/workshops/custom-base:1.0"
		upstream = "quay.io/example/tool@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	)
	obj := prePuller(new(false),
		platformv1alpha1.PrePulledImage{Image: mirrored},
		platformv1alpha1.PrePulledImage{Image: upstream},
	)

	got := pulledImages(renderImagePullerPod(t, obj, relocatedClusterConfig()))

	want := []string{mirrored, upstream}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pulled images = %q, want %q", got, want)
	}
}

func TestSessionManagerChart_ImagePullerPullsEachImageOnce(t *testing.T) {
	version := sessionManagerAppVersion(t)
	jdk := "registry.example.com/mirror/educates-jdk21-environment:" + version
	obj := prePuller(nil,
		platformv1alpha1.PrePulledImage{Name: "jdk21-environment"},
		// A default listed again, by name.
		platformv1alpha1.PrePulledImage{Name: "base-environment"},
		// The same name twice.
		platformv1alpha1.PrePulledImage{Name: "jdk21-environment"},
		// A reference a name already resolved to.
		platformv1alpha1.PrePulledImage{Image: jdk},
		platformv1alpha1.PrePulledImage{Name: "conda-environment"},
	)

	got := pulledImages(renderImagePullerPod(t, obj, relocatedClusterConfig()))

	want := []string{
		"registry.example.com/mirror/educates-training-portal:" + version,
		"registry.example.com/mirror/educates-base-environment:" + version,
		jdk,
		"registry.example.com/mirror/educates-conda-environment:" + version,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pulled images = %q, want %q", got, want)
	}
}

// installSessionManagerChart installs the session-manager chart for obj
// into a memory-backed Helm store and returns the install's error.
func installSessionManagerChart(t *testing.T, obj *platformv1alpha1.SessionManager) error {
	t.Helper()

	chrt, err := vendoredcharts.SessionManager()
	if err != nil {
		t.Fatalf("load session-manager chart: %v", err)
	}
	hc, err := helm.NewMemoryClient(platformNamespace)
	if err != nil {
		t.Fatalf("NewMemoryClient: %v", err)
	}
	values := renderSessionManagerValues(obj, valuesTestClusterConfig(), testOperatorNamespace)
	_, err = hc.Install(context.Background(), chrt.Metadata.Name, chrt, values)
	return err
}

func TestSessionManagerChart_ImagePullerFailsOnANameTheInventoryLacks(t *testing.T) {
	obj := prePuller(nil,
		platformv1alpha1.PrePulledImage{Name: "jdk21-environment"},
		platformv1alpha1.PrePulledImage{Name: "jdk22-environment"},
	)

	err := installSessionManagerChart(t, obj)

	if err == nil {
		t.Fatal("install succeeded, want it to fail on the unknown name")
	}
	for _, want := range []string{"imagePrePuller.images[1]", `"jdk22-environment"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("install error %q does not contain %q", err, want)
		}
	}
}

// The chart's values schema rejects what the SessionManager CRD rejects,
// for installs of the chart without the operator.
func TestSessionManagerChart_ImagePullerRejectsWhatTheCRDRejects(t *testing.T) {
	for name, obj := range map[string]*platformv1alpha1.SessionManager{
		"an entry with both name and image": prePuller(nil,
			platformv1alpha1.PrePulledImage{Name: "jdk21-environment", Image: "registry.example.com/jdk21:1.0"}),
		"an entry with neither name nor image": prePuller(nil, platformv1alpha1.PrePulledImage{}),
		"nothing to pull":                      prePuller(new(false)),
	} {
		t.Run(name, func(t *testing.T) {
			err := installSessionManagerChart(t, obj)

			if err == nil {
				t.Fatal("install succeeded, want the values schema to reject it")
			}
			if !strings.Contains(err.Error(), "imagePrePuller") {
				t.Errorf("install error %q does not name imagePrePuller", err)
			}
		})
	}
}

func TestSessionManagerChart_ImagePullerSwitchedOffKeepsItsList(t *testing.T) {
	obj := prePuller(new(false),
		platformv1alpha1.PrePulledImage{Name: "jdk21-environment"},
		// Not checked while the pre-puller is off.
		platformv1alpha1.PrePulledImage{Name: "jdk22-environment"},
		platformv1alpha1.PrePulledImage{Image: "registry.example.com/workshops/custom-base:1.0"},
	)
	obj.Spec.ImagePrePuller.Enabled = false

	// The render fails the test when the install fails.
	if w := renderImagePullerDaemonSet(t, obj, valuesTestClusterConfig()); w != nil {
		t.Fatal("the image-puller DaemonSet is rendered while the pre-puller is off")
	}
}
