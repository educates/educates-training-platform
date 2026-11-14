package registry

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/pkg/errors"
)

// missing is the state the fake reports for a container that does not
// exist.
const missing container.ContainerState = "missing"

// fakeShared plays a shared container whose states and create errors are
// scripted: each look at it takes the next state, and each create the next
// error, the last of each holding. It records the calls ensure makes.
type fakeShared struct {
	states    []container.ContainerState
	stateErr  error
	createErr []error
	calls     []string
	updates   []string
	warnings  []string
}

func (f *fakeShared) container() sharedContainer {
	return sharedContainer{
		name: "educates-registry",
		what: "registry",
		state: func(context.Context) (container.ContainerState, bool, error) {
			f.calls = append(f.calls, "state")
			if f.stateErr != nil {
				return "", false, f.stateErr
			}
			state := f.states[0]
			if len(f.states) > 1 {
				f.states = f.states[1:]
			}
			if state == missing {
				return "", false, nil
			}
			return state, true, nil
		},
		prepare: func(context.Context) error {
			f.calls = append(f.calls, "prepare")
			return nil
		},
		create: func(context.Context) error {
			f.calls = append(f.calls, "create")
			if len(f.createErr) == 0 {
				return nil
			}
			err := f.createErr[0]
			if len(f.createErr) > 1 {
				f.createErr = f.createErr[1:]
			}
			return err
		},
		setUp: func(context.Context) error {
			f.calls = append(f.calls, "setUp")
			return nil
		},
		adopt: func(context.Context) error {
			f.calls = append(f.calls, "adopt")
			return nil
		},
		pollInterval: time.Millisecond,
		setUpTimeout: 20 * time.Millisecond,
	}
}

func (f *fakeShared) Update(phase string) { f.updates = append(f.updates, phase) }
func (f *fakeShared) Warn(msg string)     { f.warnings = append(f.warnings, msg) }

// conflict is the error a create returns when another command created the
// container first, wrapped as the create functions wrap it.
var conflict = errors.Wrap(cerrdefs.ErrConflict, "cannot create registry container")

// steps returns the calls f recorded other than looks at the container's
// state, which waiting repeats a varying number of times.
func (f *fakeShared) steps() []string {
	return slices.DeleteFunc(slices.Clone(f.calls), func(c string) bool { return c == "state" })
}

func (f *fakeShared) waited() bool {
	return slices.ContainsFunc(f.updates, func(u string) bool { return strings.HasPrefix(u, "waiting for another command") })
}

func TestEnsureCreatesAndSetsUpAMissingContainer(t *testing.T) {
	f := &fakeShared{states: []container.ContainerState{missing}}

	if err := f.container().ensure(context.Background(), f); err != nil {
		t.Fatalf("ensure() = %v, want nil", err)
	}
	if got, want := f.steps(), []string{"prepare", "create", "setUp"}; !slices.Equal(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
}

func TestEnsureAdoptsARunningContainerWithoutWaiting(t *testing.T) {
	f := &fakeShared{states: []container.ContainerState{container.StateRunning}}

	if err := f.container().ensure(context.Background(), f); err != nil {
		t.Fatalf("ensure() = %v, want nil", err)
	}
	if got, want := f.steps(), []string{"adopt"}; !slices.Equal(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
	if f.waited() {
		t.Errorf("waited for a running container: %v", f.updates)
	}
}

func TestEnsureAdoptsAStoppedContainerAsItIs(t *testing.T) {
	f := &fakeShared{states: []container.ContainerState{container.StateExited}}

	if err := f.container().ensure(context.Background(), f); err != nil {
		t.Fatalf("ensure() = %v, want nil", err)
	}
	if got, want := f.steps(), []string{"adopt"}; !slices.Equal(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
}

func TestEnsureWaitsForAContainerAnotherCommandIsSettingUp(t *testing.T) {
	f := &fakeShared{states: []container.ContainerState{
		container.StateCreated, container.StateCreated, container.StateCreated, container.StateRunning,
	}}

	if err := f.container().ensure(context.Background(), f); err != nil {
		t.Fatalf("ensure() = %v, want nil", err)
	}
	if got, want := f.steps(), []string{"adopt"}; !slices.Equal(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
	if looks := len(f.calls) - len(f.steps()); looks != 4 {
		t.Errorf("looked at the container %d times, want 4, until it started: calls %v", looks, f.calls)
	}
	if !f.waited() {
		t.Errorf("did not report the wait: %v", f.updates)
	}
	if len(f.warnings) > 0 {
		t.Errorf("warnings = %v, want none", f.warnings)
	}
}

func TestEnsureWaitsForTheCommandThatWonTheRaceToCreate(t *testing.T) {
	f := &fakeShared{
		states:    []container.ContainerState{missing, container.StateCreated, container.StateRunning},
		createErr: []error{conflict},
	}

	if err := f.container().ensure(context.Background(), f); err != nil {
		t.Fatalf("ensure() = %v, want nil", err)
	}
	if got, want := f.steps(), []string{"prepare", "create", "adopt"}; !slices.Equal(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
}

func TestEnsureWaitsForAContainerDockerHasNotFinishedCreating(t *testing.T) {
	f := &fakeShared{
		states:    []container.ContainerState{missing, missing, missing, container.StateCreated, container.StateRunning},
		createErr: []error{conflict},
	}

	if err := f.container().ensure(context.Background(), f); err != nil {
		t.Fatalf("ensure() = %v, want nil", err)
	}
	if got, want := f.steps(), []string{"prepare", "create", "create", "create", "adopt"}; !slices.Equal(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
	if !f.waited() {
		t.Errorf("did not report the wait: %v", f.updates)
	}
}

func TestEnsureCreatesAContainerTheOtherCommandRemoved(t *testing.T) {
	f := &fakeShared{
		states:    []container.ContainerState{missing, container.StateCreated, missing},
		createErr: []error{conflict, nil},
	}

	if err := f.container().ensure(context.Background(), f); err != nil {
		t.Fatalf("ensure() = %v, want nil", err)
	}
	if got, want := f.steps(), []string{"prepare", "create", "create", "setUp"}; !slices.Equal(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
}

func TestEnsureAdoptsAContainerThatNeverStartsWithAWarning(t *testing.T) {
	f := &fakeShared{states: []container.ContainerState{container.StateCreated}}

	if err := f.container().ensure(context.Background(), f); err != nil {
		t.Fatalf("ensure() = %v, want nil", err)
	}
	if got, want := f.steps(), []string{"adopt"}; !slices.Equal(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
	if len(f.warnings) != 1 || !strings.Contains(f.warnings[0], "educates-registry has not started") {
		t.Errorf("warnings = %v, want one naming the container that has not started", f.warnings)
	}
}

func TestEnsureReturnsACreateErrorOtherThanAConflict(t *testing.T) {
	createErr := errors.New("cannot create registry container: invalid port binding")
	f := &fakeShared{states: []container.ContainerState{missing}, createErr: []error{createErr}}

	if err := f.container().ensure(context.Background(), f); err != createErr {
		t.Fatalf("ensure() = %v, want %v", err, createErr)
	}
	if got, want := f.steps(), []string{"prepare", "create"}; !slices.Equal(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
}

func TestEnsureReturnsAnErrorLookingAtTheContainer(t *testing.T) {
	inspectErr := errors.New("unable to inspect container educates-registry")
	f := &fakeShared{stateErr: inspectErr}

	if err := f.container().ensure(context.Background(), f); err != inspectErr {
		t.Fatalf("ensure() = %v, want %v", err, inspectErr)
	}
	if got := f.steps(); len(got) != 0 {
		t.Errorf("steps = %v, want none", got)
	}
}

func TestEnsureGivesUpWhenTheContainerNeverAppears(t *testing.T) {
	f := &fakeShared{
		states:    []container.ContainerState{missing},
		createErr: []error{conflict},
	}

	err := f.container().ensure(context.Background(), f)

	if err == nil || !strings.Contains(err.Error(), "no such container appeared") {
		t.Fatalf("ensure() = %v, want an error saying no such container appeared", err)
	}
	if got := f.steps(); slices.Index(got, "prepare") != 0 || slices.Index(got[1:], "prepare") != -1 {
		t.Errorf("steps = %v, want prepare once, first", got)
	}
}
