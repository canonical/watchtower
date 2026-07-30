#!/usr/bin/env python3
# Copyright 2026 Canonical Ltd.
# See LICENSE file for licensing details.

"""Watchtower charm entrypoint."""

import logging
import typing

import ops
import paas_charm.go
from paas_charm.app import App

from charms.temporal_k8s.v0.temporal_host_info import (
    TemporalHostInfoRequirer,
    TemporalHostInfoChangedEvent,
)

logger = logging.getLogger(__name__)

# Mapping from Juju config option name (kebab-case) to the env var
# name the Go app actually reads.  The go-framework extension would
# normally inject these as APP_<UPPER> but config.go reads the bare
# names, so we override the prefix to "" in _create_app().
#
# Additionally, two secrets are injected at runtime and are never
# stored as plain config options:
#   mattermost-bot-token -> MATTERMOST_BOT_TOKEN
#   openrouter-api-key   -> OPENROUTER_API_KEY
_SECRET_LABELS = {
    "mattermost-bot-token": "MATTERMOST_BOT_TOKEN",
    "openrouter-api-key": "OPENROUTER_API_KEY",
}


class _UnprefixedApp(App):
    """App subclass that strips the APP_ prefix from user config env vars.

    The watchtower binary reads env vars by their original names
    (e.g. TEMPORAL_HOST, not APP_TEMPORAL_HOST), so we set both
    configuration_prefix and framework_config_prefix to "".

    Extra env vars (TEMPORAL_HOST, secrets) are injected via the
    extra_env constructor argument and merged into gen_environment().
    """

    def __init__(
        self,
        *,
        extra_env: dict[str, str] | None = None,
        **kwargs: typing.Any,
    ) -> None:
        kwargs.setdefault("configuration_prefix", "")
        kwargs.setdefault("framework_config_prefix", "")
        super().__init__(**kwargs)
        self._extra_env: dict[str, str] = extra_env or {}

    def gen_environment(self) -> dict[str, str]:
        """Generate environment, merging base env with extra vars.

        Returns:
            Combined environment dictionary.
        """
        env = super().gen_environment()
        env.update(self._extra_env)
        return env


class WatchtowerCharm(paas_charm.go.Charm):
    """Watchtower charm - Go service with Temporal relation and Juju secrets."""

    def __init__(self, *args: typing.Any) -> None:
        """Initialize the charm.

        Args:
            args: passthrough to CharmBase.
        """
        super().__init__(*args)

        # Wire up the temporal-host-info relation library.
        self._temporal = TemporalHostInfoRequirer(self)
        self.framework.observe(
            self._temporal.on.temporal_host_info_changed,
            self._on_temporal_host_info_changed,
        )
        self.framework.observe(
            self._temporal.on.temporal_host_info_unavailable,
            self._on_temporal_host_info_unavailable,
        )

    # ------------------------------------------------------------------
    # Temporal relation handlers
    # ------------------------------------------------------------------

    def _on_temporal_host_info_changed(
        self, event: TemporalHostInfoChangedEvent
    ) -> None:
        """Handle Temporal host-info relation data becoming available.

        Args:
            event: the TemporalHostInfoChangedEvent carrying host/port.
        """
        logger.info(
            "temporal relation updated: %s:%s", event.host, event.port
        )
        self.restart()

    def _on_temporal_host_info_unavailable(self, _: ops.EventBase) -> None:
        """Handle Temporal host-info relation being broken."""
        logger.warning("temporal relation lost")
        self.update_app_and_unit_status(
            ops.BlockedStatus("Waiting for temporal relation")
        )

    # ------------------------------------------------------------------
    # Override: readiness gate
    # ------------------------------------------------------------------

    def is_ready(self) -> bool:
        """Block the workload until the temporal relation is present.

        Returns:
            True only when the base class is ready AND temporal is connected.
        """
        if not super().is_ready():
            return False
        if self._temporal.host is None or self._temporal.port is None:
            self._create_app().stop_all_services()
            self.update_app_and_unit_status(
                ops.BlockedStatus("Waiting for temporal relation")
            )
            return False
        return True

    # ------------------------------------------------------------------
    # Override: environment generation
    # ------------------------------------------------------------------

    def _create_app(self) -> App:
        """Build an App instance with no env-var prefix and secrets injected.

        Returns:
            A configured _UnprefixedApp instance.
        """
        charm_state = self._create_charm_state()
        return _UnprefixedApp(
            container=self._container,
            charm_state=charm_state,
            workload_config=self._workload_config,
            database_migration=self._database_migration,
            extra_env={**self._temporal_env(), **self._secrets_env()},
        )

    # ------------------------------------------------------------------
    # Private helpers
    # ------------------------------------------------------------------

    def _temporal_env(self) -> dict[str, str]:
        """Return TEMPORAL_HOST env var if the relation is present.

        Returns:
            Dict with TEMPORAL_HOST set, or empty dict if not yet known.
        """
        host = self._temporal.host
        port = self._temporal.port
        if host and port:
            return {"TEMPORAL_HOST": f"{host}:{port}"}
        return {}

    def _secrets_env(self) -> dict[str, str]:
        """Read Juju secrets and return their env var values.

        Secrets are looked up by label; missing secrets are silently skipped.

        Returns:
            Dict of env var name -> secret value for each configured secret.
        """
        result: dict[str, str] = {}
        for label, env_var in _SECRET_LABELS.items():
            value = self._get_secret_value(label)
            if value is not None:
                result[env_var] = value
        return result

    def _get_secret_value(self, label: str) -> str | None:
        """Retrieve the 'value' field of a Juju secret by label.

        Args:
            label: the Juju secret label.

        Returns:
            The secret value string, or None if the secret is absent.
        """
        try:
            secret = self.model.get_secret(label=label)
            return secret.get_content(refresh=True).get("value")
        except ops.SecretNotFoundError:
            logger.debug("secret %r not found, skipping", label)
            return None


if __name__ == "__main__":
    ops.main(WatchtowerCharm)
