# Copyright 2026 Canonical Ltd.
# See LICENSE file for licensing details.

"""Unit tests for the Watchtower charm.

These tests use ops[testing] (scenario) and exercise the charm logic
without any real Juju environment or container runtime.
"""

from pathlib import Path

import yaml
from ops.testing import (
    ActiveStatus,
    BlockedStatus,
    Container,
    Context,
    Relation,
    Secret,
    State,
    WaitingStatus,
)

from charm import WatchtowerCharm

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

TEMPORAL_ENDPOINT = "temporal-host-info"
CONTAINER_NAME = "app"

# charmcraft.yaml lives at the repo root, two levels above charm/src/
_CHARMCRAFT_YAML = Path(__file__).parent.parent.parent.parent / "charmcraft.yaml"
_CHARM_META = yaml.safe_load(_CHARMCRAFT_YAML.read_text())


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _ctx() -> Context:
    """Return a fresh Context for WatchtowerCharm.

    We pass the full charmcraft.yaml as ``meta`` so scenario can extract the
    metadata, config options, and relation definitions without needing to look
    up the file from the source tree (which would fail since charmcraft.yaml
    is at the repo root, not directly adjacent to the charm source).
    """
    return Context(WatchtowerCharm, meta=_CHARM_META)


def _container(can_connect: bool = True) -> Container:
    """Return a minimal app container."""
    return Container(name=CONTAINER_NAME, can_connect=can_connect)


def _temporal_relation(host: str = "temporal.svc", port: int = 7233) -> Relation:
    """Return a temporal-host-info relation with provider data set."""
    return Relation(
        endpoint=TEMPORAL_ENDPOINT,
        remote_app_data={"host": host, "port": str(port)},
    )


# ---------------------------------------------------------------------------
# pebble_ready
# ---------------------------------------------------------------------------


class TestPebbleReady:
    """Tests for the pebble_ready event."""

    def test_pebble_ready_no_temporal_sets_blocked(self):
        """Without a temporal relation the charm should set BlockedStatus."""
        ctx = _ctx()
        container = _container()
        state = State(containers=[container])

        result = ctx.run(ctx.on.pebble_ready(container=container), state)

        assert result.unit_status == BlockedStatus("Waiting for temporal-host-info relation")

    def test_pebble_ready_with_temporal_sets_active(self):
        """With a valid temporal relation the charm should reach ActiveStatus."""
        ctx = _ctx()
        container = _container()
        relation = _temporal_relation()
        state = State(containers=[container], relations=[relation])

        result = ctx.run(ctx.on.pebble_ready(container=container), state)

        assert result.unit_status == ActiveStatus()

    def test_pebble_ready_container_not_ready_sets_waiting(self):
        """If pebble is not reachable yet, the charm should set WaitingStatus."""
        ctx = _ctx()
        container = _container(can_connect=False)
        relation = _temporal_relation()
        state = State(containers=[container], relations=[relation])

        result = ctx.run(ctx.on.pebble_ready(container=container), state)

        assert result.unit_status == WaitingStatus("Waiting for pebble")


# ---------------------------------------------------------------------------
# config_changed
# ---------------------------------------------------------------------------


class TestConfigChanged:
    """Tests for the config_changed event."""

    def test_config_changed_with_temporal_stays_active(self):
        """Config changes when temporal is present should keep ActiveStatus."""
        ctx = _ctx()
        container = _container()
        relation = _temporal_relation()
        state = State(
            containers=[container],
            relations=[relation],
            config={"mattermost-server-url": "http://mm.example.com"},
        )

        result = ctx.run(ctx.on.config_changed(), state)

        assert result.unit_status == ActiveStatus()

    def test_config_changed_no_temporal_sets_blocked(self):
        """Config changes without temporal should set BlockedStatus."""
        ctx = _ctx()
        container = _container()
        state = State(containers=[container])

        result = ctx.run(ctx.on.config_changed(), state)

        assert result.unit_status == BlockedStatus("Waiting for temporal-host-info relation")

    def test_config_changed_no_pebble_sets_waiting(self):
        """Config changes when pebble is unreachable should set WaitingStatus."""
        ctx = _ctx()
        container = _container(can_connect=False)
        relation = _temporal_relation()
        state = State(containers=[container], relations=[relation])

        result = ctx.run(ctx.on.config_changed(), state)

        assert result.unit_status == WaitingStatus("Waiting for pebble")


# ---------------------------------------------------------------------------
# secret_changed
# ---------------------------------------------------------------------------


class TestSecretChanged:
    """Tests for the secret_changed event."""

    def test_secret_changed_with_temporal_stays_active(self):
        """A secret rotation with temporal present should keep ActiveStatus."""
        ctx = _ctx()
        container = _container()
        relation = _temporal_relation()
        secret = Secret(
            tracked_content={"value": "new-token"},
            id="secret:abc123",
            label="mattermost-bot-token",
        )
        state = State(
            containers=[container],
            relations=[relation],
            secrets=[secret],
        )

        result = ctx.run(ctx.on.secret_changed(secret=secret), state)

        assert result.unit_status == ActiveStatus()

    def test_secret_changed_no_temporal_sets_blocked(self):
        """A secret rotation without temporal should set BlockedStatus."""
        ctx = _ctx()
        container = _container()
        secret = Secret(
            tracked_content={"value": "new-token"},
            id="secret:abc123",
        )
        state = State(containers=[container], secrets=[secret])

        result = ctx.run(ctx.on.secret_changed(secret=secret), state)

        assert result.unit_status == BlockedStatus("Waiting for temporal-host-info relation")


# ---------------------------------------------------------------------------
# temporal relation events
# ---------------------------------------------------------------------------


class TestTemporalRelation:
    """Tests for temporal-host-info relation events."""

    def test_relation_changed_with_data_sets_active(self):
        """When temporal relation data arrives the charm should become active."""
        ctx = _ctx()
        container = _container()
        relation = _temporal_relation(host="temporal.svc", port=7233)
        state = State(containers=[container], relations=[relation])

        result = ctx.run(ctx.on.relation_changed(relation=relation), state)

        assert result.unit_status == ActiveStatus()

    def test_relation_broken_sets_blocked(self):
        """When the temporal relation is removed the charm should block."""
        ctx = _ctx()
        container = _container()
        relation = _temporal_relation()
        state = State(containers=[container], relations=[relation])

        result = ctx.run(ctx.on.relation_broken(relation=relation), state)

        assert result.unit_status == BlockedStatus("Waiting for temporal-host-info relation")

    def test_relation_changed_no_data_yet_no_status_change(self):
        """An empty relation update (no host/port yet) should not change status.

        The TemporalHostInfoRequirer library only emits temporal_host_info_changed
        when both host and port are present in the relation data. An empty
        relation_changed event is silently ignored, so the unit status stays
        at whatever it was before (UnknownStatus in a fresh test state).
        """
        ctx = _ctx()
        container = _container()
        # Relation exists but no remote_app_data yet
        relation = Relation(endpoint=TEMPORAL_ENDPOINT)
        state = State(containers=[container], relations=[relation])

        result = ctx.run(ctx.on.relation_changed(relation=relation), state)

        # Status unchanged - charm took no action because relation had no data
        assert result.unit_status == state.unit_status


# ---------------------------------------------------------------------------
# Pebble layer content
# ---------------------------------------------------------------------------


class TestPebbleLayer:
    """Tests verifying the pebble layer pushed to the container."""

    def test_layer_contains_service_definition(self):
        """After pebble_ready the container should have a watchtower service."""
        ctx = _ctx()
        container = _container()
        relation = _temporal_relation(host="temporal.svc", port=7233)
        state = State(containers=[container], relations=[relation])

        result = ctx.run(ctx.on.pebble_ready(container=container), state)

        out_container = result.get_container(CONTAINER_NAME)
        assert "watchtower" in out_container.layers

        service = out_container.layers["watchtower"].services.get("watchtower")
        assert service is not None
        assert service.startup == "enabled"

    def test_layer_sets_temporal_host_env_var(self):
        """TEMPORAL_HOST uses the stable K8s service DNS name.

        The ephemeral pod IP supplied by the relation is ignored; only the
        port is taken from relation data.
        """
        ctx = _ctx()
        container = _container()
        # Simulate a different pod IP in the relation to confirm it is
        # ignored in favour of the fixed service DNS name.
        relation = _temporal_relation(host="10.1.99.1", port=7233)
        state = State(containers=[container], relations=[relation])

        result = ctx.run(ctx.on.pebble_ready(container=container), state)

        out_container = result.get_container(CONTAINER_NAME)
        env = out_container.layers["watchtower"].services["watchtower"].environment
        assert env.get("TEMPORAL_HOST") == "temporal-k8s:7233"

    def test_layer_propagates_config_env_vars(self):
        """Config options should be forwarded as env vars to the service."""
        ctx = _ctx()
        container = _container()
        relation = _temporal_relation()
        state = State(
            containers=[container],
            relations=[relation],
            config={
                "mattermost-server-url": "http://mm.local",
                "watchtower-keyword": "@wt",
                "test-observer-url": "https://tests-api.example.com",
            },
        )

        result = ctx.run(ctx.on.pebble_ready(container=container), state)

        out_container = result.get_container(CONTAINER_NAME)
        env = out_container.layers["watchtower"].services["watchtower"].environment
        assert env.get("MATTERMOST_SERVER_URL") == "http://mm.local"
        assert env.get("WATCHTOWER_KEYWORD") == "@wt"
        assert env.get("TEST_OBSERVER_URL") == "https://tests-api.example.com"

    def test_empty_config_values_not_propagated(self):
        """Config options left at their empty default should not appear in env."""
        ctx = _ctx()
        container = _container()
        relation = _temporal_relation()
        state = State(
            containers=[container],
            relations=[relation],
            config={"mattermost-server-url": ""},
        )

        result = ctx.run(ctx.on.pebble_ready(container=container), state)

        out_container = result.get_container(CONTAINER_NAME)
        env = out_container.layers["watchtower"].services["watchtower"].environment
        assert "MATTERMOST_SERVER_URL" not in env

    def test_layer_contains_health_check(self):
        """The pebble layer should include a HTTP health check on /healthz."""
        ctx = _ctx()
        container = _container()
        relation = _temporal_relation()
        state = State(containers=[container], relations=[relation])

        result = ctx.run(ctx.on.pebble_ready(container=container), state)

        out_container = result.get_container(CONTAINER_NAME)
        checks = out_container.layers["watchtower"].checks
        assert "ready" in checks
        assert checks["ready"].http == {"url": "http://localhost:8080/healthz"}
