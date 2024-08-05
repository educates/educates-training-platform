"""REST API handlers for workshop requests."""

import logging

from aiohttp import web

from .authnz import login_required, roles_accepted

logger = logging.getLogger("educates")


@login_required
@roles_accepted("admin", "workshop-reader")
async def api_get_v1_workshops(request: web.Request) -> web.Response:
    """Returns a list of workshops available to the user."""

    # Grab tenant name from query string parameters. We need to fail with an
    # error if none is provided.

    tenant_name = request.query.get("tenantName")

    if not tenant_name:
        # We don't require a tenant name for admin users.

        matched_roles = request["matched_roles"]

        if "admin" not in matched_roles:
            return web.Response(text="Missing tenantName query parameter", status=400)

    # Now check whether the client is allowed access to this tenant.

    service_state = request.app["service_state"]
    tenant_database = service_state.tenant_database

    client = request["remote_client"]

    if tenant_name:
        if tenant_name not in client.tenants:
            return web.Response(text="Client not allowed access to tenant", status=403)

    # Work out the set of portals accessible by the user for this tenant. The
    # tenant name may not be set if the user is an admin. An empty set for
    # accessible portals means that the user has access to all portals.

    if tenant_name:
        tenant = tenant_database.get_tenant_by_name(tenant_name)

        if not tenant:
            return web.Response(text="Tenant not available", status=403)

        accessible_portals = tenant.portals_which_are_accessible()

    else:
        accessible_portals = []

    # Generate the list of workshops available to the user for this tenant which
    # are in a running state. We need to eliminate any duplicates as a workshop
    # may be available through multiple training portals. We use the title and
    # description from the last found so we expect these to be consistent.

    workshops = {}

    for portal in accessible_portals:
        for environment in portal.get_running_environments():
            workshops[environment.workshop] = {
                "name": environment.workshop,
                "title": environment.title,
                "description": environment.description,
                "labels": environment.labels,
            }

    return web.json_response({"workshops": list(workshops.values())})


@login_required
@roles_accepted("admin", "workshop-requestor")
async def api_post_v1_workshops(request: web.Request) -> web.Response:
    """Returns a workshop session for the specified tenant and workshop."""

    data = await request.json()

    tenant_name = data.get("tenantName")

    user_id = data.get("clientUserId") or ""
    action_id = data.get("clientActionId") or ""  # pylint: disable=unused-variable
    index_url = data.get("clientIndexUrl") or ""

    workshop_name = data.get("workshopName")
    parameters = data.get("workshopParams", [])

    if not tenant_name:
        return web.Response(text="Missing tenantName", status=400)

    if not workshop_name:
        return web.Response(text="Missing workshopName", status=400)

    # Check that client is allowed access to this tenant.

    client = request["remote_client"]

    if tenant_name not in client.tenants:
        return web.Response(text="Client not allowed access to tenant", status=403)

    # Find the portals accessible to the tenant which hosts the workshop.

    service_state = request.app["service_state"]
    tenant_database = service_state.tenant_database

    tenant = tenant_database.get_tenant_by_name(tenant_name)

    if not tenant:
        return web.Response(text="Tenant not available", status=403)

    # Get the list of portals hosting the workshop and calculate the subset
    # that are accessible to the tenant.

    accessible_portals = tenant.portals_which_are_accessible()

    selected_portals = []

    for portal in accessible_portals:
        if portal.hosts_workshop(workshop_name):
            selected_portals.append(portal)

    # If there are no resulting portals, then the workshop is not available to
    # the tenant.

    if not selected_portals:
        return web.Response(text="Workshop not available", status=503)

    # If a user ID is supplied, check each of the portals to see if this user
    # already has a workshop session for this workshop.

    if user_id:
        for portal in selected_portals:
            session = portal.find_existing_workshop_session_for_user(
                user_id, workshop_name
            )

            if session:
                data = await session.reacquire_workshop_session()

                if data:
                    data["tenantName"] = tenant_name
                    return web.json_response(data)

    # Find the set of workshop environments for the specified workshop that are
    # in a running state. If there are no such environments, then the workshop
    # is not available.

    environments = []

    for portal in selected_portals:
        for environment in portal.get_running_environments():
            if environment.workshop == workshop_name:
                environments.append(environment)

    logger.info("ENV#0 %s", [environment.portal.cluster.name for environment in environments])

    if not environments:
        return web.Response(text="Workshop not available", status=503)
    
    # Choose the best workshop environment to allocate a session from based on
    # available capacity of the workshop environment and the portal hosting it.

    environment = choose_best_workshop_environment(environments)

    if environment:
        data = await environment.request_workshop_session(
            user_id, parameters, index_url
        )

        if data:
            data["tenantName"] = tenant_name
            return web.json_response(data)

    # If we get here, then we don't believe there is any available capacity for
    # creating a workshop session. Even so, attempt to create a session against
    # any workshop environment, just make sure that we don't try and use the
    # same workshop environment we just tried to get a session from.

    if environment:
        environments = environments.remove(environment)

    if not environments:
        return web.Response(text="Workshop not available", status=503)

    environment = environments[0]

    data = await environment.request_workshop_session(
        user_id, parameters, index_url
    )

    if data:
        data["tenantName"] = tenant_name
        return web.json_response(data)
    
    # If we get here, then we don't believe there is any available capacity for
    # creating a workshop session.

    return web.Response(text="Workshop not available", status=503)


def choose_best_workshop_environment(environments):
    if len(environments) == 1:
        return environments[0]

    # First discard any workshop environment which have no more space available.

    logger.info("PTL-CAPACITY#1 %s", [(environment.portal.cluster.name, environment.portal.capacity, environment.portal.allocated) for environment in environments])
    logger.info("ENV-CAPACITY#1 %s", [(environment.portal.cluster.name, environment.capacity, environment.allocated) for environment in environments])

    environments = [
        environment
        for environment in environments
        if environment.capacity
        and (environment.capacity - environment.allocated > 0)
    ]

    logger.info("ENV#1 %s", [environment.portal.cluster.name for environment in environments])

    # Also discard any workshop environments where the portal as a whole has
    # no more capacity.

    environments = [
        environment
        for environment in environments
        if environment.portal.capacity
        and (environment.portal.capacity - environment.portal.allocated > 0)
    ]

    logger.info("ENV#2, %s", [environment.portal.cluster.name for environment in environments])

    # If there is only one workshop environment left, return it.

    if len(environments) == 1:
        return environments[0]

    # If there are no workshop environments left, return None.

    if len(environments) == 0:
        return None

    # If there are multiple workshop environments left, starting with the portal
    # with the most capacity remaining, look at number of reserved sessions
    # available for a workshop environment and if any, allocate it from the
    # workshop environment with the most. In other words, sort based on the
    # number of reserved sessions and if the first in the resulting list has
    # reserved sessions, use that.

    def score_based_on_reserved_sessions(environment):
        return (
            environment.portal.capacity
            and (environment.portal.capacity - environment.portal.allocated)
            or 1,
            environment.available,
        )

    environments.sort(key=score_based_on_reserved_sessions, reverse=True)

    logger.info("ENV#3, %s", [environment.portal.cluster.name for environment in environments])

    if environments[0].available > 0:
        return environments[0]

    # If there are no reserved sessions available, starting with the portal
    # with the most capacity remaining, look at the available capacity within
    # the workshop environment. In other words, sort based on the number of free
    # spots in the workshop environment and if the first in the resulting list
    # has free spots, use that.

    def score_based_on_available_capacity(environment):
        return (
            environment.portal.capacity
            and (environment.portal.capacity - environment.portal.allocated)
            or 1,
            environment.capacity
            and (environment.capacity - environment.allocated)
            or 1,
        )

    environments.sort(key=score_based_on_available_capacity, reverse=True)

    logger.info("ENV#4 %s", [environment.portal.cluster.name for environment in environments])

    return environments[0]