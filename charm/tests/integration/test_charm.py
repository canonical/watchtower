# Copyright 2026 Canonical Ltd.
# See LICENSE file for licensing details.

"""Integration tests for the Watchtower charm.

These tests require a live Juju K8s environment with a Temporal operator
available. They are skipped by default in CI. See conftest.py for setup
instructions.

To run:
    cd charm && uv run --all-extras pytest tests/integration/ -v \
        --charm-path ../../watchtower-k8s_amd64.charm
"""

import pytest

from . import APP

# Mark all tests in this module as integration tests.
pytestmark = pytest.mark.skip(
    reason=(
        "Integration tests require a live Juju/K8s environment with Temporal. "
        "Run manually with: cd charm && "
        "uv run --all-extras pytest tests/integration/ -v"
    )
)


def test_deploy(juju, charm_file, app_oci_image):
    """Charm deploys and reaches a known status."""
    juju.deploy(
        charm_file,
        app=APP,
        resources={"app-image": app_oci_image},
    )
    # Without Temporal relation the charm should be blocked.
    juju.wait(
        lambda status: status.apps[APP].app_status.current == "blocked",
        timeout=300,
    )


def test_blocked_without_temporal(juju):
    """Without the temporal-host-info relation the charm stays blocked."""
    status = juju.status()
    app_status = status.apps[APP].app_status
    assert app_status.current == "blocked"
    assert "temporal" in app_status.message.lower()


def test_active_after_temporal_relation(juju, charm_file):
    """After integrating with Temporal the charm becomes active.

    This test is a placeholder: in a full environment you would deploy
    temporal-k8s, relate it, and assert ActiveStatus.
    """
    pytest.skip("Requires temporal-k8s operator in the test environment")
