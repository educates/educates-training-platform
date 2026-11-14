package registry

import (
	"cmp"
	"context"
	"fmt"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/pkg/errors"
)

const (
	// defaultSetUpPollInterval is how often a command looks again at a
	// shared container another command is creating or setting up.
	defaultSetUpPollInterval = 250 * time.Millisecond

	// defaultSetUpTimeout is how long a command waits for another command
	// to create and set up a shared container, which takes it seconds.
	defaultSetUpTimeout = 60 * time.Second
)

// sharedContainer is a container several commands may set up at the same
// time, such as the local registry, or a registry mirror, while two local
// clusters are created at once.
type sharedContainer struct {
	// name is the container's name, and what names it in messages.
	name, what string

	// state returns the container's state, and false when it does not
	// exist.
	state func(context.Context) (container.ContainerState, bool, error)

	// prepare readies what create needs, such as the container's image
	// and the educates network. It runs once, before the first create.
	prepare func(context.Context) error

	// create creates the container, with an error Docker's conflict
	// classifies when another container holds its name.
	create func(context.Context) error

	// setUp joins the container create created to its networks and
	// starts it.
	setUp func(context.Context) error

	// adopt takes a container that exists as it is, bringing in line what
	// the command sets up again each time, such as its link to the
	// cluster network.
	adopt func(context.Context) error

	// pollInterval and setUpTimeout pace the wait for another command.
	// Zero means defaultSetUpPollInterval and defaultSetUpTimeout.
	pollInterval, setUpTimeout time.Duration
}

// ensure adopts the shared container when it exists, and otherwise
// creates it and sets it up.
//
// Docker creates a container of a given name once, refusing every other
// create with a conflict, so of several commands that find the container
// missing at once, one creates it. Docker refuses the name as soon as that
// command's create starts, but shows the container only once the create
// completes, so a command refused the name looks again until the
// container appears. It, and a command that finds the container created
// but not yet started, wait until the command that created it has set it
// up, and adopt it then: linking it to the cluster network while that
// command joins it to its networks makes one of the two connects fail,
// and the command whose connect fails removes the container. When the
// command that created the container fails and removes it, a waiting
// command creates it itself.
func (c sharedContainer) ensure(ctx context.Context, p Progress) error {
	deadline := time.Now().Add(cmp.Or(c.setUpTimeout, defaultSetUpTimeout))
	prepared, waiting := false, false

	wait := func() {
		if !waiting {
			report(p, fmt.Sprintf("waiting for another command to set up the %s container", c.what))
			waiting = true
		}
	}

	for {
		state, found, err := c.state(ctx)
		if err != nil {
			return err
		}

		if found {
			if state == container.StateCreated {
				wait()

				state, found, err = c.waitForSetUp(ctx, deadline)
				if err != nil {
					return err
				}

				if !found {
					continue
				}

				if state == container.StateCreated {
					warn(p, fmt.Sprintf("%s container %s has not started: another command may still be setting it up, "+
						"or one was interrupted; if it does not start, delete it and deploy again", c.what, c.name))
				}
			}

			return c.adopt(ctx)
		}

		if !prepared {
			if err := c.prepare(ctx); err != nil {
				return err
			}

			prepared = true
		}

		err = c.create(ctx)

		if err == nil {
			return c.setUp(ctx)
		}

		if !cerrdefs.IsConflict(err) {
			return err
		}

		if !time.Now().Before(deadline) {
			return errors.Errorf("unable to create %s container %s: Docker reports its name in use, but no such container appeared",
				c.what, c.name)
		}

		wait()

		if err := c.pause(ctx); err != nil {
			return err
		}
	}
}

// waitForSetUp waits while the container is created but not started,
// returning its state once it has started, stopped or gone, or once
// deadline passes.
func (c sharedContainer) waitForSetUp(ctx context.Context, deadline time.Time) (container.ContainerState, bool, error) {
	for {
		state, found, err := c.state(ctx)

		if err != nil || !found || state != container.StateCreated || !time.Now().Before(deadline) {
			return state, found, err
		}

		if err := c.pause(ctx); err != nil {
			return state, found, err
		}
	}
}

// pause waits for the poll interval, or until ctx is done.
func (c sharedContainer) pause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(cmp.Or(c.pollInterval, defaultSetUpPollInterval)):
		return nil
	}
}

// containerState returns the state of the container name, and false when
// it does not exist.
func containerState(ctx context.Context, cli *client.Client, name string) (container.ContainerState, bool, error) {
	info, err := cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{})

	if cerrdefs.IsNotFound(err) {
		return "", false, nil
	}

	if err != nil {
		return "", false, errors.Wrapf(err, "unable to inspect container %s", name)
	}

	if info.Container.State == nil {
		return "", true, nil
	}

	return info.Container.State.Status, true, nil
}

// ensureEducatesNetwork creates the educates network unless it exists.
// Docker creates a network of a given name once, refusing every other
// create with a conflict, so a network another command creates at the
// same time counts as created.
func ensureEducatesNetwork(ctx context.Context, cli *client.Client) error {
	if _, err := cli.NetworkInspect(ctx, EducatesNetworkName, client.NetworkInspectOptions{}); err == nil {
		return nil
	}

	if _, err := cli.NetworkCreate(ctx, EducatesNetworkName, client.NetworkCreateOptions{}); err != nil && !cerrdefs.IsConflict(err) {
		return errors.Wrap(err, "cannot create educates network")
	}

	return nil
}
