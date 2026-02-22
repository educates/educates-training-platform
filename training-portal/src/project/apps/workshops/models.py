"""Application database models for Django."""

import json
import enum

from datetime import timedelta

from django.db import models
from django.contrib.auth import get_user_model
from django.utils import timezone
from django.utils.html import format_html
from django.urls import reverse
from django.db.models import Sum

from oauth2_provider.models import Application, AbstractApplication


User = get_user_model()


class CustomApplication(AbstractApplication):
    """Custom OAuth2 Application model that supports both password and
    client_credentials grant types during transition period.

    """

    def allows_grant_type(self, *grant_types):
        return bool(
            set([self.authorization_grant_type, self.GRANT_CLIENT_CREDENTIALS])
            & set(grant_types)
        )

    class Meta:
        swappable = "OAUTH2_PROVIDER_APPLICATION_MODEL"


class JSONField(models.Field):
    """Helper class for storing JSON data as field in Django data model. Can
    remove this and use native support once update to Django 3.1.

    """

    def db_type(self, connection):
        return "text"

    def from_db_value(
        self, value, expression, connection
    ):  # pylint: disable=unused-argument
        if value is not None:
            return self.to_python(value)
        return value

    def to_python(self, value):
        if value is not None:
            try:
                return json.loads(value)
            except (TypeError, ValueError):
                return value
        return value

    def get_prep_value(self, value):
        if value is not None:
            return str(json.dumps(value))
        return value

    def value_to_string(self, obj):
        return self.value_from_object(obj)


class TrainingPortal(models.Model):
    """Database model type representing the training portal."""

    name = models.CharField(
        verbose_name="portal name", max_length=255, primary_key=True
    )
    labels = JSONField(default={}, verbose_name="portal labels")
    uid = models.CharField(verbose_name="resource uid", max_length=255, default="")
    generation = models.BigIntegerField(verbose_name="generation", default=0)
    sessions_maximum = models.IntegerField(verbose_name="sessions maximum", default=0)
    sessions_registered = models.IntegerField(
        verbose_name="sessions registered", default=0
    )
    sessions_anonymous = models.IntegerField(
        verbose_name="sessions anonymous", default=0
    )
    default_labels = JSONField(default={}, verbose_name="default labels")
    default_capacity = models.IntegerField(verbose_name="default capacity", default=0)
    default_reserved = models.IntegerField(
        verbose_name="default reserved", null=True, default=None
    )
    default_initial = models.IntegerField(
        verbose_name="default initial", null=True, default=None
    )
    default_expires = models.CharField(
        verbose_name="default expires", max_length=32, default=""
    )
    default_overtime = models.CharField(
        verbose_name="default overtime", max_length=32, default=""
    )
    default_deadline = models.CharField(
        verbose_name="default deadline", max_length=32, default=""
    )
    default_orphaned = models.CharField(
        verbose_name="default orphaned", max_length=32, default=""
    )
    default_overdue = models.CharField(
        verbose_name="default overdue", max_length=32, default=""
    )
    default_refresh = models.CharField(
        verbose_name="default refresh", max_length=32, default=""
    )
    default_registry = JSONField(verbose_name="default registry", default={})
    default_env = JSONField(verbose_name="default environment", default=[])
    update_workshop = models.BooleanField(
        verbose_name="workshop updates", default=False
    )

    def starting_environments(self):
        """Returns the set of workshop environments which are still in the
        process of being setup.

        """

        return self.environment_set.filter(state=EnvironmentState.STARTING)

    def running_environments(self):
        """Returns the set of workshop environments which are running and
        against which workshop sessions can be created. These are returned
        in order based on their position in the training portal resource
        definition.

        """

        return self.environment_set.filter(state=EnvironmentState.RUNNING).order_by(
            "position"
        )

    def active_environments(self):
        """Returns the set of all active workshop environments. This contains
        workshop environments which are already running, as well as those
        which are still in the process of being setup.

        """

        return self.environment_set.filter(
            state__in=(
                EnvironmentState.STARTING,
                EnvironmentState.RUNNING,
            )
        )

    def stopping_environments(self):
        """Returns the set of workshop environments which are in the process
        of being shutdown. These may still have active workshop sessions
        against them, but no new workshop sessions should be created against
        them.

        """

        return self.environment_set.filter(state=EnvironmentState.STOPPING)

    def environments_in_state(self, states):
        """Returns the set of workshop environments in one of the specified
        states.

        """

        return self.environment_set.filter(state__in=states)

    def environment_for_workshop(self, name):
        """Returns the current active workshop environment for the named
        workshop. This can be a running workshop environment or one which
        is in the process of being setup. If trying to determine if a new
        workshop session can be created against it, you need to separately
        check whether it is in the running state. Will return None if there
        is no active named workshop.

        """

        try:
            return self.environment_set.get(
                workshop_name=name,
                state__in=(
                    EnvironmentState.STARTING,
                    EnvironmentState.RUNNING,
                ),
            )
        except Environment.DoesNotExist:
            pass

    def environments_for_resource(self, name):
        """Returns any current active workshop environments for the named
        resource. These can be a running workshop environment or one which
        is in the process of being setup. If trying to determine if a new
        workshop session can be created against it, you need to separately
        check whether it is in the running state. Will return empty list if
        there are no matching environments.

        """

        return self.environment_set.filter(
            resource_name=name,
            state__in=(
                EnvironmentState.STARTING,
                EnvironmentState.RUNNING,
            ),
        )

    def workshop_environment(self, name):
        """Returns the named workshop environment. This can be a running
        workshop environment or one which is in the process of being setup.

        """

        try:
            return self.environment_set.get(
                name=name,
                state__in=(
                    EnvironmentState.STARTING,
                    EnvironmentState.RUNNING,
                ),
            )
        except Environment.DoesNotExist:
            pass

    def overall_capacity(self):
        """Returns the notional maximum number of workshop sessions that can
        be created. This will either be the maximum session count defined for
        the training portal, if none is defined, the total capacity count
        across all workshop environments.

        """

        if self.sessions_maximum:
            return self.sessions_maximum
        return Environment.objects.filter(
            portal=self, state=EnvironmentState.RUNNING
        ).aggregate(capacity=Sum("capacity"))["capacity"]

    overall_capacity.short_description = "Overall Capacity"

    def available_sessions(self):
        """Returns the set of reserved sessions in existence across all
        workshop environments.

        """

        return Session.objects.filter(
            environment__portal=self,
            owner__isnull=True,
            state__in=(SessionState.STARTING, SessionState.WAITING),
        )

    def available_sessions_count(self):
        """Returns a count of the number of reserved sessions in existence
        across all workshop environments.

        """

        return self.available_sessions().count()

    available_sessions_count.short_description = "Available"

    def allocated_sessions(self):
        """Returns the set of workshop sessions across all workshop
        environments that are active and allocated to a user. This includes
        any workshop sessions which are in the process of being shutdown.

        """

        return (
            Session.objects.filter(environment__portal=self)
            .exclude(owner__isnull=True)
            .exclude(state=SessionState.STOPPED)
        )

    def allocated_sessions_count(self):
        """Returns a count of the number of workshop sessions across all
        workshop environments that are active and allocated to a user. This
        includes any workshop sessions which are in the process of being
        shutdown.

        """

        return self.allocated_sessions().count()

    allocated_sessions_count.short_description = "Allocated"

    def active_sessions(self):
        """Returns the set of active workshop sessions across all workshop
        environments. This includes any reserved workshop sessions that have
        not as yet been allocated to a user, as well as workshop sessions
        which are currently being shutdown.

        """

        return Session.objects.filter(environment__portal=self).exclude(
            state=SessionState.STOPPED
        )

    def active_sessions_count(self):
        """Returns the count of active workshop sessions across all workshop
        environments. This includes any reserved workshop sessions that have
        not as yet been allocated to a user, as well as workshop sessions
        which are currently being shutdown.

        """

        return self.active_sessions().count()

    active_sessions_count.short_description = "Active"

    def all_sessions(self):
        """Returns the set of all workshop sessions across all workshop
        environments. This includes any reserved workshop sessions that have
        not as yet been allocated to a user, workshop sessions which are
        currently being shutdown, as well as stopped workshop sessions that
        haven't been purged.

        """

        return Session.objects.filter(environment__portal=self)

    def all_sessions_count(self):
        """Returns the count of all workshop sessions across all workshop
        environments. This includes any reserved workshop sessions that have
        not as yet been allocated to a user, workshop sessions which are
        currently being shutdown, as well as stopped workshop sessions that
        haven't been purged.

        """

        return self.total_sessions().count()

    all_sessions_count.short_description = "Total"

    def capacity_available(self):
        """Returns whether there is capacity to have another workshop session.
        This will always return True if no sessions maximum was specified for
        the training portal. In either case, if returns True, you still need
        to check whether a specific workshop environment has capacity.

        """

        if not self.sessions_maximum:
            return True

        return self.allocated_sessions_count() < self.sessions_maximum

    def allocated_session(self, name, user=None):
        """Returns the allocated workshop session with the specified name.
        Optionally validates whether allocated to the specified user and
        only returns the workshop session if it is.

        """

        try:
            session = Session.objects.get(
                name=name,
                environment__portal=self,
                state__in=(
                    SessionState.STARTING,
                    SessionState.WAITING,
                    SessionState.RUNNING,
                    SessionState.STOPPING,
                ),
            )
            if user:
                if session.owner == user:
                    return session

            else:
                return session

        except Session.DoesNotExist:
            pass

    def allocated_sessions_for_user(self, user):
        """Returns the set of all workshop sessions allocated across all
        workshop environments for the specified user.

        """

        return Session.objects.filter(environment__portal=self, owner=user).exclude(
            state=SessionState.STOPPED
        )

    def stopped_session(self, name, user=None):
        """Returns any stopped workshop session with the specified name.
        Optionally validates whether allocated to the specified user and
        only returns the workshop session if it is.

        """

        try:
            session = Session.objects.get(
                name=name,
                environment__portal=self,
                state__in=(
                    SessionState.STOPPED,
                ),
            )
            if user:
                if session.owner == user:
                    return session

            else:
                return session

        except Session.DoesNotExist:
            pass

    def session_permitted_for_user(self, user):
        """Returns where the specified user is permitted to create a workshop
        session. If the user is staff, they are always permitted. For non
        staff user that may be prohibited from creating a workshop session if
        they have already exceeded the maximum allowed number of workshop
        sessions allowed for the type of user.

        """

        if user.is_staff:
            return True

        sessions = self.allocated_sessions_for_user(user)

        if user.groups.filter(name="anonymous").exists():
            if self.sessions_anonymous:
                if sessions.count() >= self.sessions_anonymous:
                    return False

        else:
            if self.sessions_registered:
                if sessions.count() >= self.sessions_registered:
                    return False

        return True


class Workshop(models.Model):
    name = models.CharField(verbose_name="workshop name", max_length=255)
    uid = models.CharField(verbose_name="resource uid", max_length=255)
    generation = models.BigIntegerField(verbose_name="generation")
    title = models.CharField(max_length=255)
    description = models.TextField()
    vendor = models.CharField(max_length=128)
    authors = JSONField(default=[])
    difficulty = models.CharField(max_length=128)
    duration = models.CharField(max_length=128)
    tags = JSONField(default=[])
    labels = JSONField(default={})
    logo = models.TextField()
    url = models.CharField(max_length=255)
    ingresses = JSONField(verbose_name="session ingresses", default=[])
    params = JSONField(verbose_name="session parameters", default=[])


class EnvironmentState(enum.IntEnum):
    STARTING = 1
    RUNNING = 2
    STOPPING = 3
    STOPPED = 4

    @classmethod
    def choices(cls):
        return [(key.value, key.name) for key in cls]


class Environment(models.Model):
    portal = models.ForeignKey(TrainingPortal, on_delete=models.PROTECT)
    workshop_name = models.CharField(verbose_name="workshop name", max_length=256)
    resource_name = models.CharField(verbose_name="resource name", max_length=256, default="")
    workshop = models.ForeignKey(Workshop, null=True, on_delete=models.PROTECT)
    name = models.CharField(verbose_name="environment name", max_length=255, default="")
    uid = models.CharField(verbose_name="resource uid", max_length=255, default="")
    created_at = models.DateTimeField(auto_now_add=True)
    state = models.IntegerField(
        choices=EnvironmentState.choices(), default=EnvironmentState.STARTING
    )
    position = models.IntegerField(verbose_name="index position", default=0)
    capacity = models.IntegerField(verbose_name="maximum capacity", default=0)
    initial = models.IntegerField(verbose_name="initial instances", default=0)
    reserved = models.IntegerField(verbose_name="reserved instances", default=0)
    expires = models.DurationField(
        verbose_name="workshop duration", default=timedelta()
    )
    overtime = models.DurationField(verbose_name="overtime period", default=timedelta())
    deadline = models.DurationField(
        verbose_name="maximum deadline", default=timedelta()
    )
    orphaned = models.DurationField(
        verbose_name="inactivity timeout", default=timedelta()
    )
    overdue = models.DurationField(verbose_name="startup timeout", default=timedelta())
    refresh = models.DurationField(verbose_name="refresh interval", default=timedelta())
    registry = JSONField(verbose_name="registry override", default={})
    env = JSONField(verbose_name="environment overrides", default=[])
    labels = JSONField(verbose_name="label overrides", default={})
    tally = models.IntegerField(verbose_name="workshop tally", default=0)

    def portal_name(self):
        return self.portal.name

    portal_name.admin_order_field = "portal__name"

    def workshop_link(self):
        if self.workshop:
            return format_html(
                '<a href="{}">{}</a>',
                reverse("admin:workshops_workshop_change", args=[self.workshop.id]),
                self.workshop.name,
            )
        else:
            return self.workshop_name

    workshop_link.short_description = "Workshop"

    def is_starting(self):
        return self.state == EnvironmentState.STARTING

    is_starting.short_description = "Starting"
    is_starting.boolean = True

    def is_running(self):
        return self.state == EnvironmentState.RUNNING

    is_running.short_description = "Running"
    is_running.boolean = True

    def is_stopping(self):
        return self.state == EnvironmentState.STOPPING

    is_stopping.short_description = "Stopping"
    is_stopping.boolean = True

    def is_stopped(self):
        return self.state == EnvironmentState.STOPPED

    is_stopped.short_description = "Stopped"
    is_stopped.boolean = True

    def mark_as_running(self):
        self.state = EnvironmentState.RUNNING
        self.save()
        return self

    def mark_as_stopping(self):
        self.state = EnvironmentState.STOPPING
        self.capacity = 0
        self.reserved = 0
        self.save()
        return self

    def mark_as_stopped(self):
        self.state = EnvironmentState.STOPPED
        self.capacity = 0
        self.reserved = 0
        self.save()
        return self

    def available_session(self):
        sessions = self.available_sessions()
        return sessions and sessions[0] or None

    def available_sessions(self):
        """Returns the set of available sessions that can be allocated to a
        user. This doesn't include starting state as the workshop session
        hasn't actually been created in the cluster at that point.

        """

        return self.session_set.filter(owner__isnull=True, state=SessionState.WAITING)

    def available_sessions_count(self):
        return self.available_sessions().count()

    available_sessions_count.short_description = "Available"

    def allocated_sessions(self):
        return self.session_set.exclude(owner__isnull=True).exclude(
            state=SessionState.STOPPED
        )

    def allocated_sessions_count(self):
        return self.allocated_sessions().count()

    allocated_sessions_count.short_description = "Allocated"

    def active_sessions(self):
        """Returns the set of active workshop sessions. This includes any
        reserved workshop sessions that have not as yet been allocated to a
        user, as well as workshop sessions which are currently being started
        or shutdown.

        """

        return self.session_set.exclude(state=SessionState.STOPPED)

    def active_sessions_count(self):
        """Returns the count of active workshop sessions. This includes any
        reserved workshop sessions that have not as yet been allocated to a
        user, as well as workshop sessions which are currently being shutdown.

        """

        return self.active_sessions().count()

    active_sessions_count.short_description = "Active"

    def all_sessions(self):
        """Returns the set of all workshop sessions. This includes any
        reserved workshop sessions that have not as yet been allocated to a
        user, workshop sessions which are currently being shutdown, as well as
        stopped workshop sessions that haven't been purged.

        """

        return self.session_set.all()

    def all_sessions_count(self):
        """Returns the count of all workshop sessions. This includes any
        reserved workshop sessions that have not as yet been allocated to a
        user,workshop sessions which are currently being shutdown, as well as
        stopped workshop sessions that haven't been purged.

        """

        return self.all_sessions().count()

    all_sessions_count.short_description = "Total"

    def allocated_session_for_user(self, user):
        """Returns any allocated workshop session for the defined user. There
        should only be at most one, so only need to return the first if one
        does exist. We don't include sessions which are in the process of
        stopping as once stopping we would never return it to the user again.

        """

        sessions = self.session_set.filter(owner=user).exclude(
            state__in=(SessionState.STOPPING, SessionState.STOPPED)
        )

        if sessions:
            return sessions[0]


class SessionState(enum.IntEnum):
    STARTING = 1
    WAITING = 2
    RUNNING = 3
    STOPPING = 4
    STOPPED = 5

    @classmethod
    def choices(cls):
        return [(key.value, key.name) for key in cls]


class Session(models.Model):
    name = models.CharField(
        verbose_name="session name", max_length=256, primary_key=True
    )
    id = models.CharField(verbose_name="session id", max_length=64)
    uid = models.CharField(verbose_name="resource uid", max_length=255, default="")
    environment = models.ForeignKey(Environment, on_delete=models.PROTECT)
    application = models.ForeignKey(
        Application, blank=True, null=True, on_delete=models.PROTECT
    )
    state = models.IntegerField(
        choices=SessionState.choices(), default=SessionState.STARTING
    )
    owner = models.ForeignKey(User, blank=True, null=True, on_delete=models.PROTECT)
    created = models.DateTimeField(null=True, blank=True)
    started = models.DateTimeField(null=True, blank=True)
    expires = models.DateTimeField(null=True, blank=True)
    token = models.CharField(
        verbose_name="activation token", max_length=256, null=True, blank=True
    )
    url = models.URLField(verbose_name="session url", null=True)
    params = JSONField(verbose_name="session params", default={})
    password = models.CharField(
        verbose_name="config password", max_length=256, null=True, blank=True
    )
    index_url = models.URLField(verbose_name="index url", null=True, blank=True)
    analytics_url = models.URLField(verbose_name="analytics url", null=True, blank=True)

    def environment_name(self):
        return self.environment.name

    environment_name.admin_order_field = "environment__name"

    def workshop_name(self):
        return self.environment.workshop.name

    workshop_name.admin_order_field = "environment__workshop__name"

    def workshop_link(self):
        if self.environment.workshop:
            return format_html(
                '<a href="{}">{}</a>',
                reverse(
                    "admin:workshops_workshop_change",
                    args=[self.environment.workshop.id],
                ),
                self.environment.workshop.name,
            )
        else:
            return self.environment.workshop_name

    workshop_link.short_description = "Workshop"

    def environment_link(self):
        return format_html(
            '<a href="{}">{}</a>',
            reverse("admin:workshops_environment_change", args=[self.environment.id]),
            self.environment.name,
        )

    environment_link.short_description = "Environment"

    def url_link(self):
        if self.url and self.is_running():
            return format_html(
                '<a href="{}" target="_blank">{}</a>',
                self.url,
                self.url,
            )
        else:
            return "-"

    url_link.short_description = "Session URL"

    def is_available(self):
        return self.owner is None and self.state in (
            SessionState.STARTING,
            SessionState.WAITING,
        )

    is_available.short_description = "Available"
    is_available.boolean = True

    def is_pending(self):
        return self.owner and self.state in (
            SessionState.STARTING,
            SessionState.WAITING,
        )

    is_pending.short_description = "Pending"
    is_pending.boolean = True

    def is_allocated(self):
        return self.owner is not None and self.state != SessionState.STOPPED

    is_allocated.short_description = "Allocated"
    is_allocated.boolean = True

    def is_starting(self):
        return self.state == SessionState.STARTING

    is_starting.short_description = "Starting"
    is_starting.boolean = True

    def is_running(self):
        return self.state == SessionState.RUNNING

    is_running.short_description = "Running"
    is_running.boolean = True

    def is_stopping(self):
        return self.state == SessionState.STOPPING

    is_stopping.short_description = "Stopping"
    is_stopping.boolean = True

    def is_stopped(self):
        return self.state == SessionState.STOPPED

    is_stopped.short_description = "Stopped"
    is_stopped.boolean = True

    def remaining_time(self):
        now = timezone.now()
        if self.is_allocated() and self.expires:
            if now >= self.expires:
                return 0

            return (self.expires - now).total_seconds()

    def remaining_time_as_string(self):
        remaining = self.remaining_time()
        if remaining is not None:
            return "%02d:%02d" % (remaining / 60, remaining % 60)

    remaining_time_as_string.short_description = "Remaining"

    def mark_as_pending(self, user, token=None, timeout=None):
        self.owner = user
        self.started = timezone.now()
        self.token = self.token or token
        if timeout is None:
            timeout = 60
        if token and timeout:
            self.expires = self.started + timedelta(seconds=timeout)
        elif self.environment.expires:
            self.expires = self.started + self.environment.expires
        self.save()
        return self

    def mark_as_waiting(self):
        self.state = SessionState.WAITING
        self.save()
        return self

    def mark_as_running(self, user=None):
        self.owner = user or self.owner
        self.state = SessionState.RUNNING
        self.started = timezone.now()
        if self.environment.expires:
            self.expires = self.started + self.environment.expires
        else:
            self.expires = None
        self.save()
        return self

    def mark_as_stopping(self):
        self.state = SessionState.STOPPING
        self.expires = timezone.now()
        self.save()
        return self

    def mark_as_stopped(self):
        application = self.application
        self.state = SessionState.STOPPED
        self.expires = timezone.now()
        self.application = None
        self.save()
        application.delete()
        return self

    def time_remaining(self):
        if self.expires:
            now = timezone.now()
            if self.expires > now:
                return int((self.expires - now).total_seconds())
            return 0

    def extension_threshold(self):
        return max(300, min(self.environment.expires.total_seconds(), 4 * 3600) / 4)

    def extension_duration(self):
        if self.environment.overtime.total_seconds():
            return max(300, self.environment.overtime.total_seconds())
        return self.extension_threshold()

    def is_expiring(self):
        if self.expires:
            return self.time_remaining() < self.extension_threshold()
        return False

    def is_extension_permitted(self):
        if self.state != SessionState.RUNNING:
            return False
        if not self.expires:
            return False
        if self.expires >= self.started + self.environment.deadline:
            return False
        return self.remaining_time() < self.extension_duration()

    def extend_time_remaining(self):
        if self.is_extension_permitted():
            self.expires = self.expires + timedelta(seconds=self.extension_duration())
            if self.expires > self.started + self.environment.deadline:
                self.expires = self.started + self.environment.deadline
            self.save()
            return True
        return False
