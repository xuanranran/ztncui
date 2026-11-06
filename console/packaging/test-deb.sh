#!/bin/bash
set -euo pipefail
deb=${1:?DEB file required}
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq systemd init-system-helpers adduser iputils-ping python3 curl
mkdir -p /tmp/zt-fixture/DEBIAN
printf 'Package: zerotier-one\nVersion: 1.16.2\nArchitecture: all\nMaintainer: Test <test@example.invalid>\nDescription: isolated dependency fixture, not a real ZeroTier installation\n' > /tmp/zt-fixture/DEBIAN/control
dpkg-deb --build /tmp/zt-fixture /tmp/zerotier-fixture.deb
dpkg -i /tmp/zerotier-fixture.deb
mkdir -p /var/lib/zerotier-one
printf 'test-identity-preserve\n' > /var/lib/zerotier-one/identity.secret
printf 'test-token-preserve\n' > /var/lib/zerotier-one/authtoken.secret
chmod 0600 /var/lib/zerotier-one/*.secret
sha256sum /var/lib/zerotier-one/identity.secret /var/lib/zerotier-one/authtoken.secret > /tmp/controller-before.sha256
dpkg -i "$deb"
sha256sum -c /tmp/controller-before.sha256
test "$(dpkg-query -W -f='${Status}' ztncui-console)" = 'install ok installed'
test "$(dpkg-query -W -f='${Architecture}' ztncui-console)" = "$(dpkg --print-architecture)"
test "$(stat -c %a /var/lib/zerotier-one/authtoken.secret)" = 600
printf '[Service]\nType=oneshot\nExecStart=/bin/true\n' > /usr/lib/systemd/system/zerotier-one.service
systemd-analyze verify /usr/lib/systemd/system/ztncui-console.service
mkdir -p /tmp/credentials
cp /var/lib/zerotier-one/authtoken.secret /tmp/credentials/zerotier-token
chown ztncui:ztncui /tmp/credentials/zerotier-token
setpriv --reuid="$(id -u ztncui)" --regid="$(id -g ztncui)" --init-groups \
    env CREDENTIALS_DIRECTORY=/tmp/credentials /opt/ztncui-console/ztncui-console --addr=127.0.0.1:3105 --db=/var/lib/ztncui-console/console.sqlite &
pid=$!
for i in $(seq 1 30); do
    if curl -fsS http://127.0.0.1:3105/api/auth > /tmp/auth.json; then break; fi
    sleep 1
done
python3 -c 'import json; d=json.load(open("/tmp/auth.json")); assert d["setupRequired"] and not d["demo"]'
curl -fsS -H 'Content-Type: application/json' --data '{"username":"admin","password":"TestOnly-Strong-Password-123"}' http://127.0.0.1:3105/api/setup >/dev/null
kill "$pid"
wait "$pid"
python3 - <<'PY'
import sqlite3
with sqlite3.connect('/var/lib/ztncui-console/console.sqlite') as db:
    assert db.execute('select count(*) from admin').fetchone()[0] == 1
    db.execute("insert into metadata(key,value) values('test:preserved','\"keep\"')")
    db.commit()
    db.execute('pragma wal_checkpoint(TRUNCATE)')
PY
printf '\n# preserved user setting\n' >> /etc/ztncui-console/console.conf
sha256sum /var/lib/ztncui-console/console.sqlite /var/lib/zerotier-one/identity.secret /var/lib/zerotier-one/authtoken.secret /etc/ztncui-console/console.conf > /tmp/preserved.sha256
dpkg -i "$deb"
sha256sum -c /tmp/preserved.sha256
dpkg --remove ztncui-console
sha256sum -c /tmp/preserved.sha256
dpkg --purge ztncui-console
test -s /var/lib/ztncui-console/console.sqlite
test -s /var/lib/zerotier-one/identity.secret
test -s /var/lib/zerotier-one/authtoken.secret
echo 'PASS: fresh installation, real binary setup, reinstall, remove and purge preserve application/controller data'
