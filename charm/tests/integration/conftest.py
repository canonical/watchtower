# Copyright 2026 Canonical Ltd.
# See LICENSE file for licensing details.

"""Pytest fixtures for Watchtower integration tests.

Integration tests require a live Juju/K8s environment bootstrapped with a
Temporal operator. They are not run in the standard CI pipeline (which only
runs unit tests). To run them locally:

    # 1. Bootstrap a Juju K8s controller (e.g. MicroK8s)
    juju bootstrap microk8s

    # 2. Pack the charm and rock
    make charm-pack rock

    # 3. Run integration tests
    cd charm && uv run --all-extras pytest tests/integration/ -v

Pass --charm-path <path.charm> to reuse a pre-built charm file.
"""

import subprocess
from pathlib import Path

import jubilant
import yaml
from pytest import fixture


@fixture(scope="module")
def juju():
    """Spin up a temporary Juju model for the test module, then clean up."""
    with jubilant.temp_model() as juju:
        yield juju


@fixture(scope="module")
def charm_file(request):
    """Return the path to the packed charm file.

    If ``--charm-path`` is passed on the pytest command line, that path is
    used directly. Otherwise the charm is built with ``charmcraft pack``.
    """
    charm_path = request.config.getoption("--charm-path", default=None)
    if charm_path:
        return charm_path

    repo_root = Path(__file__).parent.parent.parent.parent
    subprocess.run(
        ["/snap/bin/charmcraft", "pack", "--verbose"],
        cwd=repo_root,
        check=True,
    )
    charm_files = list(repo_root.glob("*.charm"))
    assert charm_files, "charmcraft pack produced no .charm file"
    return str(charm_files[0].absolute())


@fixture(scope="module")
def app_oci_image():
    """Return the OCI image reference from charmcraft.yaml upstream-source."""
    repo_root = Path(__file__).parent.parent.parent.parent
    meta = yaml.safe_load((repo_root / "charmcraft.yaml").read_text())
    return meta["resources"]["app-image"].get("upstream-source", "")


def pytest_addoption(parser):
    """Register --charm-path CLI option."""
    parser.addoption(
        "--charm-path",
        action="store",
        default=None,
        help="Path to a pre-built .charm file to use instead of packing.",
    )
