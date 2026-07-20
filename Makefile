VERSION ?= v20260717.1557
DISTBASE ?= dist
DISTARCH ?= $(if $(GOARCH),$(GOARCH),$(shell go env GOARCH))
DISTPLATFORM ?= $(ROUTERD_OS)-$(DISTARCH)
DISTDIR ?= $(DISTBASE)/$(DISTPLATFORM)
DISTROOT ?= $(DISTDIR)/package
DISTTAR ?= $(DISTDIR)/routerd-$(VERSION)-$(DISTPLATFORM).tar.gz
DISTTAR_ALIAS ?= $(DISTDIR)/routerd-$(DISTPLATFORM).tar.gz
CONFIG ?=
UNAME_S := $(shell uname -s)

ifeq ($(UNAME_S),FreeBSD)
ROUTERD_OS ?= freebsd
else
ROUTERD_OS ?= linux
endif

BUILDDIR ?= bin/$(ROUTERD_OS)$(if $(GOARCH),-$(GOARCH))
ROUTERD_BIN := $(BUILDDIR)/routerd
ROUTERCTL_BIN := $(BUILDDIR)/routerctl
ROUTERD_DHCPv4_CLIENT_BIN := $(BUILDDIR)/routerd-dhcpv4-client
ROUTERD_DHCPv6_CLIENT_BIN := $(BUILDDIR)/routerd-dhcpv6-client
ROUTERD_DHCP_EVENT_RELAY_BIN := $(BUILDDIR)/routerd-dhcp-event-relay
ROUTERD_DHCP_FINGERPRINT_WATCHER_BIN := $(BUILDDIR)/routerd-dhcp-fingerprint-watcher
ROUTERD_HEALTHCHECK_BIN := $(BUILDDIR)/routerd-healthcheck
ROUTERD_BGP_BIN := $(BUILDDIR)/routerd-bgp
ROUTERD_DNS_RESOLVER_BIN := $(BUILDDIR)/routerd-dns-resolver
ROUTERD_FIREWALL_LOGGER_BIN := $(BUILDDIR)/routerd-firewall-logger
ROUTERD_DPI_CLASSIFIER_BIN := $(BUILDDIR)/routerd-dpi-classifier
ROUTERD_NDPI_AGENT_BIN := $(BUILDDIR)/routerd-ndpi-agent
ROUTERD_RA_OBSERVER_BIN := $(BUILDDIR)/routerd-ra-observer
ROUTERD_ARP_OBSERVER_BIN := $(BUILDDIR)/routerd-arp-observer
ROUTERD_PPPOE_CLIENT_BIN := $(BUILDDIR)/routerd-pppoe-client
ROUTERD_EVENTD_BIN := $(BUILDDIR)/routerd-eventd
AWS_PROVIDER_EXECUTOR_BIN := $(BUILDDIR)/aws-provider-executor
AZURE_PROVIDER_EXECUTOR_BIN := $(BUILDDIR)/azure-provider-executor
OCI_PROVIDER_EXECUTOR_BIN := $(BUILDDIR)/oci-provider-executor
ROUTERD_PROVIDER_EXECUTOR_BINS := $(AWS_PROVIDER_EXECUTOR_BIN) $(AZURE_PROVIDER_EXECUTOR_BIN) $(OCI_PROVIDER_EXECUTOR_BIN)
ROUTERD_RELEASE_BINS := $(ROUTERD_BIN) $(ROUTERCTL_BIN) $(ROUTERD_DHCPv4_CLIENT_BIN) $(ROUTERD_DHCPv6_CLIENT_BIN) $(ROUTERD_DHCP_EVENT_RELAY_BIN) $(ROUTERD_DHCP_FINGERPRINT_WATCHER_BIN) $(ROUTERD_HEALTHCHECK_BIN) $(ROUTERD_BGP_BIN) $(ROUTERD_DNS_RESOLVER_BIN) $(ROUTERD_FIREWALL_LOGGER_BIN) $(ROUTERD_DPI_CLASSIFIER_BIN) $(ROUTERD_NDPI_AGENT_BIN) $(ROUTERD_RA_OBSERVER_BIN) $(ROUTERD_ARP_OBSERVER_BIN) $(ROUTERD_PPPOE_CLIENT_BIN) $(ROUTERD_EVENTD_BIN) $(ROUTERD_PROVIDER_EXECUTOR_BINS)
ROUTERD_NDPI_AGENT_LIBNDPI_DISTROOT := $(DISTDIR)/ndpi-agent-libndpi-package
ROUTERD_NDPI_AGENT_LIBNDPI_TAR := $(DISTDIR)/routerd-ndpi-agent-libndpi-$(VERSION)-$(DISTPLATFORM).tar.gz
ROUTERD_NDPI_AGENT_LIBNDPI_ALIAS := $(DISTDIR)/routerd-ndpi-agent-libndpi-$(DISTPLATFORM).tar.gz
GO_BUILD_ENV := CGO_ENABLED=0 GOOS=$(ROUTERD_OS)
ifneq ($(GOARCH),)
GO_BUILD_ENV += GOARCH=$(GOARCH)
endif
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || true)
GO_LDFLAGS ?= -s -w -X github.com/imksoo/routerd/pkg/version.Version=$(VERSION) $(if $(GIT_COMMIT),-X github.com/imksoo/routerd/pkg/version.Commit=$(GIT_COMMIT))
GO_BUILD_FLAGS ?= -buildvcs=false -trimpath -ldflags="$(GO_LDFLAGS)"
EXAMPLE_CONFIGS ?= $(wildcard examples/*.yaml)
PLAYWRIGHT_INSTALL_FLAGS ?= --with-deps
WEBSITE_SCHEMA_DIR := website/static/schemas
CONFIG_SCHEMA := routerd-config-v1alpha1.schema.json
CONTROL_SCHEMA := routerd-control-v1alpha1.schema.json
CONTROL_OPENAPI_SCHEMA := routerd-control-openapi-v1alpha1.json
WIZARD_FIXTURE_DIR := website/fixtures/wizard

WEBSITE_NODE_MODULES_STAMP := website/node_modules/.package-lock.json

.PHONY: test check-version-ldflags check-tmp-dir-mutations check-tar-safe-paths-test build build-daemons build-provider-executors build-ndpi-agent build-ndpi-agent-libndpi build-daemons-freebsd check-freebsd-cross-compile check-linux-static check-ndpi-agent-libndpi check-install-deps cloudedge-acceptance-lint cloudedge-acceptance-offline-test cloudedge-runners-offline-test cloudedge-poc-evidence-offline-test cloudedge-e2e-preflight-offline-test webconsole-build webconsole-browser-install webconsole-screenshot generate-schema sync-website-schemas check-schema check-website-schemas generate-wizard-fixtures check-wizard-fixtures validate-wizard-fixtures check-examples-line-limits check-render-golden update-render-golden check-bespoke-lifecycle website-deps website-build third-party-licenses check-build-deps dist dist-ndpi-agent-libndpi live-iso validate-example dry-run-example plan-config release clean

test: check-version-ldflags check-tmp-dir-mutations check-tar-safe-paths-test
	go test ./...

check-version-ldflags:
	scripts/check-version-ldflags.sh

check-tmp-dir-mutations:
	scripts/check-tmp-dir-mutations.sh

check-tar-safe-paths-test:
	scripts/check-tar-safe-paths-test.sh

build: webconsole-build
	$(MAKE) build-daemons

build-daemons:
	install -d $(BUILDDIR)
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_BIN) ./cmd/routerd
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERCTL_BIN) ./cmd/routerctl
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_DHCPv4_CLIENT_BIN) ./cmd/routerd-dhcpv4-client
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_DHCPv6_CLIENT_BIN) ./cmd/routerd-dhcpv6-client
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_DHCP_EVENT_RELAY_BIN) ./cmd/routerd-dhcp-event-relay
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_DHCP_FINGERPRINT_WATCHER_BIN) ./cmd/routerd-dhcp-fingerprint-watcher
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_HEALTHCHECK_BIN) ./cmd/routerd-healthcheck
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_BGP_BIN) ./cmd/routerd-bgp
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_DNS_RESOLVER_BIN) ./cmd/routerd-dns-resolver
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_FIREWALL_LOGGER_BIN) ./cmd/routerd-firewall-logger
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_DPI_CLASSIFIER_BIN) ./cmd/routerd-dpi-classifier
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_NDPI_AGENT_BIN) ./cmd/routerd-ndpi-agent
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_RA_OBSERVER_BIN) ./cmd/routerd-ra-observer
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_ARP_OBSERVER_BIN) ./cmd/routerd-arp-observer
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_PPPOE_CLIENT_BIN) ./cmd/routerd-pppoe-client
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_EVENTD_BIN) ./cmd/routerd-eventd

build-provider-executors:
	install -d $(BUILDDIR)
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(AWS_PROVIDER_EXECUTOR_BIN) ./examples/plugins/aws-provider-executor
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(AZURE_PROVIDER_EXECUTOR_BIN) ./examples/plugins/azure-provider-executor
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(OCI_PROVIDER_EXECUTOR_BIN) ./examples/plugins/oci-provider-executor

build-ndpi-agent:
	install -d $(BUILDDIR)
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_NDPI_AGENT_BIN) ./cmd/routerd-ndpi-agent

build-ndpi-agent-libndpi:
	install -d $(BUILDDIR)
	CGO_ENABLED=1 GOOS=$(ROUTERD_OS) $(if $(GOARCH),GOARCH=$(GOARCH),) go build $(GO_BUILD_FLAGS) -tags libndpi -o $(ROUTERD_NDPI_AGENT_BIN) ./cmd/routerd-ndpi-agent

build-daemons-freebsd:
	$(MAKE) build-daemons ROUTERD_OS=freebsd GOARCH=amd64

# Cross-compilation cannot execute FreeBSD test binaries on a Linux runner.
# -exec /bin/true compiles FreeBSD test binaries; native test execution is
# covered by the VM smoke job.
check-freebsd-cross-compile:
	$(MAKE) build-daemons ROUTERD_OS=freebsd GOARCH=amd64
	$(MAKE) build-provider-executors ROUTERD_OS=freebsd GOARCH=amd64
	CGO_ENABLED=0 GOOS=freebsd GOARCH=amd64 go test -exec /bin/true ./...

check-linux-static:
	@if [ "$(ROUTERD_OS)" != "linux" ]; then exit 0; fi; \
	missing=0; \
	for bin in $(ROUTERD_RELEASE_BINS); do \
		if [ ! -x "$$bin" ]; then echo "missing binary: $$bin" >&2; missing=1; fi; \
	done; \
	[ "$$missing" -eq 0 ] || exit 1; \
	if ! command -v file >/dev/null 2>&1; then echo "missing file(1), cannot verify static Linux binaries" >&2; exit 1; fi; \
	for bin in $(ROUTERD_RELEASE_BINS); do \
		info=$$(file "$$bin"); \
		case "$$info" in \
			*"statically linked"*) ;; \
			*) echo "Linux binary is not statically linked: $$bin" >&2; echo "$$info" >&2; exit 1 ;; \
		esac; \
	done

check-ndpi-agent-libndpi:
	@if [ "$(ROUTERD_OS)" != "linux" ]; then echo "libndpi agent archive is Linux-only" >&2; exit 2; fi
	@if [ ! -x "$(ROUTERD_NDPI_AGENT_BIN)" ]; then echo "missing binary: $(ROUTERD_NDPI_AGENT_BIN)" >&2; exit 1; fi
	@if ! command -v file >/dev/null 2>&1; then echo "missing file(1), cannot verify libndpi agent binary" >&2; exit 1; fi
	@info=$$(file "$(ROUTERD_NDPI_AGENT_BIN)"); \
	case "$$info" in \
		*"dynamically linked"*|*"shared object"*) ;; \
		*) echo "libndpi agent binary is expected to be dynamically linked: $(ROUTERD_NDPI_AGENT_BIN)" >&2; echo "$$info" >&2; exit 1 ;; \
	esac
	@if [ "$(DISTARCH)" = "$$(go env GOARCH)" ]; then \
		if ! "$(ROUTERD_NDPI_AGENT_BIN)" selftest | grep -q '"libndpiLoaded":true'; then \
			echo "libndpi agent selftest did not load libndpi" >&2; \
			exit 1; \
		fi; \
	else \
		echo "skipping libndpi agent selftest for non-native arch $(DISTARCH)" >&2; \
	fi

check-install-deps:
	./scripts/install-deps-smoke.sh

check-bootstrap-cleanup:
	./scripts/bootstrap-cleanup-smoke.sh

cloudedge-acceptance-lint:
	./scripts/cloudedge-acceptance.sh lint

cloudedge-acceptance-offline-test:
	./scripts/cloudedge-acceptance-offline-test.sh

cloudedge-runners-offline-test:
	./scripts/runners/cloudedge-runners-offline-test.sh

cloudedge-poc-evidence-offline-test:
	./scripts/cloudedge-poc-evidence-offline-test.sh

cloudedge-e2e-preflight-offline-test:
	./tests/e2e/cloudedge/scripts/sam-preflight-offline-test.sh

webconsole-build:
	cd webconsole && npm ci && npm run build

webconsole-browser-install:
	cd webconsole && npx playwright install $(PLAYWRIGHT_INSTALL_FLAGS) chromium

webconsole-screenshot: webconsole-build webconsole-browser-install
	cd webconsole && npm run screenshot

generate-schema:
	install -d schemas
	go run ./cmd/routerd-schema > schemas/$(CONFIG_SCHEMA)
	go run ./cmd/routerd-schema --schema control > schemas/$(CONTROL_SCHEMA)
	go run ./cmd/routerd-schema --schema control-openapi > schemas/$(CONTROL_OPENAPI_SCHEMA)

sync-website-schemas: generate-schema
	install -d $(WEBSITE_SCHEMA_DIR)
	cp schemas/$(CONFIG_SCHEMA) $(WEBSITE_SCHEMA_DIR)/$(CONFIG_SCHEMA)
	cp schemas/$(CONTROL_SCHEMA) $(WEBSITE_SCHEMA_DIR)/$(CONTROL_SCHEMA)
	cp schemas/$(CONTROL_OPENAPI_SCHEMA) $(WEBSITE_SCHEMA_DIR)/$(CONTROL_OPENAPI_SCHEMA)

check-schema:
	go run ./cmd/routerd-schema > /tmp/$(CONFIG_SCHEMA)
	diff -u schemas/$(CONFIG_SCHEMA) /tmp/$(CONFIG_SCHEMA)
	go run ./cmd/routerd-schema --schema control > /tmp/$(CONTROL_SCHEMA)
	diff -u schemas/$(CONTROL_SCHEMA) /tmp/$(CONTROL_SCHEMA)
	go run ./cmd/routerd-schema --schema control-openapi > /tmp/$(CONTROL_OPENAPI_SCHEMA)
	diff -u schemas/$(CONTROL_OPENAPI_SCHEMA) /tmp/$(CONTROL_OPENAPI_SCHEMA)

check-website-schemas:
	cmp schemas/$(CONFIG_SCHEMA) $(WEBSITE_SCHEMA_DIR)/$(CONFIG_SCHEMA)
	cmp schemas/$(CONTROL_SCHEMA) $(WEBSITE_SCHEMA_DIR)/$(CONTROL_SCHEMA)
	cmp schemas/$(CONTROL_OPENAPI_SCHEMA) $(WEBSITE_SCHEMA_DIR)/$(CONTROL_OPENAPI_SCHEMA)
	jq -e '."$$id" == "https://routerd.net/schemas/$(CONFIG_SCHEMA)"' $(WEBSITE_SCHEMA_DIR)/$(CONFIG_SCHEMA)
	jq -e '."$$id" == "https://routerd.net/schemas/$(CONTROL_SCHEMA)"' $(WEBSITE_SCHEMA_DIR)/$(CONTROL_SCHEMA)
	jq -e '."$$id" == "https://routerd.net/schemas/$(CONTROL_OPENAPI_SCHEMA)"' $(WEBSITE_SCHEMA_DIR)/$(CONTROL_OPENAPI_SCHEMA)

generate-wizard-fixtures: website-deps
	rm -rf /tmp/routerd-wizard-builder
	website/node_modules/.bin/tsc --target ES2020 --module commonjs --moduleResolution node --esModuleInterop --skipLibCheck --outDir /tmp/routerd-wizard-builder website/src/lib/routerdWizard.ts
	node scripts/generate-wizard-fixtures.cjs /tmp/routerd-wizard-builder/routerdWizard.js $(WIZARD_FIXTURE_DIR)

check-wizard-fixtures: website-deps
	rm -rf /tmp/routerd-wizard-builder /tmp/routerd-wizard-fixtures
	website/node_modules/.bin/tsc --target ES2020 --module commonjs --moduleResolution node --esModuleInterop --skipLibCheck --outDir /tmp/routerd-wizard-builder website/src/lib/routerdWizard.ts
	node scripts/generate-wizard-fixtures.cjs /tmp/routerd-wizard-builder/routerdWizard.js /tmp/routerd-wizard-fixtures
	diff -ru $(WIZARD_FIXTURE_DIR) /tmp/routerd-wizard-fixtures

validate-wizard-fixtures: website-deps
	node scripts/validate-wizard-fixtures.cjs schemas/$(CONFIG_SCHEMA) $(WIZARD_FIXTURE_DIR)
	@configs=$$(find $(WIZARD_FIXTURE_DIR) -type f -name '*.yaml' -print | sort); \
	scripts/routerd-sandbox-run.sh sh -c 'for config do \
		echo "validating $$config"; \
		go run ./cmd/routerctl validate --socket "$$ROUTERD_SANDBOX_STATUS_SOCKET" -f "$$config" --replace >/dev/null || exit 1; \
	done' sh $$configs

check-examples-line-limits:
	./scripts/check-examples-line-limits.sh

check-render-golden:
	go test ./tests/golden

update-render-golden:
	ROUTERD_UPDATE_GOLDEN=1 go test ./tests/golden

check-bespoke-lifecycle:
	go test ./pkg/servicemgr ./pkg/controller/bgp ./pkg/controller/vrrp ./pkg/controller/chain ./pkg/controller/nat44 ./cmd/routerctl

website-deps: $(WEBSITE_NODE_MODULES_STAMP)

$(WEBSITE_NODE_MODULES_STAMP): website/package.json website/package-lock.json
	cd website && npm ci --prefer-offline --no-audit

website-build: website-deps
	cd website && npm run build

third-party-licenses:
	./scripts/collect-third-party-licenses.sh THIRD_PARTY_LICENSES.md

check-build-deps:
	@missing=0; \
	for cmd in go install tar find cp; do \
		if ! command -v $$cmd >/dev/null 2>&1; then echo "missing build dependency: $$cmd" >&2; missing=1; fi; \
	done; \
	exit $$missing

dist:
	rm -rf $(DISTROOT) $(DISTTAR) $(DISTTAR).sha256 $(DISTTAR_ALIAS) $(DISTTAR_ALIAS).sha256
	$(MAKE) build-daemons
	$(MAKE) build-provider-executors
	$(MAKE) check-linux-static
	install -d $(DISTROOT)/bin
	install -m 0755 $(ROUTERD_BIN) $(DISTROOT)/bin/routerd
	install -m 0755 $(ROUTERCTL_BIN) $(DISTROOT)/bin/routerctl
	install -m 0755 $(ROUTERD_DHCPv4_CLIENT_BIN) $(DISTROOT)/bin/routerd-dhcpv4-client
	install -m 0755 $(ROUTERD_DHCPv6_CLIENT_BIN) $(DISTROOT)/bin/routerd-dhcpv6-client
	install -m 0755 $(ROUTERD_DHCP_EVENT_RELAY_BIN) $(DISTROOT)/bin/routerd-dhcp-event-relay
	install -m 0755 $(ROUTERD_DHCP_FINGERPRINT_WATCHER_BIN) $(DISTROOT)/bin/routerd-dhcp-fingerprint-watcher
	install -m 0755 $(ROUTERD_HEALTHCHECK_BIN) $(DISTROOT)/bin/routerd-healthcheck
	install -m 0755 $(ROUTERD_BGP_BIN) $(DISTROOT)/bin/routerd-bgp
	install -m 0755 $(ROUTERD_DNS_RESOLVER_BIN) $(DISTROOT)/bin/routerd-dns-resolver
	install -m 0755 $(ROUTERD_FIREWALL_LOGGER_BIN) $(DISTROOT)/bin/routerd-firewall-logger
	install -m 0755 $(ROUTERD_DPI_CLASSIFIER_BIN) $(DISTROOT)/bin/routerd-dpi-classifier
	install -m 0755 $(ROUTERD_NDPI_AGENT_BIN) $(DISTROOT)/bin/routerd-ndpi-agent
	install -m 0755 $(ROUTERD_RA_OBSERVER_BIN) $(DISTROOT)/bin/routerd-ra-observer
	install -m 0755 $(ROUTERD_ARP_OBSERVER_BIN) $(DISTROOT)/bin/routerd-arp-observer
	install -m 0755 $(ROUTERD_PPPOE_CLIENT_BIN) $(DISTROOT)/bin/routerd-pppoe-client
	install -m 0755 $(ROUTERD_EVENTD_BIN) $(DISTROOT)/bin/routerd-eventd
	install -d $(DISTROOT)/libexec/routerd/plugins/aws-provider-executor/bin
	install -m 0755 $(AWS_PROVIDER_EXECUTOR_BIN) $(DISTROOT)/libexec/routerd/plugins/aws-provider-executor/bin/aws-provider-executor
	install -m 0644 examples/plugins/aws-provider-executor/plugin.yaml $(DISTROOT)/libexec/routerd/plugins/aws-provider-executor/plugin.yaml
	install -d $(DISTROOT)/libexec/routerd/plugins/azure-provider-executor/bin
	install -m 0755 $(AZURE_PROVIDER_EXECUTOR_BIN) $(DISTROOT)/libexec/routerd/plugins/azure-provider-executor/bin/azure-provider-executor
	install -m 0644 examples/plugins/azure-provider-executor/plugin.yaml $(DISTROOT)/libexec/routerd/plugins/azure-provider-executor/plugin.yaml
	install -d $(DISTROOT)/libexec/routerd/plugins/oci-provider-executor/bin
	install -m 0755 $(OCI_PROVIDER_EXECUTOR_BIN) $(DISTROOT)/libexec/routerd/plugins/oci-provider-executor/bin/oci-provider-executor
	install -m 0644 examples/plugins/oci-provider-executor/plugin.yaml $(DISTROOT)/libexec/routerd/plugins/oci-provider-executor/plugin.yaml
	install -m 0755 examples/cloudedge-mobility-demo/plugins/provider-private-ip-inventory $(DISTROOT)/libexec/routerd/plugins/provider-private-ip-inventory
	install -m 0755 packaging/install.sh $(DISTROOT)/install.sh
	install -m 0755 packaging/uninstall.sh $(DISTROOT)/uninstall.sh
	install -d $(DISTROOT)/etc/routerd
	install -m 0644 examples/router-lab.yaml $(DISTROOT)/etc/routerd/router.yaml.sample
	install -d $(DISTROOT)/share/doc
	install -m 0644 README.md $(DISTROOT)/share/doc/README.md
	if [ -f README.ja.md ]; then install -m 0644 README.ja.md $(DISTROOT)/share/doc/README.ja.md; fi
	if [ -f LICENSE ]; then install -m 0644 LICENSE $(DISTROOT)/share/doc/LICENSE; elif [ -f LICENSE.md ]; then install -m 0644 LICENSE.md $(DISTROOT)/share/doc/LICENSE; else printf '%s\n' 'No LICENSE file is present in this repository.' > $(DISTROOT)/share/doc/LICENSE; fi
	if [ -f THIRD_PARTY_LICENSES.md ]; then install -m 0644 THIRD_PARTY_LICENSES.md $(DISTROOT)/share/doc/THIRD_PARTY_LICENSES.md; fi
	printf '%s\n' '$(VERSION)' > $(DISTROOT)/share/doc/VERSION
	printf '%s\n' '$(ROUTERD_OS)-$(DISTARCH)' > $(DISTROOT)/share/doc/TARGET
	if [ "$(ROUTERD_OS)" = "freebsd" ]; then \
		install -d $(DISTROOT)/rc.d; \
		install -m 0555 contrib/freebsd/routerd $(DISTROOT)/rc.d/routerd; \
	else \
		install -d $(DISTROOT)/systemd; \
		install -m 0644 contrib/systemd/routerd.service contrib/systemd/routerd-bgp.service $(DISTROOT)/systemd/; \
	fi
	install -d $(DISTDIR)
	tar -C $(DISTROOT) -czf $(DISTTAR) .
	scripts/check-tar-safe-paths.sh $(DISTTAR)
	cp $(DISTTAR) $(DISTTAR_ALIAS)
	if command -v sha256sum >/dev/null 2>&1; then (cd $(DISTDIR) && sha256sum $(notdir $(DISTTAR)) > $(notdir $(DISTTAR)).sha256); elif command -v shasum >/dev/null 2>&1; then (cd $(DISTDIR) && shasum -a 256 $(notdir $(DISTTAR)) > $(notdir $(DISTTAR)).sha256); elif command -v sha256 >/dev/null 2>&1; then (cd $(DISTDIR) && sha256 -r $(notdir $(DISTTAR)) > $(notdir $(DISTTAR)).sha256); else echo "missing sha256 tool" >&2; exit 1; fi
	if command -v sha256sum >/dev/null 2>&1; then (cd $(DISTDIR) && sha256sum $(notdir $(DISTTAR_ALIAS)) > $(notdir $(DISTTAR_ALIAS)).sha256); elif command -v shasum >/dev/null 2>&1; then (cd $(DISTDIR) && shasum -a 256 $(notdir $(DISTTAR_ALIAS)) > $(notdir $(DISTTAR_ALIAS)).sha256); elif command -v sha256 >/dev/null 2>&1; then (cd $(DISTDIR) && sha256 -r $(notdir $(DISTTAR_ALIAS)) > $(notdir $(DISTTAR_ALIAS)).sha256); else echo "missing sha256 tool" >&2; exit 1; fi

dist-ndpi-agent-libndpi:
	@if [ "$(ROUTERD_OS)" != "linux" ]; then echo "libndpi agent archive is Linux-only" >&2; exit 2; fi
	rm -rf $(ROUTERD_NDPI_AGENT_LIBNDPI_DISTROOT) $(ROUTERD_NDPI_AGENT_LIBNDPI_TAR) $(ROUTERD_NDPI_AGENT_LIBNDPI_TAR).sha256 $(ROUTERD_NDPI_AGENT_LIBNDPI_ALIAS) $(ROUTERD_NDPI_AGENT_LIBNDPI_ALIAS).sha256
	$(MAKE) build-ndpi-agent-libndpi
	$(MAKE) check-ndpi-agent-libndpi
	install -d $(ROUTERD_NDPI_AGENT_LIBNDPI_DISTROOT)/bin
	install -m 0755 $(ROUTERD_NDPI_AGENT_BIN) $(ROUTERD_NDPI_AGENT_LIBNDPI_DISTROOT)/bin/routerd-ndpi-agent
	install -d $(ROUTERD_NDPI_AGENT_LIBNDPI_DISTROOT)/share/doc
	install -m 0644 docs/operations/ndpi-agent-libndpi.md $(ROUTERD_NDPI_AGENT_LIBNDPI_DISTROOT)/share/doc/README.md
	printf '%s\n' '$(VERSION)' > $(ROUTERD_NDPI_AGENT_LIBNDPI_DISTROOT)/share/doc/VERSION
	printf '%s\n' '$(DISTPLATFORM)' > $(ROUTERD_NDPI_AGENT_LIBNDPI_DISTROOT)/share/doc/TARGET
	install -d $(DISTDIR)
	tar -C $(ROUTERD_NDPI_AGENT_LIBNDPI_DISTROOT) -czf $(ROUTERD_NDPI_AGENT_LIBNDPI_TAR) .
	scripts/check-tar-safe-paths.sh $(ROUTERD_NDPI_AGENT_LIBNDPI_TAR)
	cp $(ROUTERD_NDPI_AGENT_LIBNDPI_TAR) $(ROUTERD_NDPI_AGENT_LIBNDPI_ALIAS)
	if command -v sha256sum >/dev/null 2>&1; then (cd $(DISTDIR) && sha256sum $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_TAR)) > $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_TAR)).sha256); elif command -v shasum >/dev/null 2>&1; then (cd $(DISTDIR) && shasum -a 256 $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_TAR)) > $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_TAR)).sha256); elif command -v sha256 >/dev/null 2>&1; then (cd $(DISTDIR) && sha256 -r $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_TAR)) > $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_TAR)).sha256); else echo "missing sha256 tool" >&2; exit 1; fi
	if command -v sha256sum >/dev/null 2>&1; then (cd $(DISTDIR) && sha256sum $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_ALIAS)) > $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_ALIAS)).sha256); elif command -v shasum >/dev/null 2>&1; then (cd $(DISTDIR) && shasum -a 256 $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_ALIAS)) > $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_ALIAS)).sha256); elif command -v sha256 >/dev/null 2>&1; then (cd $(DISTDIR) && sha256 -r $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_ALIAS)) > $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_ALIAS)).sha256); else echo "missing sha256 tool" >&2; exit 1; fi

live-iso:
	VERSION=$(VERSION) DISTBASE=$(DISTBASE) scripts/build-live-iso.sh

validate-example:
	@scripts/routerd-sandbox-run.sh sh -c 'for config do \
		echo "validating $$config"; \
		go run ./cmd/routerctl validate --socket "$$ROUTERD_SANDBOX_STATUS_SOCKET" -f "$$config" --replace >/dev/null || exit 1; \
	done' sh $(EXAMPLE_CONFIGS)

dry-run-example:
	scripts/routerd-sandbox-run.sh sh -c 'go run ./cmd/routerctl apply --socket "$$ROUTERD_SANDBOX_SOCKET" -f examples/basic-static.yaml --replace > /tmp/routerd-status.json'

plan-config:
	test -n "$(CONFIG)" || (echo "CONFIG is required, for example: make plan-config CONFIG=path/to/router.yaml" >&2; exit 2)
	scripts/routerd-sandbox-run.sh sh -c 'go run ./cmd/routerctl plan --socket "$$ROUTERD_SANDBOX_STATUS_SOCKET" -f "$$1" --replace > /tmp/routerd-plan-status.json' sh "$(CONFIG)"

release:
	scripts/release.sh

clean:
	rm -rf bin dist
