VERSION ?= v20260510.1301
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
ROUTERD_HEALTHCHECK_BIN := $(BUILDDIR)/routerd-healthcheck
ROUTERD_DNS_RESOLVER_BIN := $(BUILDDIR)/routerd-dns-resolver
ROUTERD_FIREWALL_LOGGER_BIN := $(BUILDDIR)/routerd-firewall-logger
ROUTERD_PPPOE_CLIENT_BIN := $(BUILDDIR)/routerd-pppoe-client
GO_BUILD_ENV := CGO_ENABLED=0 GOOS=$(ROUTERD_OS)
ifneq ($(GOARCH),)
GO_BUILD_ENV += GOARCH=$(GOARCH)
endif
GO_BUILD_FLAGS ?= -trimpath -ldflags="-s -w"
EXAMPLE_CONFIGS ?= $(wildcard examples/*.yaml)

.PHONY: test build build-daemons build-daemons-freebsd webconsole-build generate-schema check-schema website-build check-build-deps dist live-iso validate-example dry-run-example plan-config release clean

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
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_HEALTHCHECK_BIN) ./cmd/routerd-healthcheck
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_DNS_RESOLVER_BIN) ./cmd/routerd-dns-resolver
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_FIREWALL_LOGGER_BIN) ./cmd/routerd-firewall-logger
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(ROUTERD_PPPOE_CLIENT_BIN) ./cmd/routerd-pppoe-client

build-daemons-freebsd:
	$(MAKE) build-daemons ROUTERD_OS=freebsd GOARCH=amd64

webconsole-build:
	cd webconsole && npm ci && npm run build

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

website-build:
	cd website && npm ci && npm run build

check-build-deps:
	@missing=0; \
	for cmd in go install tar find cp; do \
		if ! command -v $$cmd >/dev/null 2>&1; then echo "missing build dependency: $$cmd" >&2; missing=1; fi; \
	done; \
	exit $$missing

dist:
	rm -rf $(DISTROOT) $(DISTTAR) $(DISTTAR).sha256 $(DISTTAR_ALIAS) $(DISTTAR_ALIAS).sha256
	$(MAKE) build-daemons
	install -d $(DISTROOT)/bin
	install -m 0755 $(ROUTERD_BIN) $(DISTROOT)/bin/routerd
	install -m 0755 $(ROUTERCTL_BIN) $(DISTROOT)/bin/routerctl
	install -m 0755 $(ROUTERD_DHCPv4_CLIENT_BIN) $(DISTROOT)/bin/routerd-dhcpv4-client
	install -m 0755 $(ROUTERD_DHCPv6_CLIENT_BIN) $(DISTROOT)/bin/routerd-dhcpv6-client
	install -m 0755 $(ROUTERD_DHCP_EVENT_RELAY_BIN) $(DISTROOT)/bin/routerd-dhcp-event-relay
	install -m 0755 $(ROUTERD_HEALTHCHECK_BIN) $(DISTROOT)/bin/routerd-healthcheck
	install -m 0755 $(ROUTERD_DNS_RESOLVER_BIN) $(DISTROOT)/bin/routerd-dns-resolver
	install -m 0755 $(ROUTERD_FIREWALL_LOGGER_BIN) $(DISTROOT)/bin/routerd-firewall-logger
	install -m 0755 $(ROUTERD_PPPOE_CLIENT_BIN) $(DISTROOT)/bin/routerd-pppoe-client
	install -m 0755 packaging/install.sh $(DISTROOT)/install.sh
	install -m 0755 packaging/uninstall.sh $(DISTROOT)/uninstall.sh
	install -d $(DISTROOT)/etc/routerd
	install -m 0644 examples/router-lab.yaml $(DISTROOT)/etc/routerd/router.yaml.sample
	install -d $(DISTROOT)/share/doc
	install -m 0644 README.md $(DISTROOT)/share/doc/README.md
	if [ -f README.ja.md ]; then install -m 0644 README.ja.md $(DISTROOT)/share/doc/README.ja.md; fi
	if [ -f LICENSE ]; then install -m 0644 LICENSE $(DISTROOT)/share/doc/LICENSE; elif [ -f LICENSE.md ]; then install -m 0644 LICENSE.md $(DISTROOT)/share/doc/LICENSE; else printf '%s\n' 'No LICENSE file is present in this repository.' > $(DISTROOT)/share/doc/LICENSE; fi
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
	if command -v sha256sum >/dev/null 2>&1; then sha256sum $(DISTTAR) > $(DISTTAR).sha256; elif command -v shasum >/dev/null 2>&1; then shasum -a 256 $(DISTTAR) > $(DISTTAR).sha256; elif command -v sha256 >/dev/null 2>&1; then sha256 -r $(DISTTAR) > $(DISTTAR).sha256; else echo "missing sha256 tool" >&2; exit 1; fi
	if command -v sha256sum >/dev/null 2>&1; then sha256sum $(DISTTAR_ALIAS) > $(DISTTAR_ALIAS).sha256; elif command -v shasum >/dev/null 2>&1; then shasum -a 256 $(DISTTAR_ALIAS) > $(DISTTAR_ALIAS).sha256; elif command -v sha256 >/dev/null 2>&1; then sha256 -r $(DISTTAR_ALIAS) > $(DISTTAR_ALIAS).sha256; else echo "missing sha256 tool" >&2; exit 1; fi

live-iso:
	VERSION=$(VERSION) DISTBASE=$(DISTBASE) scripts/build-live-iso.sh

validate-example:
	@for config in $(EXAMPLE_CONFIGS); do \
		echo "validating $$config"; \
		go run ./cmd/routerd validate --config "$$config"; \
	done

dry-run-example:
	go run ./cmd/routerd apply --config examples/basic-static.yaml --once --dry-run --status-file /tmp/routerd-status.json

plan-config:
	test -n "$(CONFIG)" || (echo "CONFIG is required, for example: make plan-config CONFIG=path/to/router.yaml" >&2; exit 2)
	go run ./cmd/routerd plan --config $(CONFIG) --status-file /tmp/routerd-plan-status.json

release:
	scripts/release.sh

clean:
	rm -rf bin dist
