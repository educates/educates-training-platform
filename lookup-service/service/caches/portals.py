"""Configuration database for training portals."""

import logging
from dataclasses import dataclass
from typing import TYPE_CHECKING, Dict, List, Tuple, Union

from aiohttp import BasicAuth, ClientSession, ClientConnectorError, ClientError

from .clusters import ClusterConfig

if TYPE_CHECKING:
    from .environments import WorkshopEnvironment
    from .sessions import WorkshopSession


logger = logging.getLogger("educates")


@dataclass
class PortalCredentials:
    """Configuration object for a portal's authentication."""

    client_id: str
    client_secret: str
    username: str
    password: str


@dataclass
class TrainingPortal:
    """Snapshot of training portal state. This includes a database of the
    workshop environments managed by the training portal."""

    cluster: ClusterConfig
    name: str
    uid: str
    generation: int
    labels: List[Dict[str, str]]
    url: str
    namespace: str
    credentials: PortalCredentials
    phase: str
    capacity: int
    allocated: int
    environments: Dict[str, "WorkshopEnvironment"]

    def __init__(
        self,
        cluster: ClusterConfig,
        name: str,
        uid: str,
        generation: int,
        labels: List[Dict[str, str]],
        url: str,
        namespace: str,
        credentials: PortalCredentials,
        phase: str,
        capacity: int,
        allocated: int,
    ) -> None:
        self.cluster = cluster
        self.name = name
        self.uid = uid
        self.generation = generation
        self.labels = labels
        self.url = url
        self.namespace = namespace
        self.credentials = credentials
        self.phase = phase
        self.capacity = capacity
        self.allocated = allocated
        self.environments = {}

    @property
    def api_url(self) -> str:
        """Return the URL to use for REST API calls to the training portal. For
        local clusters, use the internal Kubernetes service URL. For remote
        clusters, use the public URL."""

        if self.cluster.local:
            return f"http://training-portal.{self.namespace}"

        return self.url

    def get_environments(self) -> List["WorkshopEnvironment"]:
        """Returns all workshop environments."""

        return list(self.environments.values())

    def get_running_environments(self) -> List["WorkshopEnvironment"]:
        """Returns all running workshop environments."""

        return [
            environment
            for environment in self.environments.values()
            if environment.phase == "Running"
        ]

    def get_environment(self, environment_name: str) -> "WorkshopEnvironment":
        """Returns a workshop environment by name."""

        return self.environments.get(environment_name)

    def add_environment(self, environment: "WorkshopEnvironment") -> None:
        """Add a workshop environment to the portal."""

        self.environments[environment.name] = environment

    def remove_environment(self, environment_name: str) -> None:
        """Remove a workshop environment from the portal."""

        self.environments.pop(environment_name, None)

    def hosts_workshop(self, workshop_name: str) -> bool:
        """Check if the portal hosts a workshop."""

        for environment in self.environments.values():
            if environment.workshop == workshop_name:
                return True

        return False

    def recalculate_capacity(self) -> None:
        """Recalculate the capacity of the portal."""

        for environment in self.environments.values():
            environment.recalculate_capacity()

        self.allocated = sum(
            environment.allocated for environment in self.environments.values()
        )

        logger.info(
            "Recalculated capacity for portal %s in cluster %s: %s",
            self.name,
            self.cluster.name,
            {"allocated": self.allocated, "capacity": self.capacity},
        )

    def find_existing_workshop_session_for_user(
        self, user_id: str, workshop_name: str
    ) -> Union["WorkshopSession", None]:
        """Find an existing workshop session for a user."""

        for environment in self.environments.values():
            for session in environment.get_sessions():
                if (
                    session.user == user_id
                    and session.environment.workshop == workshop_name
                ):
                    return session

        return None

    def client_session(self, session: ClientSession) -> "TrainingPortalClientSession":
        """Create a HTTP client session for accessing the remote training
        portal."""

        return TrainingPortalClientSession(self, session)


@dataclass
class TrainingPortalClientSession:
    """HTTP client session for accessing the remote training portal."""

    portal: TrainingPortal
    session: ClientSession
    access_token: str | None

    def __init__(self, portal: TrainingPortal, session: ClientSession) -> None:
        self.portal = portal
        self.session = session
        self.access_token = None

    async def __aenter__(self) -> "TrainingPortalClientSession":
        """Login to the portal service."""

        await self.login()

        return self

    async def __aexit__(self, exc_type, exc_value, traceback) -> None:
        """Logout from the portal service."""

        await self.logout()

    @property
    def connected(self):
        """Check if the client session is connected."""

        return bool(self.access_token)

    async def login(self) -> bool:
        """Login to the portal service ."""

        try:
            async with self.session.post(
                f"{self.portal.api_url}/oauth2/token/",
                data={
                    "grant_type": "password",
                    "username": self.portal.credentials.username,
                    "password": self.portal.credentials.password,
                },
                auth=BasicAuth(
                    self.portal.credentials.client_id,
                    self.portal.credentials.client_secret,
                ),
            ) as response:
                if response.status != 200:
                    logger.error(
                        "Failed to login to portal %s of cluster %s.",
                        self.portal.name,
                        self.portal.cluster.name,
                    )

                    return False

                data = await response.json()

                self.access_token = data.get("access_token")

                return True

        except ClientConnectorError as exc:
            logger.error(
                "Failed to connect to portal %s of cluster %s when attempting to login: %s",
                self.portal.name,
                self.portal.cluster.name,
                exc,
            )

            return False

    async def logout(self) -> None:
        """Logout from the portal service."""

        if not self.connected:
            return

        try:
            async with self.session.post(
                f"{self.portal.api_url}/oauth2/revoke-token/",
                data={
                    "client_id": self.portal.credentials.client_id,
                    "client_secret": self.portal.credentials.client_secret,
                    "token": self.access_token,
                },
            ) as response:
                if response.status != 200:
                    logger.error(
                        "Failed to logout from portal %s of cluster %s.",
                        self.portal.name,
                        self.portal.cluster.name,
                    )

        except ClientConnectorError as exc:
            logger.error(
                "Failed to connect to portal %s of cluster %s when attempting to logout: %s",
                self.portal.name,
                self.portal.cluster.name,
                exc,
            )

    async def reacquire_workshop_session(
        self, user_id: str, environment_name: str, session_name: str, index_url: str
    ) -> Dict[str, str] | None:
        """Reacquire a workshop session for a user."""

        if not self.connected:
            return

        if not session_name:
            return

        headers = {"Authorization": f"Bearer {self.access_token}"}

        try:
            async with self.session.get(
                f"{self.portal.api_url}/workshops/environment/{environment_name}/request/",
                headers=headers,
                params={
                    "index_url": index_url,
                    "user": user_id,
                    "session": session_name,
                },
            ) as response:
                if response.status != 200:
                    logger.error(
                        "Failed to reacquire session %s from portal %s of cluster %s for user %s.",
                        session_name,
                        self.portal.name,
                        self.portal.cluster.name,
                        user_id,
                    )
                    logger.error("Failed response status: %s", response.status)
                    logger.error("Failed response text: %s", await response.text())

                    return

                data = await response.json()

                url = data.get("url")

                if url:
                    return {
                        "clusterName": self.portal.cluster.name,
                        "portalName": self.portal.name,
                        "environmentName": environment_name,
                        "sessionName": session_name,
                        "clientUserId": user_id,
                        "sessionActivationUrl": f"{self.portal.url}{url}",
                    }

        except ClientConnectorError as exc:
            logger.error(
                "Failed to connect to portal %s of cluster %s when attempting to reacquire session %s for user %s: %s",  # pylint: disable=line-too-long
                self.portal.name,
                self.portal.cluster.name,
                session_name,
                user_id,
                exc,
            )

    async def request_workshop_session(
        self,
        environment_name: str,
        user_id: str,
        user_email: str,
        user_first_name: str,
        user_last_name: str,
        parameters: List[Dict[str, str]],
        index_url: str,
        analytics_url: str,
    ) -> Dict[str, str] | None:
        """Request a workshop session for a user."""

        if not self.connected:
            return

        headers = {"Authorization": f"Bearer {self.access_token}"}

        try:
            async with self.session.get(
                f"{self.portal.api_url}/workshops/environment/{environment_name}/request/",
                headers=headers,
                params={
                    "user": user_id,
                    "email": user_email,
                    "first_name": user_first_name,
                    "last_name": user_last_name,
                    "index_url": index_url,
                    "analytics_url": analytics_url,
                },
                json={"parameters": parameters},
            ) as response:
                if response.status != 200:
                    logger.error(
                        "Failed to request session from portal %s of cluster %s for user %s.",
                        self.portal.name,
                        self.portal.cluster.name,
                        user_id,
                    )
                    logger.error("Failed response status: %s", response.status)
                    logger.error("Failed response text: %s", await response.text())

                    return

                data = await response.json()

                url = data.get("url")
                session_name = data.get("name")

                if url:
                    return {
                        "clusterName": self.portal.cluster.name,
                        "portalName": self.portal.name,
                        "environmentName": environment_name,
                        "sessionName": session_name,
                        "clientUserId": user_id,
                        "sessionActivationUrl": f"{self.portal.url}{url}",
                    }

        except ClientConnectorError as exc:
            logger.error(
                "Failed to connect to portal %s of cluster %s when attempting to request session for user %s: %s",  # pylint: disable=line-too-long
                self.portal.name,
                self.portal.cluster.name,
                user_id,
                exc,
            )

        except ClientError as exc:
            logger.error(
                "Failed to request workshop session from portal %s of cluster %s for user %s: %s",
                self.portal.name,
                self.portal.cluster.name,
                user_id,
                exc,
            )

        except Exception as exc:
            logger.exception(
                "Unexpected exception when requesting workshop session from portal %s of cluster %s for user %s",
                self.portal.name,
                self.portal.cluster.name,
                user_id,
            )
