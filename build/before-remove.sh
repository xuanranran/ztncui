#!/bin/bash

# $1 is "remove" on uninstall and "upgrade" on upgrade (deb prerm).
# Do not disable the service during an upgrade.
if [ -d /run/systemd/system ] && [ "$1" != "upgrade" ]; then
  systemctl stop ztncui || true
  systemctl disable ztncui || true
fi
