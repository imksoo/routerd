PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/sbin
SYSCONFDIR ?= $(PREFIX)/etc/routerd
PLUGINDIR ?= $(PREFIX)/libexec/routerd/plugins
SYSTEMDUNITDIR ?= $(PREFIX)/lib/systemd/system
RCDDIR ?= $(PREFIX)/etc/rc.d
DESTDIR ?=
DISTBASE ?= dist
DISTDIR ?= $(DISTBASE)/$(ROUTERD_OS)$(if $(GOARCH),-$(GOARCH))
DISTROOT ?= $(DISTDIR)/root
DISTTAR ?= $(DISTDIR)/routerd-install.tar
REMOTE_HOST ?=
REMOTE_TAR ?= /tmp/routerd-install.tar
CONFIG ?=
REMOTE_CONFIG ?= $(SYSCONFDIR)/router.yaml
UNAME_S := $(shell uname -s)

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
GO_BUILD_ENV := CGO_ENABLED=0 GOOS=$(ROUTERD_OS)
ifneq ($(GOARCH),)
GO_BUILD_ENV += GOARCH=$(GOARCH)
endif

.PHONY: test build generate-schema check-schema website-build check-build-deps check-remote-deps install install-service install-systemd install-rc-freebsd dist remote-install remote-install-config validate-example dry-run-example plan-config clean

test:
	go test ./...

build:
	install -d $(BUILDDIR)
	$(GO_BUILD_ENV) go build -o $(ROUTERD_BIN) ./cmd/routerd
	$(GO_BUILD_ENV) go build -o $(ROUTERCTL_BIN) ./cmd/routerctl

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

check-remote-deps:
	@test -n "$(REMOTE_HOST)" || (echo "REMOTE_HOST is required, for example: make check-remote-deps REMOTE_HOST=user@router.example" >&2; exit 2)
	@need_ppp=unknown; need_dhcp6c=unknown; need_bridge=unknown; \
	if [ -n "$(CONFIG)" ] && [ -f "$(CONFIG)" ]; then \
		grep -q "kind:[[:space:]]*PPPoEInterface" "$(CONFIG)" && need_ppp=1 || need_ppp=0; \
		grep -q "client:[[:space:]]*dhcp6c" "$(CONFIG)" && need_dhcp6c=1 || need_dhcp6c=0; \
		grep -q "kind:[[:space:]]*Bridge" "$(CONFIG)" && need_bridge=1 || need_bridge=0; \
	fi; \
	ssh $(REMOTE_HOST) "NEED_PPP=$$need_ppp NEED_DHCP6C=$$need_dhcp6c NEED_BRIDGE=$$need_bridge REMOTE_CONFIG=$(REMOTE_CONFIG) sh -c 'missing=0; \
		remote_os=\$$(uname -s); \
		need_ppp=\$${NEED_PPP:-unknown}; need_dhcp6c=\$${NEED_DHCP6C:-unknown}; need_bridge=\$${NEED_BRIDGE:-unknown}; \
		if [ \"\$$need_ppp\" = unknown ] && [ -r \"\$$REMOTE_CONFIG\" ]; then grep -q \"kind:[[:space:]]*PPPoEInterface\" \"\$$REMOTE_CONFIG\" && need_ppp=1 || need_ppp=0; fi; \
		if [ \"\$$need_dhcp6c\" = unknown ] && [ -r \"\$$REMOTE_CONFIG\" ]; then grep -q \"client:[[:space:]]*dhcp6c\" \"\$$REMOTE_CONFIG\" && need_dhcp6c=1 || need_dhcp6c=0; fi; \
		if [ \"\$$need_bridge\" = unknown ] && [ -r \"\$$REMOTE_CONFIG\" ]; then grep -q \"kind:[[:space:]]*Bridge\" \"\$$REMOTE_CONFIG\" && need_bridge=1 || need_bridge=0; fi; \
		[ \"\$$need_ppp\" = unknown ] && need_ppp=0; [ \"\$$need_dhcp6c\" = unknown ] && need_dhcp6c=0; [ \"\$$need_bridge\" = unknown ] && need_bridge=0; \
		if [ \"\$$remote_os\" = FreeBSD ]; then required=\"sudo tar install ifconfig sysctl sysrc service pfctl dnsmasq dhcp6c jq dig ping ping6 tcpdump traceroute netstat\"; else required=\"sudo tar install ip sysctl systemctl resolvectl dnsmasq nft conntrack jq dig ping tcpdump tracepath\"; fi; \
		for cmd in \$$required; do if ! command -v \$$cmd >/dev/null 2>&1; then echo \"missing remote dependency: \$$cmd\" >&2; missing=1; fi; done; \
		if [ \"\$$remote_os\" != FreeBSD ] && [ \"\$$need_dhcp6c\" = 1 ] && ! command -v dhcp6c >/dev/null 2>&1; then echo \"missing remote dependency: wide-dhcpv6-client / dhcp6c command\" >&2; missing=1; fi; \
		if [ \"\$$remote_os\" != FreeBSD ] && [ \"\$$need_bridge\" = 1 ] && ! command -v mstpctl >/dev/null 2>&1; then echo \"missing remote dependency: mstpd / mstpctl command for Bridge rstp\" >&2; missing=1; fi; \
		if [ \"\$$remote_os\" = FreeBSD ] && [ \"\$$need_ppp\" = 1 ] && ! command -v mpd5 >/dev/null 2>&1; then echo \"missing remote dependency: mpd5\" >&2; missing=1; fi; \
		if [ \"\$$remote_os\" != FreeBSD ] && [ \"\$$need_ppp\" = 1 ] && ! command -v pppd >/dev/null 2>&1 && ! test -x /usr/sbin/pppd; then echo \"missing remote dependency: ppp package / pppd command\" >&2; missing=1; fi; \
		exit \$$missing'"

install: check-build-deps build
	install -d $(DESTDIR)$(BINDIR)
	install -m 0755 $(ROUTERD_BIN) $(DESTDIR)$(BINDIR)/routerd
	install -m 0755 $(ROUTERCTL_BIN) $(DESTDIR)$(BINDIR)/routerctl
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
	rm -rf $(DISTROOT) $(DISTTAR)
	$(MAKE) install DESTDIR=$(abspath $(DISTROOT))
	$(MAKE) install-service DESTDIR=$(abspath $(DISTROOT))
	install -d $(DISTDIR)
	tar -C $(DISTROOT) -cf $(DISTTAR) .

remote-install: check-build-deps check-remote-deps dist
	test -n "$(REMOTE_HOST)" || (echo "REMOTE_HOST is required, for example: make remote-install REMOTE_HOST=user@router.example" >&2; exit 2)
	scp $(DISTTAR) $(REMOTE_HOST):$(REMOTE_TAR)
	ssh $(REMOTE_HOST) 'sudo tar --no-same-owner -C / -xf $(REMOTE_TAR) && rm -f $(REMOTE_TAR) && \
		if [ "$$(uname)" = "FreeBSD" ] && [ -f /usr/local/etc/rc.d/routerd ]; then \
			sudo sysrc routerd_enable=YES >/dev/null; \
		fi'

remote-install-config:
	test -n "$(REMOTE_HOST)" || (echo "REMOTE_HOST is required, for example: make remote-install-config REMOTE_HOST=user@router.example CONFIG=path/to/router.yaml" >&2; exit 2)
	test -n "$(CONFIG)" || (echo "CONFIG is required, for example: make remote-install-config REMOTE_HOST=user@router.example CONFIG=path/to/router.yaml" >&2; exit 2)
	scp $(CONFIG) $(REMOTE_HOST):/tmp/routerd-router.yaml
	ssh $(REMOTE_HOST) 'sudo install -d $(dir $(REMOTE_CONFIG)) && sudo install -m 0644 /tmp/routerd-router.yaml $(REMOTE_CONFIG) && rm -f /tmp/routerd-router.yaml'

validate-example:
	go run ./cmd/routerd validate --config examples/basic-static.yaml

dry-run-example:
	go run ./cmd/routerd apply --config examples/basic-static.yaml --once --dry-run --status-file /tmp/routerd-status.json

plan-config:
	test -n "$(CONFIG)" || (echo "CONFIG is required, for example: make plan-config CONFIG=path/to/router.yaml" >&2; exit 2)
	go run ./cmd/routerd plan --config $(CONFIG) --status-file /tmp/routerd-plan-status.json

clean:
	rm -rf bin dist
