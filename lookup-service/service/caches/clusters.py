"""Configuration for target clusters."""

from dataclasses import dataclass
from typing import TYPE_CHECKING, Any, Dict, List

if TYPE_CHECKING:
    from .portals import TrainingPortal


@dataclass
class ClusterConfig:
    """Configuration object for a target cluster. This includes a database of
    the training portals hosted on the cluster."""

    name: str
    uid: str
    labels: List[Dict[str, str]]
    kubeconfig: Dict[str, Any]
    local: bool
    portals: Dict[str, "TrainingPortal"]

    def __init__(
        self,
        name: str,
        uid: str,
        labels: List[Dict[str, str]],
        kubeconfig: Dict[str, Any],
        local: bool = False,
    ):
        self.name = name
        self.uid = uid
        self.labels = labels
        self.kubeconfig = kubeconfig
        self.local = local
        self.portals = {}

    def add_portal(self, portal: "TrainingPortal") -> None:
        """Add a portal to the cluster."""

        self.portals[portal.name] = portal

    def remove_portal(self, name: str) -> None:
        """Remove a portal from the cluster."""

        self.portals.pop(name, None)

    def get_portals(self) -> List["TrainingPortal"]:
        """Retrieve a list of portals from the cluster."""

        return list(self.portals.values())

    def get_portal(self, name: str) -> "TrainingPortal":
        """Retrieve a portal from the cluster by name."""

        return self.portals.get(name)
