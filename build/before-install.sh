#!/bin/bash

getent passwd ztncui >/dev/null || useradd --system --home-dir /opt/ztncui --shell /usr/sbin/nologin ztncui
