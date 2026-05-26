VERSION ?= v20260526.2152
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
ROUTERD_PPPOE_CLIENT_BIN := $(BUILDDIR)/routerd-pppoe-client
ROUTERD_RELEASE_BINS := $(ROUTERD_BIN) $(ROUTERCTL_BIN) $(ROUTERD_DHCPv4_CLIENT_BIN) $(ROUTERD_DHCPv6_CLIENT_BIN) $(ROUTERD_DHCP_EVENT_RELAY_BIN) $(ROUTERD_DHCP_FINGERPRINT_WATCHER_BIN) $(ROUTERD_HEALTHCHECK_BIN) $(ROUTERD_BGP_BIN) $(ROUTERD_DNS_RESOLVER_BIN) $(ROUTERD_FIREWALL_LOGGER_BIN) $(ROUTERD_DPI_CLASSIFIER_BIN) $(ROUTERD_NDPI_AGENT_BIN) $(ROUTERD_RA_OBSERVER_BIN) $(ROUTERD_PPPOE_CLIENT_BIN)
ROUTERD_NDPI_AGENT_LIBNDPI_DISTROOT := $(DISTDIR)/ndpi-agent-libndpi-package
ROUTERD_NDPI_AGENT_LIBNDPI_TAR := $(DISTDIR)/routerd-ndpi-agent-libndpi-$(VERSION)-$(DISTPLATFORM).tar.gz
ROUTERD_NDPI_AGENT_LIBNDPI_ALIAS := $(DISTDIR)/routerd-ndpi-agent-libndpi-$(DISTPLATFORM).tar.gz
GO_BUILD_ENV := CGO_ENABLED=0 GOOS=$(ROUTERD_OS)
ifneq ($(GOARCH),)
GO_BUILD_ENV += GOARCH=$(GOARCH)
endif
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || true)
GO_LDFLAGS ?= -s -w $(if $(GIT_COMMIT),-X github.com/imksoo/routerd/pkg/version.Commit=$(GIT_COMMIT))
GO_BUILD_FLAGS ?= -trimpath -ldflags="$(GO_LDFLAGS)"
EXAMPLE_CONFIGS ?= $(wildcard examples/*.yaml)
PLAYWRIGHT_INSTALL_FLAGS ?= --with-deps

WEBSITE_NODE_MODULES_STAMP := website/node_modules/.package-lock.json

.PHONY: test build build-daemons build-ndpi-agent build-ndpi-agent-libndpi build-daemons-freebsd check-linux-static check-ndpi-agent-libndpi check-install-deps alpine-vm-smoke webconsole-build webconsole-browser-install webconsole-screenshot generate-schema check-schema check-examples-line-limits check-render-golden update-render-golden check-bespoke-lifecycle website-deps website-build third-party-licenses check-build-deps dist dist-ndpi-agent-libndpi live-iso validate-example dry-run-example plan-config release clean

test:
	go test ./...

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
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_PPPOE_CLIENT_BIN) ./cmd/routerd-pppoe-client

build-ndpi-agent:
	install -d $(BUILDDIR)
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_NDPI_AGENT_BIN) ./cmd/routerd-ndpi-agent

build-ndpi-agent-libndpi:
	install -d $(BUILDDIR)
	CGO_ENABLED=1 GOOS=$(ROUTERD_OS) $(if $(GOARCH),GOARCH=$(GOARCH),) go build $(GO_BUILD_FLAGS) -tags libndpi -o $(ROUTERD_NDPI_AGENT_BIN) ./cmd/routerd-ndpi-agent

build-daemons-freebsd:
	$(MAKE) build-daemons ROUTERD_OS=freebsd GOARCH=amd64

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

alpine-vm-smoke:
	./scripts/alpine-vm-smoke.sh

webconsole-build:
	cd webconsole && npm ci && npm run build

webconsole-browser-install:
	cd webconsole && npx playwright install $(PLAYWRIGHT_INSTALL_FLAGS) chromium

webconsole-screenshot: webconsole-build webconsole-browser-install
	cd webconsole && npm run screenshot

generate-schema:
	install -d schemas
	go run ./cmd/routerd-schema > schemas/routerd-config-v1alpha1.schema.json
	go run ./cmd/routerd-schema --schema control > schemas/routerd-control-v1alpha1.schema.json
	go run ./cmd/routerd-schema --schema control-openapi > schemas/routerd-control-openapi-v1alpha1.json

check-schema:
	go run ./cmd/routerd-schema > /tmp/routerd-config-v1alpha1.schema.json
	diff -u schemas/routerd-config-v1alpha1.schema.json /tmp/routerd-config-v1alpha1.schema.json
	go run ./cmd/routerd-schema --schema control > /tmp/routerd-control-v1alpha1.schema.json
	diff -u schemas/routerd-control-v1alpha1.schema.json /tmp/routerd-control-v1alpha1.schema.json
	go run ./cmd/routerd-schema --schema control-openapi > /tmp/routerd-control-openapi-v1alpha1.json
	diff -u schemas/routerd-control-openapi-v1alpha1.json /tmp/routerd-control-openapi-v1alpha1.json

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
	install -m 0755 $(ROUTERD_PPPOE_CLIENT_BIN) $(DISTROOT)/bin/routerd-pppoe-client
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
		install -m 0644 contrib/systemd/routerd.service $(DISTROOT)/systemd/routerd.service; \
	fi
	install -d $(DISTDIR)
	tar -C $(DISTROOT) -czf $(DISTTAR) .
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
	cp $(ROUTERD_NDPI_AGENT_LIBNDPI_TAR) $(ROUTERD_NDPI_AGENT_LIBNDPI_ALIAS)
	if command -v sha256sum >/dev/null 2>&1; then (cd $(DISTDIR) && sha256sum $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_TAR)) > $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_TAR)).sha256); elif command -v shasum >/dev/null 2>&1; then (cd $(DISTDIR) && shasum -a 256 $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_TAR)) > $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_TAR)).sha256); elif command -v sha256 >/dev/null 2>&1; then (cd $(DISTDIR) && sha256 -r $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_TAR)) > $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_TAR)).sha256); else echo "missing sha256 tool" >&2; exit 1; fi
	if command -v sha256sum >/dev/null 2>&1; then (cd $(DISTDIR) && sha256sum $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_ALIAS)) > $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_ALIAS)).sha256); elif command -v shasum >/dev/null 2>&1; then (cd $(DISTDIR) && shasum -a 256 $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_ALIAS)) > $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_ALIAS)).sha256); elif command -v sha256 >/dev/null 2>&1; then (cd $(DISTDIR) && sha256 -r $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_ALIAS)) > $(notdir $(ROUTERD_NDPI_AGENT_LIBNDPI_ALIAS)).sha256); else echo "missing sha256 tool" >&2; exit 1; fi

live-iso:
	VERSION=$(VERSION) DISTBASE=$(DISTBASE) scripts/build-live-iso.sh

validate-example:
	@for config in $(EXAMPLE_CONFIGS); do \
		echo "validating $$config"; \
		go run ./cmd/routerd validate --config "$$config"; \
	done

dry-run-example:
	go run ./cmd/routerd apply --config examples/basic-static.yaml --once --dry-run --status-file /tmp/routerd-status.json --state-file /tmp/routerd-dry-run-example.db --ledger-file /tmp/routerd-dry-run-ledger.db

plan-config:
	test -n "$(CONFIG)" || (echo "CONFIG is required, for example: make plan-config CONFIG=path/to/router.yaml" >&2; exit 2)
	go run ./cmd/routerd plan --config $(CONFIG) --status-file /tmp/routerd-plan-status.json

release:
	scripts/release.sh

clean:
	rm -rf bin dist
