#!/bin/bash

ETC='/opt/ztncui/etc'
if [ -f ${ETC}/passwd ]; then
  echo "Password file aready exists"
else
  echo "Copying default password file..."
  cp -pv ${ETC}/default.passwd ${ETC}/passwd
fi
if [ -f /opt/ztncui/etc/tls/privkey.pem ] && [ -f /opt/ztncui/etc/tls/fullchain.pem ]; then
  echo "TLS key and certificate already exist"
else
  echo "Generating new TLS key and self-signed certificate..."
  openssl req -x509 -sha256 -nodes -days 365 -newkey rsa:2048 -keyout /opt/ztncui/etc/tls/privkey.pem -out /opt/ztncui/etc/tls/fullchain.pem -subj "/C=XX/ST=YY/L=ZZ/O=Security/OU=SelfSigned/CN=example.com"
fi
chown ztncui:ztncui /opt/ztncui/etc/tls/*
if [ -d /run/systemd/system ]; then
  echo "Enabling and starting ztncui service..."
  systemctl daemon-reload
  systemctl enable ztncui
  systemctl restart ztncui
fi
