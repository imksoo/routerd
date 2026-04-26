PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/sbin
SYSCONFDIR ?= $(PREFIX)/etc/routerd
PLUGINDIR ?= $(PREFIX)/libexec/routerd/plugins
SYSTEMDUNITDIR ?= $(PREFIX)/lib/systemd/system
DESTDIR ?=
DISTDIR ?= dist
DISTROOT ?= $(DISTDIR)/root
DISTTAR ?= $(DISTDIR)/routerd-install.tar
REMOTE_HOST ?=
REMOTE_TAR ?= /tmp/routerd-install.tar
CONFIG ?=
REMOTE_CONFIG ?= $(SYSCONFDIR)/router.yaml
UNAME_S := $(shell uname -s)

ifeq ($(UNAME_S),FreeBSD)
RUNDIR ?= /var/run/routerd
STATEDIR ?= /var/db/routerd
else
RUNDIR ?= /run/routerd
STATEDIR ?= /var/lib/routerd
endif

ROUTERD_BIN := bin/routerd

.PHONY: test build install install-systemd dist remote-install remote-install-config validate-example dry-run-example plan-config clean

test:
	go test ./...

build:
	go build -o $(ROUTERD_BIN) ./cmd/routerd

install: build
	install -d $(DESTDIR)$(BINDIR)
	install -m 0755 $(ROUTERD_BIN) $(DESTDIR)$(BINDIR)/routerd
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

install-systemd:
	install -d $(DESTDIR)$(SYSTEMDUNITDIR)
	install -m 0644 contrib/systemd/routerd.service $(DESTDIR)$(SYSTEMDUNITDIR)/routerd.service

dist:
	rm -rf $(DISTROOT) $(DISTTAR)
	$(MAKE) install DESTDIR=$(abspath $(DISTROOT))
	install -d $(DISTDIR)
	tar -C $(DISTROOT) -cf $(DISTTAR) .

remote-install: dist
	test -n "$(REMOTE_HOST)" || (echo "REMOTE_HOST is required, for example: make remote-install REMOTE_HOST=user@router.example" >&2; exit 2)
	scp $(DISTTAR) $(REMOTE_HOST):$(REMOTE_TAR)
	ssh $(REMOTE_HOST) 'sudo tar --no-same-owner -C / -xf $(REMOTE_TAR) && rm -f $(REMOTE_TAR)'

remote-install-config:
	test -n "$(REMOTE_HOST)" || (echo "REMOTE_HOST is required, for example: make remote-install-config REMOTE_HOST=user@router.example CONFIG=path/to/router.yaml" >&2; exit 2)
	test -n "$(CONFIG)" || (echo "CONFIG is required, for example: make remote-install-config REMOTE_HOST=user@router.example CONFIG=path/to/router.yaml" >&2; exit 2)
	scp $(CONFIG) $(REMOTE_HOST):/tmp/routerd-router.yaml
	ssh $(REMOTE_HOST) 'sudo install -d $(dir $(REMOTE_CONFIG)) && sudo install -m 0644 /tmp/routerd-router.yaml $(REMOTE_CONFIG) && rm -f /tmp/routerd-router.yaml'

validate-example:
	go run ./cmd/routerd validate --config examples/basic-static.yaml

dry-run-example:
	go run ./cmd/routerd reconcile --config examples/basic-static.yaml --once --dry-run --status-file /tmp/routerd-status.json

plan-config:
	test -n "$(CONFIG)" || (echo "CONFIG is required, for example: make plan-config CONFIG=path/to/router.yaml" >&2; exit 2)
	go run ./cmd/routerd plan --config $(CONFIG) --status-file /tmp/routerd-plan-status.json

clean:
	rm -rf bin dist
