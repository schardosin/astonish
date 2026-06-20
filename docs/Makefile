# SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Astonish contributors
#
# SPDX-License-Identifier: Apache-2.0

.PHONY: install
install: ## Install npm dependencies
	npm ci

.PHONY: dev
dev: ## Start development server with hot reload
	npx vitepress dev

.PHONY: build
build: ## Build the documentation site
	npx vitepress build

.PHONY: preview
preview: ## Preview the built site locally
	npx vitepress preview
