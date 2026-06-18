#!/bin/bash

if [ -f /opt/ztncui/etc/tls/privkey.pem ] && [ -f /opt/ztncui/etc/tls/fullchain.pem ]; then
  echo "TLS key and certificate already exist"
else
  echo "Generating new TLS key and self-signed certificate..."
  openssl req -x509 -sha256 -nodes -days 365 -newkey rsa:2048 -keyout /opt/ztncui/etc/tls/privkey.pem -out /opt/ztncui/etc/tls/fullchain.pem -subj "/C=XX/ST=YY/L=ZZ/O=Security/OU=SelfSigned/CN=example.com"
fi
chown ztncui:ztncui /opt/ztncui/etc/tls/*
if [ -d /run/systemd/system ]; then
  systemctl daemon-reload
  systemctl try-restart ztncui
fi
