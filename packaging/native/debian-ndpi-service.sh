# SPDX-License-Identifier: BSD-3-Clause
# Appended to the base hook's service functions by the native package builder.
agent=/usr/sbin/routerd-ndpi-agent
addon=routerd-ndpi-agent-libndpi

divert_agent()
{
    owner=$(dpkg-divert --listpackage "$agent")
    case "$owner" in
        '') dpkg-divert --package "$addon" --divert "$agent.routerd-static" --add --rename "$agent" ;;
        "$addon") ;;
        *) echo "routerd: $agent is diverted by $owner; refusing addon installation" >&2; return 1 ;;
    esac
}

restore_base_agent()
{
    owner=$(dpkg-divert --listpackage "$agent")
    case "$owner" in
        '') return 0 ;;
        "$addon") dpkg-divert --package "$addon" --divert "$agent.routerd-static" --remove --rename "$agent" ;;
        *) echo "routerd: refusing to remove $owner diversion" >&2; return 1 ;;
    esac
}

# The native builder assigns phase in each complete emitted script.
# shellcheck disable=SC2154
case "$phase:${1:-}" in
    preinst:install|preinst:upgrade)
        if [ -d /run/systemd/system ]; then stop_services; fi
        divert_agent
        ;;
    prerm:remove|prerm:upgrade)
        if [ -d /run/systemd/system ]; then stop_services; fi
        ;;
    postinst:configure)
        native_status=$("$agent" selftest)
        printf '%s\n' "$native_status" | grep -q '"libndpiLoaded":true'
        if [ -d /run/systemd/system ]; then restore_services; fi
        ;;
    postinst:abort-upgrade|postinst:abort-remove|postinst:abort-deconfigure)
        if [ -d /run/systemd/system ]; then restore_services; fi
        ;;
    postrm:remove|postrm:purge|postrm:abort-install)
        restore_base_agent
        if [ -d /run/systemd/system ]; then restore_services; fi
        ;;
esac
