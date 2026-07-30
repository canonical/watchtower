.PHONY: build clean test lint check run-bot up down restart-bot reset clean-state \
        rock charm-pack charm-push charm-refresh juju-status juju-logs

BOT = bin/bot

# OCI image tag pushed to the MicroK8s local registry
ROCK_VERSION ?= 0.3
ROCK_IMAGE   ?= localhost:32000/watchtower:$(ROCK_VERSION)
ROCK_FILE    ?= watchtower_$(ROCK_VERSION)_amd64.rock

# Charm file produced by charmcraft pack
CHARM_FILE ?= $(CURDIR)/charm/watchtower-k8s_amd64.charm

## ── Local build ────────────────────────────────────────────────────────────

build: $(BOT)

$(BOT):
	go build -o $@ ./cmd/bot/

clean:
	rm -rf bin/

## ── Quality gates (must pass before every commit) ──────────────────────────

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

check: lint test

## ── Local dev (Option B — 2 terminals, recommended for active development) ──
##
##   terminal 1:  temporal server start-dev
##   terminal 2:  make run-bot
##
## To restart after a code change: Ctrl-C in terminal 2, then make run-bot again.

run-bot:
	go run ./cmd/bot/

clean-state:
	rm -f state/snapshot.json

## ── Docker Compose (Option A — demo / staging) ─────────────────────────────

up:
	docker compose up --build -d

down:
	docker compose down

## Rebuild + restart only the bot container (Temporal/Postgres keep running).
## Use this after a code change when running Option A.
restart-bot:
	docker compose up --build -d bot

## Full reset: stop everything, wipe all volumes (Temporal state + snapshot),
## then bring the whole stack back up from scratch.
reset:
	docker compose down -v
	docker compose up --build -d

## ── Charm packaging (Juju / K8s deploy) ────────────────────────────────────
##
## Prerequisites (one-time):
##   sudo snap install rockcraft --channel latest/edge --classic
##   sudo snap install charmcraft --channel latest/edge --classic
##
## Typical inner loop after a code change:
##   make rock          # rebuild OCI image and push to MicroK8s registry
##   make charm-pack    # rebuild charm
##   make charm-refresh # push new image + refresh running charm

## Build OCI rock and push it to the MicroK8s local registry.
rock:
	ROCKCRAFT_ENABLE_EXPERIMENTAL_EXTENSIONS=true rockcraft pack
	rockcraft.skopeo copy \
		--insecure-policy \
		--dest-tls-verify=false \
		oci-archive:$(ROCK_FILE) \
		docker://$(ROCK_IMAGE)

## Pack the charm (clean first to avoid stale build cache).
charm-pack:
	cd charm && CHARMCRAFT_ENABLE_EXPERIMENTAL_EXTENSIONS=true charmcraft clean
	cd charm && CHARMCRAFT_ENABLE_EXPERIMENTAL_EXTENSIONS=true charmcraft pack

## Push a new image and refresh the running watchtower-k8s charm in Juju.
## Resolves any hook error before and after the refresh.
charm-refresh: rock charm-pack
	juju resolve watchtower-k8s/0 2>/dev/null || true
	juju refresh watchtower-k8s \
		--path=$(CHARM_FILE) \
		--resource app-image=$(ROCK_IMAGE)
	sleep 5
	juju resolve watchtower-k8s/0 2>/dev/null || true

## ── Juju observability helpers ──────────────────────────────────────────────

juju-status:
	juju status --relations

juju-logs:
	juju debug-log --tail
