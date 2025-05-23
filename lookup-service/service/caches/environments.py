"""Configuration for workshop environments."""

import logging
from dataclasses import dataclass
from typing import TYPE_CHECKING, Dict, List

from aiohttp import ClientSession
from wrapt import synchronized

if TYPE_CHECKING:
    from .portals import TrainingPortal
    from .sessions import WorkshopSession

logger = logging.getLogger("educates")


@dataclass
class WorkshopEnvironment:
    """Snapshot of workshop environment state. This includes a database of
    the workshop sessions created from the workshop environment."""

    portal: "TrainingPortal"
    name: str
    uid: str
    generation: int
    workshop: str
    title: str
    description: str
    labels: List[Dict[str, str]]
    capacity: int
    reserved: int
    allocated: int
    available: int
    phase: str
    sessions: Dict[str, "WorkshopSession"]

    def __init__(
        self,
        portal: "TrainingPortal",
        name: str,
        uid: str,
        generation: int,
        workshop: str,
        title: str,
        description: str,
        labels: List[Dict[str, str]],
        capacity: int,
        reserved: int,
        allocated: int,
        available: int,
        phase: str,
    ) -> None:
        self.portal = portal
        self.name = name
        self.uid = uid
        self.generation = generation
        self.workshop = workshop
        self.title = title
        self.description = description
        self.labels = labels
        self.capacity = capacity
        self.reserved = reserved
        self.allocated = allocated
        self.available = available
        self.phase = phase
        self.sessions = {}

    def get_sessions(self) -> Dict[str, "WorkshopSession"]:
        """Returns all workshop sessions."""

        return list(self.sessions.values())

    def get_session(self, session_name: str) -> "WorkshopSession":
        """Returns a workshop session by name."""

        return self.sessions.get(session_name)

    def add_session(self, session: "WorkshopSession") -> None:
        """Add a session to the environment."""

        self.sessions[session.name] = session

    def remove_session(self, session_name: str) -> None:
        """Remove a session from the environment."""

        self.sessions.pop(session_name, None)

    @synchronized
    def recalculate_capacity(self) -> None:
        """Recalculate the available capacity of the environment."""

        allocated = 0
        available = 0

        for session in list(self.sessions.values()):
            if session.phase == "Allocated":
                allocated += 1
            elif session.phase == "Available":
                available += 1

        self.allocated = allocated
        self.available = available

        logger.info(
            "Recalculated capacity for environment %s of portal %s in cluster %s: %s",
            self.name,
            self.portal.name,
            self.portal.cluster.name,
            {"allocated": allocated, "available": available},
        )

    async def request_workshop_session(
        self,
        user_id: str,
        user_email: str,
        user_first_name: str,
        user_last_name: str,
        parameters: List[Dict[str, str]],
        index_url: str,
        analytics_url: str,
    ) -> Dict[str, str] | None:
        """Request a workshop session for a user."""

        portal = self.portal

        async with ClientSession() as http_client:
            async with portal.client_session(http_client) as portal_client:
                if not portal_client.connected:
                    return

                return await portal_client.request_workshop_session(
                    environment_name=self.name,
                    user_id=user_id,
                    user_email=user_email,
                    user_first_name=user_first_name,
                    user_last_name=user_last_name,
                    parameters=parameters,
                    index_url=index_url,
                    analytics_url=analytics_url,
                )
