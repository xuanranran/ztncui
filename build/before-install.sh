#!/bin/bash

getent passwd ztncui >/dev/null || useradd --system --home-dir /opt/ztncui --shell /usr/sbin/nologin ztncui

if getent group zerotier-one >/dev/null; then
  echo "Adding user ztncui to group zerotier-one..."
  usermod -a -G zerotier-one ztncui
  # zerotier-one runs as root, so authtoken.secret is owned root:root mode 0600.
  # Re-group it to zerotier-one and make it (and its directory) group-readable
  # so the ztncui user can read the controller auth token.
  if [ -f /var/lib/zerotier-one/authtoken.secret ]; then
    chgrp zerotier-one /var/lib/zerotier-one/authtoken.secret || true
    chmod g+r /var/lib/zerotier-one/authtoken.secret || true
    chmod g+rx /var/lib/zerotier-one || true
  fi
else
  echo "Could not add user ztncui to group zerotier-one... is zerotier-one installed?"
fi
