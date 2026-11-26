package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/localcerts"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/router"
)

// routerContainer is how the local cluster commands reach the router
// container.
type routerContainer struct {
	// image is the router image the CLI pins.
	image string

	// create creates the router container publishing ports 80 and 443 on
	// an address and serving the router folder dir, and starts it, as
	// router.Create does.
	create func(ctx context.Context, address, dir string) (adopted bool, err error)

	// remove removes the router container, when there is one.
	remove func(ctx context.Context) error

	// reload makes the running router serve its folder's configuration.
	reload func(ctx context.Context) error

	// join connects the router to the kind network, once it exists.
	join func(ctx context.Context) error
}

// dockerRouterContainer is the router container on the Docker host.
var dockerRouterContainer = routerContainer{
	image:  router.Image,
	create: router.Create,
	remove: router.Remove,
	reload: router.Reload,
	join:   router.JoinClusterNetwork,
}

// routerOperations are the operations a local cluster command creates,
// re-creates or refreshes the router with: those on the router container,
// the data home whose router folder it serves, the check of Docker
// Desktop's privileged port setting, which applies wherever the router is
// created or re-created, and where the certificates come from that the
// router ends TLS with.
type routerOperations struct {
	container routerContainer
	dataHome  string

	// checkPrivilegedPorts refuses a Docker engine that can't serve the
	// router's ports 80 and 443 on a non-loopback address, as
	// checkDockerDesktopPrivilegedPorts does.
	checkPrivilegedPorts func(ctx context.Context) error

	// certificate returns the certificate the router ends TLS with for an
	// ingress domain, given the one it serves now, nil for none, with a
	// line for each change it made, as localcerts.RouterCertificate does.
	certificate func(domain string, current *localclusters.RouterCertificate) (localclusters.RouterCertificate, []string, error)
}

// writeRouterFolder writes the router folder serving routes, as
// localclusters.RenderRouter renders them, with the certificate the router
// ends TLS with for each route whose TLS ends at the router, which
// ops.certificate gives from the one the folder holds, issuing it again
// near its expiry. It returns a line for each change to a certificate, for
// the caller to note once step is done. A certificate it can't get is a
// warning in step, and the router closes the TLS connections of that
// cluster's domain until a later write gets one.
func (ops routerOperations) writeRouterFolder(step progress.Step, routes []localclusters.RouterRoute) ([]string, error) {
	var certificates []localclusters.RouterCertificate
	var notes []string
	for _, domain := range localclusters.RouterCertificateDomains(routes) {
		current, err := localclusters.ReadRouterCertificate(ops.dataHome, domain)
		if err != nil {
			step.Warn(fmt.Sprintf("%v: the router gets another certificate for ingress domain %q", err, domain))
			current = nil
		}
		certificate, changes, err := ops.certificate(domain, current)
		notes = append(notes, changes...)
		if err != nil {
			step.Warn(fmt.Sprintf("the router has no certificate for ingress domain %q, whose TLS ends at the router, "+
				"so it closes the domain's TLS connections: %v", domain, err))
			continue
		}
		certificates = append(certificates, certificate)
	}
	return notes, localclusters.WriteRouterFolder(ops.dataHome, localclusters.RenderRouter(routes, certificates...))
}

// noteAll notes each of notes on rep.
func noteAll(rep progress.Reporter, notes []string) {
	for _, n := range notes {
		rep.Note(n)
	}
}

// ensureRouter creates the router container when it is missing, and
// re-creates it when it runs another image than the pinned one, publishes
// on another address than the router address, or has stopped, as
// localclusters.Inventory.PlanRouter decides from inventory, as a step of
// rep. It reports whether it did either: a router it leaves as it is
// still has to be refreshed, with refreshRouter.
//
// Creating or re-creating the router checks the router address rules and
// Docker Desktop's privileged port setting first, and writes the router
// folder from the local clusters inventory holds. The router container's
// start is the check that ports 80 and 443 are free on the router address:
// when it fails, the error names any local cluster made by an earlier
// release of the CLI that still publishes them from its node.
func (ops routerOperations) ensureRouter(ctx context.Context, rep progress.Reporter, inventory localclusters.Inventory) (bool, error) {
	plan, err := inventory.PlanRouter(ops.container.image)
	if err != nil {
		return false, err
	}
	if plan.Action == localclusters.RouterReload {
		return false, nil
	}
	if err := ops.checkPrivilegedPorts(ctx); err != nil {
		return false, err
	}

	label := fmt.Sprintf("creating router %s on %s", router.ContainerName, plan.Address)
	if plan.Action == localclusters.RouterRecreate {
		label = fmt.Sprintf("re-creating router %s on %s", router.ContainerName, plan.Address)
	}
	var notes []string
	defer func() { noteAll(rep, notes) }()
	err = runStep(rep, label, "running", func(s progress.Step) error {
		if plan.RecreateReason != "" {
			s.Update(plan.RecreateReason + ", which drops every connection through it")
		}
		routes, leftOut, err := inventory.RouterRoutes()
		if err != nil {
			return err
		}
		for _, e := range leftOut {
			s.Warn(e.Error())
		}
		if notes, err = ops.writeRouterFolder(s, routes); err != nil {
			return err
		}
		if plan.Action == localclusters.RouterRecreate {
			if err := ops.container.remove(ctx); err != nil {
				return err
			}
		}
		adopted, err := ops.container.create(ctx, plan.Address, config.LocalRouterDir(ops.dataHome))
		var startErr *router.StartError
		if errors.As(err, &startErr) {
			return inventory.RouterStartError(plan.Address, startErr.Err)
		}
		if err != nil || !adopted {
			return err
		}
		// Another command created the router at the same time: serve the
		// folder just written from it.
		return ops.container.reload(ctx)
	})
	return err == nil, err
}

// refreshRouter renders the router's configuration again, from the local
// clusters observe returns, with the local clusters of given served from
// the configs given for them, writes it to the router folder, joins the
// router to the kind network, and reloads the router, as a step of rep,
// printing nothing else. The reload keeps open connections, such as
// terminals. When the router container does not exist it does nothing.
//
// It never fails. Local cluster create, start and delete refresh the
// router once the cluster exists, or is gone, so the step warns about each
// config the router leaves out, and a router it can't render, write or
// reload is a warning that says to run 'educates local router update'. A
// router that is not running only has its folder written, which it serves
// once it starts.
func (ops routerOperations) refreshRouter(ctx context.Context, rep progress.Reporter,
	observe func(context.Context) (localclusters.Inventory, error), given ...localclusters.LoadedConfig) {
	inventory, err := observe(ctx)
	running := false
	if err == nil {
		var exists bool
		var observed localclusters.RouterContainer
		if observed, exists = inventory.Router(); !exists {
			return
		}
		running = observed.Running
	}
	step := rep.Start("refreshing router")
	var notes []string
	defer func() { noteAll(rep, notes) }()
	if err == nil {
		notes, err = ops.serveRoutes(ctx, step, inventory, running, given...)
	}
	if err != nil {
		step.Warn(fmt.Sprintf("not refreshed: %v. Run 'educates local router update' once that is fixed", err))
		step.Done("skipped")
		return
	}
	if !running {
		step.Warn(fmt.Sprintf("the router container %s is not running: it serves the configuration written to %s once it starts",
			router.ContainerName, config.LocalRouterDir(ops.dataHome)))
	}
	step.Done("updated")
}

// refreshAfterDelete refreshes the router once a local cluster is deleted,
// from the local clusters observe returns, as refreshRouter does, and
// removes from the router folder the certificate and key of every local
// cluster the router no longer serves, as
// localclusters.Inventory.PruneRouterCertificates does, whatever the
// router's state: refreshRouter leaves the folder as it is when there is
// no router container, or one it can't refresh, and no key outlives its
// cluster. A certificate it can't remove is a warning, since the cluster
// is gone by then.
func (ops routerOperations) refreshAfterDelete(ctx context.Context, rep progress.Reporter,
	observe func(context.Context) (localclusters.Inventory, error)) {
	inventory, err := observe(ctx)
	ops.refreshRouter(ctx, rep, func(context.Context) (localclusters.Inventory, error) { return inventory, err })
	if err != nil {
		return
	}
	if err := inventory.PruneRouterCertificates(); err != nil {
		rep.Note(fmt.Sprintf("Warning: the router folder %s may still hold the certificate and key of the deleted local cluster: %v",
			config.LocalRouterDir(ops.dataHome), err))
	}
}

// serveRoutes renders the router's configuration again, from the local
// clusters inventory holds, with the local clusters of given served from
// the configs given for them, writes it to the router folder with the
// certificates the router ends TLS with, as writeRouterFolder does, joins
// the router to the kind network, and reloads the router when running is
// set. It warns in step about each config the router leaves out, and
// returns a line for each change to a certificate, for the caller to note
// once step is done.
func (ops routerOperations) serveRoutes(ctx context.Context, step progress.Step, inventory localclusters.Inventory,
	running bool, given ...localclusters.LoadedConfig) ([]string, error) {
	routes, leftOut, err := inventory.RouterRoutes(given...)
	for _, e := range leftOut {
		step.Warn(e.Error())
	}
	if err != nil {
		return nil, err
	}
	notes, err := ops.writeRouterFolder(step, routes)
	if err != nil {
		return notes, err
	}
	if err := ops.container.join(ctx); err != nil {
		return notes, err
	}
	if !running {
		return notes, nil
	}
	return notes, ops.container.reload(ctx)
}

// removeRouter removes the router container and the router folder of the
// data home, keeping the router file, router.yaml, which holds the router
// address.
func (ops routerOperations) removeRouter(ctx context.Context) error {
	if err := ops.container.remove(ctx); err != nil {
		return err
	}
	return localclusters.RemoveRouterFolder(ops.dataHome)
}

// dockerRouterOperations are the router operations on the router container of the
// Docker host, serving the router folder of the data home dataHome. The
// check of Docker Desktop's privileged port setting writes to w the
// warning for a setting it can't read.
func dockerRouterOperations(dataHome string, w io.Writer) routerOperations {
	return routerOperations{
		container: dockerRouterContainer,
		dataHome:  dataHome,
		checkPrivilegedPorts: func(ctx context.Context) error {
			return checkDockerDesktopPrivilegedPorts(ctx, w)
		},
		certificate: localcerts.RouterCertificate,
	}
}
