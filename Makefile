.PHONY: build up down logs ps clean db-reset dump help

# ── Default target ──────────────────────────────────────────────
.DEFAULT_GOAL := help

# ── Docker Compose ──────────────────────────────────────────────
COMPOSE := docker compose
PROFILE_BACKUP := --profile backup

# ── Build ───────────────────────────────────────────────────────
build: ## Build all images (Caddy + Next.js + Backup)
	$(COMPOSE) build
	$(COMPOSE) $(PROFILE_BACKUP) build

# ── Start / Stop ─────────────────────────────────────────────────
up: ## Start the stack (no backup sidecar)
	$(COMPOSE) up -d

up-all: ## Start with backup sidecar
	$(COMPOSE) $(PROFILE_BACKUP) up -d

down: ## Stop all containers
	$(COMPOSE) $(PROFILE_BACKUP) down

restart: down up ## Restart the stack

# ── Logs ─────────────────────────────────────────────────────────
logs: ## Tail all container logs
	$(COMPOSE) logs -f --tail=100

logs-caddy: ## Tail Caddy logs
	$(COMPOSE) logs -f caddy

logs-app: ## Tail Next.js logs
	$(COMPOSE) logs -f nextjs_app

logs-db: ## Tail Postgres logs
	$(COMPOSE) logs -f portcullis_db

logs-backup: ## Tail backup logs
	$(COMPOSE) $(PROFILE_BACKUP) logs -f backup

# ── Status ───────────────────────────────────────────────────────
ps: ## Show container status and health
	$(COMPOSE) $(PROFILE_BACKUP) ps
	@echo ""
	@echo "Disk usage:"
	@du -sh data/ 2>/dev/null || echo "(no data/)"

# ── Database ─────────────────────────────────────────────────────
db-reset: ## Reset database (⚠️ destroys all data)
	@echo "⚠️  This will destroy ALL databases (manager + service DBs)."
	@read -p "Type 'yes' to confirm: " answer; \
	if [ "$$answer" != "yes" ]; then echo "Aborted."; exit 1; fi
	$(COMPOSE) down
	rm -rf data/pg_data
	$(COMPOSE) up -d

# ── Dump ─────────────────────────────────────────────────────────
dump: ## On-demand dump of a service (usage: make dump SERVICE=cuid123)
	@if [ -z "$(SERVICE)" ]; then \
		echo "Usage: make dump SERVICE=<service-id>"; \
		echo "  e.g.: make dump SERVICE=proj_abc123"; \
		exit 1; \
	fi
	@PASSCODE=$$(grep PORTCULLIS_PASSCODE .env 2>/dev/null | cut -d= -f2); \
	if [ -z "$$PASSCODE" ]; then \
		echo "❌ PORTCULLIS_PASSCODE not found in .env"; \
		exit 1; \
	fi; \
	DOMAIN=$$(grep PORTCULLIS_DOMAIN .env 2>/dev/null | cut -d= -f2); \
	DATE=$$(date +%Y-%m-%d); \
	curl -sS -H "Authorization: Bearer $$PASSCODE" \
		"http://localhost:3000/api/services/$(SERVICE)/dump" \
		-o "$(SERVICE)-$$DATE.dump" && \
		echo "✅ Dump saved to $(SERVICE)-$$DATE.dump" || \
		echo "❌ Dump failed"

# ── Cleanup ──────────────────────────────────────────────────────
clean: ## Remove all containers, images, volumes, and data
	@echo "⚠️  This will destroy everything: containers, images, volumes, data/."
	@read -p "Type 'yes' to confirm: " answer; \
	if [ "$$answer" != "yes" ]; then echo "Aborted."; exit 1; fi
	$(COMPOSE) $(PROFILE_BACKUP) down -v --rmi all
	rm -rf data/
	@echo "✅ Clean."

# ── Help ─────────────────────────────────────────────────────────
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}'
