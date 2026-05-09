PREFIX ?= /usr/local
VERSION ?= 20260509
BINDIR ?= $(PREFIX)/sbin
SYSCONFDIR ?= $(PREFIX)/etc/routerd
PLUGINDIR ?= $(PREFIX)/libexec/routerd/plugins
SYSTEMDUNITDIR ?= $(PREFIX)/lib/systemd/system
RCDDIR ?= $(PREFIX)/etc/rc.d
DESTDIR ?=
DISTBASE ?= dist
DISTPLATFORM ?= $(ROUTERD_OS)$(if $(GOARCH),-$(GOARCH))
DISTDIR ?= $(DISTBASE)/$(DISTPLATFORM)
DISTROOT ?= $(DISTDIR)/package
DISTTAR ?= $(DISTDIR)/routerd-$(VERSION)-$(DISTPLATFORM).tar.gz
ROOTFSDISTROOT ?= $(DISTDIR)/root
ROOTFSDISTTAR ?= $(DISTDIR)/routerd-install.tar
REMOTE_HOST ?=
REMOTE_TAR ?= /tmp/routerd-install.tar
CONFIG ?=
REMOTE_CONFIG ?= $(SYSCONFDIR)/router.yaml
REMOTE_UNIT ?= contrib/systemd/routerd.service
REMOTE_UNIT_NAME ?= routerd.service
UNAME_S := $(shell uname -s)
UBUNTU_SERVICE_PACKAGES ?= dnsmasq-base nftables conntrack iproute2 iputils-ping iputils-tracepath dnsutils tcpdump traceroute procps ppp wireguard-tools strongswan-swanctl radvd systemd net-tools kmod libnetfilter-log1

ifeq ($(UNAME_S),FreeBSD)
ROUTERD_OS ?= freebsd
else
ROUTERD_OS ?= linux
endif

ifeq ($(ROUTERD_OS),freebsd)
RUNDIR ?= /var/run/routerd
STATEDIR ?= /var/db/routerd
INSTALL_SERVICE_TARGET ?= install-rc-freebsd
SERVICE_DEPS := pf dnsmasq dig ping ping6 tcpdump traceroute netstat
else
RUNDIR ?= /run/routerd
STATEDIR ?= /var/lib/routerd
INSTALL_SERVICE_TARGET ?= install-systemd
SERVICE_DEPS := systemctl resolvectl dnsmasq nft conntrack dig ping tcpdump tracepath
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
EXAMPLE_CONFIGS ?= examples/basic-static.yaml examples/dslite-lan-range-snat.yaml

.PHONY: test build build-daemons webconsole-build generate-schema check-schema website-build check-build-deps install-ubuntu-deps remote-install-service-deps check-remote-deps install install-service install-systemd install-rc-freebsd dist dist-rootfs remote-install remote-install-config remote-install-systemd-unit validate-example dry-run-example plan-config clean

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

install-ubuntu-deps:
	sudo apt-get update
	sudo apt-get install -y $(UBUNTU_SERVICE_PACKAGES)

remote-install-service-deps:
	test -n "$(REMOTE_HOST)" || (echo "REMOTE_HOST is required, for example: make remote-install-service-deps REMOTE_HOST=user@router.example" >&2; exit 2)
	ssh $(REMOTE_HOST) 'set -eu; \
		remote_os=$$(uname -s); \
		if [ "$$remote_os" = Linux ]; then \
			if command -v apt-get >/dev/null 2>&1; then \
				sudo apt-get update; \
				sudo apt-get install -y $(UBUNTU_SERVICE_PACKAGES); \
			else \
				echo "unsupported Linux package manager; install: $(UBUNTU_SERVICE_PACKAGES)" >&2; \
				exit 2; \
			fi; \
		else \
			echo "remote service dependency installation is not implemented for $$remote_os; use check-remote-deps" >&2; \
		fi'

check-remote-deps:
	@test -n "$(REMOTE_HOST)" || (echo "REMOTE_HOST is required, for example: make check-remote-deps REMOTE_HOST=user@router.example" >&2; exit 2)
	@need_ppp=unknown; need_dhcp6c=unknown; need_bridge=unknown; \
	if [ -n "$(CONFIG)" ] && [ -f "$(CONFIG)" ]; then \
		grep -q "kind:[[:space:]]*PPPoEInterface" "$(CONFIG)" && need_ppp=1 || need_ppp=0; \
		grep -q "client:[[:space:]]*dhcp6c" "$(CONFIG)" && need_dhcp6c=1 || need_dhcp6c=0; \
		grep -q "kind:[[:space:]]*Bridge" "$(CONFIG)" && need_bridge=1 || need_bridge=0; \
	fi; \
	ssh $(REMOTE_HOST) "NEED_PPP=$$need_ppp NEED_DHCPv6C=$$need_dhcp6c NEED_BRIDGE=$$need_bridge REMOTE_CONFIG=$(REMOTE_CONFIG) sh -c 'missing=0; \
		remote_os=\$$(uname -s); \
		need_ppp=\$${NEED_PPP:-unknown}; need_dhcp6c=\$${NEED_DHCPv6C:-unknown}; need_bridge=\$${NEED_BRIDGE:-unknown}; \
		if [ \"\$$need_ppp\" = unknown ] && [ -r \"\$$REMOTE_CONFIG\" ]; then grep -q \"kind:[[:space:]]*PPPoEInterface\" \"\$$REMOTE_CONFIG\" && need_ppp=1 || need_ppp=0; fi; \
		if [ \"\$$need_dhcp6c\" = unknown ] && [ -r \"\$$REMOTE_CONFIG\" ]; then grep -q \"client:[[:space:]]*dhcp6c\" \"\$$REMOTE_CONFIG\" && need_dhcp6c=1 || need_dhcp6c=0; fi; \
		if [ \"\$$need_bridge\" = unknown ] && [ -r \"\$$REMOTE_CONFIG\" ]; then grep -q \"kind:[[:space:]]*Bridge\" \"\$$REMOTE_CONFIG\" && need_bridge=1 || need_bridge=0; fi; \
		[ \"\$$need_ppp\" = unknown ] && need_ppp=0; [ \"\$$need_dhcp6c\" = unknown ] && need_dhcp6c=0; [ \"\$$need_bridge\" = unknown ] && need_bridge=0; \
		if [ \"\$$remote_os\" = FreeBSD ]; then required=\"sudo tar install ifconfig sysctl sysrc service pfctl dnsmasq dhcp6c jq dig ping ping6 tcpdump traceroute netstat\"; else required=\"sudo tar install ip sysctl systemctl resolvectl dnsmasq nft conntrack jq dig ping tcpdump tracepath\"; fi; \
		for cmd in \$$required; do if ! command -v \$$cmd >/dev/null 2>&1; then echo \"missing remote dependency: \$$cmd\" >&2; missing=1; fi; done; \
		if [ \"\$$remote_os\" != FreeBSD ] && [ \"\$$need_dhcp6c\" = 1 ] && ! command -v dhcp6c >/dev/null 2>&1; then echo \"missing remote dependency: wide-dhcpv6-client / dhcp6c command\" >&2; missing=1; fi; \
		if [ \"\$$remote_os\" != FreeBSD ] && [ \"\$$need_bridge\" = 1 ] && ! command -v mstpctl >/dev/null 2>&1; then echo \"warning: mstpd / mstpctl not installed; Bridge with rstp will fall back to kernel STP (slower convergence)\" >&2; fi; \
		if [ \"\$$remote_os\" = FreeBSD ] && [ \"\$$need_ppp\" = 1 ] && ! command -v mpd5 >/dev/null 2>&1; then echo \"missing remote dependency: mpd5\" >&2; missing=1; fi; \
		if [ \"\$$remote_os\" != FreeBSD ] && [ \"\$$need_ppp\" = 1 ] && ! command -v pppd >/dev/null 2>&1 && ! test -x /usr/sbin/pppd; then echo \"missing remote dependency: ppp package / pppd command\" >&2; missing=1; fi; \
		exit \$$missing'"

install: check-build-deps build
	install -d $(DESTDIR)$(BINDIR)
	install -m 0755 $(ROUTERD_BIN) $(DESTDIR)$(BINDIR)/routerd
	install -m 0755 $(ROUTERCTL_BIN) $(DESTDIR)$(BINDIR)/routerctl
	install -m 0755 $(ROUTERD_DHCPv4_CLIENT_BIN) $(DESTDIR)$(BINDIR)/routerd-dhcpv4-client
	install -m 0755 $(ROUTERD_DHCPv6_CLIENT_BIN) $(DESTDIR)$(BINDIR)/routerd-dhcpv6-client
	install -d $(DESTDIR)$(PREFIX)/libexec/routerd
	install -m 0755 $(ROUTERD_DHCP_EVENT_RELAY_BIN) $(DESTDIR)$(PREFIX)/libexec/routerd/dhcp-event-relay
	install -m 0755 $(ROUTERD_HEALTHCHECK_BIN) $(DESTDIR)$(BINDIR)/routerd-healthcheck
	install -m 0755 $(ROUTERD_DNS_RESOLVER_BIN) $(DESTDIR)$(BINDIR)/routerd-dns-resolver
	install -m 0755 $(ROUTERD_FIREWALL_LOGGER_BIN) $(DESTDIR)$(BINDIR)/routerd-firewall-logger
	install -m 0755 $(ROUTERD_PPPOE_CLIENT_BIN) $(DESTDIR)$(BINDIR)/routerd-pppoe-client
	install -d $(DESTDIR)$(SYSCONFDIR)
	install -m 0644 examples/basic-static.yaml $(DESTDIR)$(SYSCONFDIR)/router.yaml.example
	install -d $(DESTDIR)$(SYSCONFDIR)/examples
	install -m 0644 examples/*.yaml $(DESTDIR)$(SYSCONFDIR)/examples/
	install -d $(DESTDIR)$(PLUGINDIR)
	cp -R plugins/. $(DESTDIR)$(PLUGINDIR)/
	find $(DESTDIR)$(PLUGINDIR) -type d -exec chmod 0755 {} +
	find $(DESTDIR)$(PLUGINDIR) -type f -name 'plugin.sh' -exec chmod 0755 {} +
	find $(DESTDIR)$(PLUGINDIR) -type f -name 'plugin.yaml' -exec chmod 0644 {} +
	install -d $(DESTDIR)$(RUNDIR)
	install -d $(DESTDIR)$(STATEDIR)

install-service: $(INSTALL_SERVICE_TARGET)

install-systemd:
	install -d $(DESTDIR)$(SYSTEMDUNITDIR)
	install -m 0644 contrib/systemd/routerd.service $(DESTDIR)$(SYSTEMDUNITDIR)/routerd.service

install-rc-freebsd:
	install -d $(DESTDIR)$(RCDDIR)
	install -m 0555 contrib/freebsd/routerd $(DESTDIR)$(RCDDIR)/routerd

dist:
	rm -rf $(DISTROOT) $(DISTTAR) $(DISTTAR).sha256
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
	if [ "$(ROUTERD_OS)" = "freebsd" ]; then \
		install -d $(DISTROOT)/rc.d; \
		install -m 0555 contrib/freebsd/routerd $(DISTROOT)/rc.d/routerd; \
	else \
		install -d $(DISTROOT)/systemd; \
		install -m 0644 contrib/systemd/routerd.service $(DISTROOT)/systemd/routerd.service; \
	fi
	install -d $(DISTDIR)
	tar -C $(DISTROOT) -czf $(DISTTAR) .
	if command -v sha256sum >/dev/null 2>&1; then sha256sum $(DISTTAR) > $(DISTTAR).sha256; elif command -v shasum >/dev/null 2>&1; then shasum -a 256 $(DISTTAR) > $(DISTTAR).sha256; elif command -v sha256 >/dev/null 2>&1; then sha256 -r $(DISTTAR) > $(DISTTAR).sha256; else echo "missing sha256 tool" >&2; exit 1; fi

dist-rootfs:
	rm -rf $(ROOTFSDISTROOT) $(ROOTFSDISTTAR)
	$(MAKE) install DESTDIR=$(abspath $(ROOTFSDISTROOT))
	$(MAKE) install-service DESTDIR=$(abspath $(ROOTFSDISTROOT))
	install -d $(DISTDIR)
	tar -C $(ROOTFSDISTROOT) -cf $(ROOTFSDISTTAR) .

remote-install: check-build-deps remote-install-service-deps check-remote-deps dist-rootfs
	test -n "$(REMOTE_HOST)" || (echo "REMOTE_HOST is required, for example: make remote-install REMOTE_HOST=user@router.example" >&2; exit 2)
	scp $(ROOTFSDISTTAR) $(REMOTE_HOST):$(REMOTE_TAR)
	ssh $(REMOTE_HOST) 'sudo tar --no-same-owner -C / -xf $(REMOTE_TAR) && rm -f $(REMOTE_TAR) && \
		if [ "$$(uname)" = "FreeBSD" ] && [ -f /usr/local/etc/rc.d/routerd ]; then \
			sudo sysrc routerd_enable=YES >/dev/null; \
		fi'

remote-install-config:
	test -n "$(REMOTE_HOST)" || (echo "REMOTE_HOST is required, for example: make remote-install-config REMOTE_HOST=user@router.example CONFIG=path/to/router.yaml" >&2; exit 2)
	test -n "$(CONFIG)" || (echo "CONFIG is required, for example: make remote-install-config REMOTE_HOST=user@router.example CONFIG=path/to/router.yaml" >&2; exit 2)
	scp $(CONFIG) $(REMOTE_HOST):/tmp/routerd-router.yaml
	ssh $(REMOTE_HOST) 'sudo install -d $(dir $(REMOTE_CONFIG)) && sudo install -m 0644 /tmp/routerd-router.yaml $(REMOTE_CONFIG) && rm -f /tmp/routerd-router.yaml'

remote-install-systemd-unit:
	test -n "$(REMOTE_HOST)" || (echo "REMOTE_HOST is required, for example: make remote-install-systemd-unit REMOTE_HOST=user@router.example REMOTE_UNIT=contrib/systemd/routerd.service" >&2; exit 2)
	test -f "$(REMOTE_UNIT)" || (echo "REMOTE_UNIT does not exist: $(REMOTE_UNIT)" >&2; exit 2)
	scp $(REMOTE_UNIT) $(REMOTE_HOST):/tmp/routerd-systemd-unit.service
	ssh $(REMOTE_HOST) 'sudo install -m 0644 /tmp/routerd-systemd-unit.service /etc/systemd/system/$(REMOTE_UNIT_NAME) && rm -f /tmp/routerd-systemd-unit.service && sudo systemctl daemon-reload'

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

clean:
	rm -rf bin dist
