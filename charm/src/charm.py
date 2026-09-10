#!/usr/bin/env python3
# Copyright 2026 Canonical Ltd.
# See LICENSE file for licensing details.

"""Watchtower charm entrypoint."""

import logging

import ops
from charms.temporal_k8s.v0.temporal_host_info import (
    TemporalHostInfoChangedEvent,
    TemporalHostInfoRequirer,
)
from ops.pebble import APIError, ConnectionError, ProtocolError

logger = logging.getLogger(__name__)

# Container name must match charmcraft.yaml containers key.
CONTAINER = "app"
# The Go binary is the pebble service entrypoint.
SERVICE = "watchtower"
BINARY = "/usr/local/bin/watchtower"

# Mapping from charmcraft.yaml config option name (type: secret) to the
# env var name the Go app reads.
_SECRET_CONFIG_OPTIONS: dict[str, str] = {
    "mattermost-bot-token-secret-id": "MATTERMOST_BOT_TOKEN",
    "mattermost-poll-token-secret-id": "MATTERMOST_POLL_TOKEN",
    "openrouter-api-key-secret-id": "OPENROUTER_API_KEY",
}

# Config options that map directly to env vars (name -> env var).
# Charm config uses kebab-case; the Go app reads the env var.
_CONFIG_ENV_VARS: dict[str, str] = {
    "mattermost-server-url": "MATTERMOST_SERVER_URL",
    "mattermost-bot-user-id": "MATTERMOST_BOT_USER_ID",
    "watchtower-keyword": "WATCHTOWER_KEYWORD",
    "mattermost-broadcast-channel-ids": "MATTERMOST_BROADCAST_CHANNEL_IDS",
    "mattermost-reconnect-delay": "MATTERMOST_RECONNECT_DELAY",
    "mattermost-channel-id": "MATTERMOST_CHANNEL_ID",
    "mattermost-poll-interval": "MATTERMOST_POLL_INTERVAL",
    "test-observer-url": "TEST_OBSERVER_URL",
    "watchtower-releases-scope": "WATCHTOWER_RELEASES_SCOPE",
    "summary-for-products": "SUMMARY_FOR_PRODUCTS",
    "refresh-cron-schedule": "REFRESH_CRON_SCHEDULE",
    "summary-cron-schedule": "SUMMARY_CRON_SCHEDULE",
    "failure-analysis-cron-schedule": "FAILURE_ANALYSIS_CRON_SCHEDULE",
    "max-failures-per-analysis-run": "MAX_FAILURES_PER_ANALYSIS_RUN",
    "llm-model": "LLM_MODEL",
}


class WatchtowerCharm(ops.CharmBase):
    """Watchtower charm - runs the Go bot as a pebble service."""

    def __init__(self, framework: ops.Framework) -> None:
        """Initialise the charm."""
        super().__init__(framework)

        self._container = self.unit.get_container(CONTAINER)

        # Temporal host-info relation.
        self._temporal = TemporalHostInfoRequirer(self)
        framework.observe(
            self._temporal.on.temporal_host_info_changed,
            self._on_temporal_changed,
        )
        framework.observe(
            self._temporal.on.temporal_host_info_unavailable,
            self._on_temporal_unavailable,
        )

        # Standard charm events.
        framework.observe(self.on[CONTAINER].pebble_ready, self._on_pebble_ready)
        framework.observe(self.on.config_changed, self._on_config_changed)
        framework.observe(self.on.upgrade_charm, self._on_upgrade_charm)
        framework.observe(self.on.secret_changed, self._on_secret_changed)

    # ------------------------------------------------------------------
    # Event handlers
    # ------------------------------------------------------------------

    def _on_pebble_ready(self, _: ops.EventBase) -> None:
        """Start the service once pebble is ready."""
        self._replan()

    def _on_config_changed(self, _: ops.EventBase) -> None:
        """Replan on every config change."""
        self._replan()

    def _on_upgrade_charm(self, _: ops.EventBase) -> None:
        """Replan on charm upgrade, cleaning up any stale services."""
        if self._container.can_connect():
            self._remove_stale_services()
        self._replan()

    def _on_secret_changed(self, _: ops.EventBase) -> None:
        """Replan when a Juju secret value is rotated."""
        self._replan()

    def _on_temporal_changed(self, event: TemporalHostInfoChangedEvent) -> None:
        """Handle Temporal relation data becoming available."""
        logger.info("temporal relation updated: %s:%s", event.host, event.port)
        self._replan()

    def _on_temporal_unavailable(self, _: ops.EventBase) -> None:
        """Handle Temporal relation being broken or unavailable."""
        logger.warning("temporal relation lost")
        self._stop_service()
        self.unit.status = ops.BlockedStatus("Waiting for temporal-host-info relation")

    # ------------------------------------------------------------------
    # Core logic
    # ------------------------------------------------------------------

    def _replan(self) -> None:
        """Push pebble layer and reconcile service state."""
        if not self._container.can_connect():
            self.unit.status = ops.WaitingStatus("Waiting for pebble")
            return

        host = self._temporal.host
        port = self._temporal.port
        if not host or not port:
            self._stop_service()
            self.unit.status = ops.BlockedStatus("Waiting for temporal-host-info relation")
            return

        env = self._build_env(host, port)
        layer = ops.pebble.Layer(
            {
                "services": {
                    SERVICE: {
                        "override": "replace",
                        "summary": "watchtower bot",
                        "command": BINARY,
                        "startup": "enabled",
                        "environment": env,
                    }
                },
                "checks": {
                    "ready": {
                        "override": "replace",
                        "period": "30s",
                        "http": {"url": "http://localhost:8080/healthz"},
                    }
                },
            }
        )

        try:
            self._container.add_layer(SERVICE, layer, combine=True)
            self._container.replan()
        except ops.pebble.ChangeError as exc:
            # The service may exit immediately (e.g. Temporal not yet
            # reachable on first start). Log it but do not fail the hook;
            # the service will be restarted by pebble's back-off policy.
            logger.warning("service start issue (will retry): %s", exc)
        except (ConnectionError, ProtocolError, APIError) as exc:
            logger.error("pebble error during replan: %s", exc)
            self.unit.status = ops.BlockedStatus("Pebble error - check juju debug-log")
            return

        self.unit.status = ops.ActiveStatus()

    def _remove_stale_services(self) -> None:
        """Stop services left by a previous charm version.

        The previous go-framework-based charm named its pebble service 'go'.
        After the upgrade we own a service called 'watchtower'. Stopping any
        unknown running service prevents pebble replan from managing stale ones.
        """
        try:
            services = self._container.get_services()
        except (ConnectionError, ProtocolError, APIError) as exc:
            logger.warning("could not list services for cleanup: %s", exc)
            return
        for name, svc in services.items():
            if name != SERVICE and svc.is_running():
                logger.info("stopping stale service %r from previous charm", name)
                try:
                    self._container.stop(name)
                except (ConnectionError, ProtocolError, APIError) as exc:
                    logger.warning("could not stop stale service %r: %s", name, exc)

    def _stop_service(self) -> None:
        """Stop the workload service if pebble is reachable."""
        if not self._container.can_connect():
            return
        try:
            svc = self._container.get_services(SERVICE).get(SERVICE)
            if svc and svc.is_running():
                self._container.stop(SERVICE)
        except (ConnectionError, ProtocolError, APIError) as exc:
            logger.warning("could not stop service: %s", exc)

    def _build_env(self, temporal_host: str, temporal_port: int) -> dict[str, str]:
        """Build the full environment dictionary for the Go binary.

        Args:
            temporal_host: Temporal server hostname from the relation.
            temporal_port: Temporal server port from the relation.

        Returns:
            Dict of env var name to value.
        """
        # Use the stable K8s service DNS name rather than the pod IP
        # provided by the relation. Pod IPs are ephemeral and change on
        # every pod restart or VM reboot; the service DNS name is stable.
        _ = temporal_host  # relation host retained for guard logic only
        env: dict[str, str] = {
            "TEMPORAL_HOST": f"temporal-k8s:{temporal_port}",
        }

        # Plain string/int config options.
        for config_key, env_var in _CONFIG_ENV_VARS.items():
            val = self.config.get(config_key)
            if val is not None and val != "":
                env[env_var] = str(val)

        # Secret config options.
        env.update(self._secrets_env())

        return env

    def _secrets_env(self) -> dict[str, str]:
        """Read Juju secrets from secret-type config options.

        Returns:
            Dict of env var name -> secret value for each configured secret.
        """
        result: dict[str, str] = {}
        for config_key, env_var in _SECRET_CONFIG_OPTIONS.items():
            value = self._get_secret_value_from_config(config_key)
            if value is not None:
                result[env_var] = value
        return result

    def _get_secret_value_from_config(self, config_key: str) -> str | None:
        """Retrieve the 'value' field of a Juju secret via a config option.

        Args:
            config_key: charmcraft.yaml config option of type ``secret``
                        whose value is a Juju secret URI/ID.

        Returns:
            The secret value string, or None if the option is unset or
            the secret cannot be read.
        """
        secret_id = self.config.get(config_key)
        if not secret_id:
            return None
        try:
            secret = self.model.get_secret(id=secret_id)
            return secret.get_content(refresh=True).get("value")
        except ops.SecretNotFoundError:
            logger.warning(
                "secret %r (from config %r) not found",
                secret_id,
                config_key,
            )
            return None


if __name__ == "__main__":  # pragma: nocover
    ops.main(WatchtowerCharm)
