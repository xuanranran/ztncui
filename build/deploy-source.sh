#!/bin/bash
# =====================================================================
# ztncui 源码模式部署脚本 (在目标 Debian 服务器上以 root 运行)
# 用法: bash deploy-source.sh /tmp/ztncui-src.tar.gz
#   - 安装 nodejs / 编译工具
#   - 解压源码到 /opt/ztncui, npm 安装依赖 (argon2 源码编译, 走国内镜像)
#   - 迁移旧版本 (/opt/key-networks/ztncui) 的 passwd / storage / tls
#   - 配置 zerotier-one token 访问, 安装 systemd 服务并启动
# =====================================================================
set -e

SRC_TARBALL="${1:-/tmp/ztncui-src.tar.gz}"
APP_DIR=/opt/ztncui
OLD_DIR=/opt/key-networks/ztncui
NPM_REGISTRY="${NPM_REGISTRY:-https://registry.npmmirror.com}"

if [ "$(id -u)" != "0" ]; then
  echo "请用 root 运行 (sudo bash $0 ...)"
  exit 1
fi
if [ ! -f "$SRC_TARBALL" ]; then
  echo "找不到源码包: $SRC_TARBALL"
  exit 1
fi

echo "[1/8] 安装依赖 (nodejs / npm / 编译工具)..."
if ! command -v node >/dev/null 2>&1; then
  apt-get update
  apt-get install -y nodejs npm
fi
# argon2 需从源码编译, 需要 build-essential + python3
apt-get install -y build-essential python3 openssl || true
echo "  node: $(node --version 2>/dev/null), npm: $(npm --version 2>/dev/null)"

echo "[2/8] 创建用户与目录..."
getent passwd ztncui >/dev/null || useradd --system --home-dir "$APP_DIR" --shell /usr/sbin/nologin ztncui
mkdir -p "$APP_DIR"

echo "[3/8] 解压源码到 $APP_DIR ..."
# 保留 etc (passwd/storage/tls), 仅覆盖代码
tar -xzf "$SRC_TARBALL" -C "$APP_DIR"

echo "[4/8] 安装 npm 依赖 (镜像: $NPM_REGISTRY, argon2 源码编译)..."
cd "$APP_DIR"
export npm_config_build_from_source=true
npm install --omit=dev --no-audit --no-fund --registry="$NPM_REGISTRY" --build-from-source \
  || npm install --production --registry="$NPM_REGISTRY" --build-from-source

echo "[5/8] 迁移旧版本用户参数..."
mkdir -p "$APP_DIR/etc/tls" "$APP_DIR/etc/storage"
if [ -d "$OLD_DIR/etc" ]; then
  if [ -f "$OLD_DIR/etc/passwd" ]; then cp -a "$OLD_DIR/etc/passwd" "$APP_DIR/etc/passwd"; echo "  迁移 登录账号 passwd"; fi
  if [ -d "$OLD_DIR/etc/storage" ]; then cp -a "$OLD_DIR/etc/storage/." "$APP_DIR/etc/storage/" 2>/dev/null || true; echo "  迁移 成员名称 storage"; fi
  if [ -f "$OLD_DIR/etc/tls/privkey.pem" ]; then cp -a "$OLD_DIR/etc/tls/." "$APP_DIR/etc/tls/" 2>/dev/null || true; echo "  迁移 TLS 证书"; fi
else
  echo "  未找到旧版本 $OLD_DIR/etc, 跳过迁移"
fi

# 无 passwd 则用默认; 无 TLS 则生成自签
[ -f "$APP_DIR/etc/passwd" ] || cp -a "$APP_DIR/etc/default.passwd" "$APP_DIR/etc/passwd"
if [ ! -f "$APP_DIR/etc/tls/privkey.pem" ] || [ ! -f "$APP_DIR/etc/tls/fullchain.pem" ]; then
  echo "  生成自签 TLS 证书..."
  openssl req -x509 -sha256 -nodes -days 3650 -newkey rsa:2048 \
    -keyout "$APP_DIR/etc/tls/privkey.pem" -out "$APP_DIR/etc/tls/fullchain.pem" \
    -subj "/C=XX/ST=YY/L=ZZ/O=Security/OU=SelfSigned/CN=ztncui"
fi
# 默认配置文件 (可选; 应用无此文件也能用默认值运行)
if [ ! -f "$APP_DIR/etc/ztncui.conf" ]; then
  cat > "$APP_DIR/etc/ztncui.conf" <<'CONF'
# HTTP_PORT=3000
# HTTP_ALL_INTERFACES=yes
# HTTPS_PORT=3443
ZT_ADDR=localhost:9993
# ZT_TOKEN=
# BASE_PATH=
CONF
fi

echo "[6/8] 配置 zerotier-one token 访问..."
if getent group zerotier-one >/dev/null; then
  usermod -a -G zerotier-one ztncui
  if [ -f /var/lib/zerotier-one/authtoken.secret ]; then
    chgrp zerotier-one /var/lib/zerotier-one/authtoken.secret || true
    chmod g+r /var/lib/zerotier-one/authtoken.secret || true
    chmod g+rx /var/lib/zerotier-one || true
  fi
else
  echo "  未发现 zerotier-one 组, 请确认已安装 zerotier-one"
fi

echo "[7/8] 安装 systemd 服务 (源码模式)..."
NODE_BIN="$(command -v node)"
cat > /etc/systemd/system/ztncui.service <<EOF
[Unit]
Description=ztncui - ZeroTier network controller user interface
Documentation=https://github.com/xuanranran/ztncui
After=network.target zerotier-one.service
Wants=zerotier-one.service

[Service]
Type=simple
User=ztncui
Group=ztncui
WorkingDirectory=$APP_DIR
EnvironmentFile=-$APP_DIR/etc/ztncui.conf
ExecStart=$NODE_BIN $APP_DIR/bin/www
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF

echo "[8/8] 权限与启动..."
chown -R ztncui:ztncui "$APP_DIR"
chmod 600 "$APP_DIR/etc/passwd" 2>/dev/null || true
# 停旧服务 (旧 deb 的 unit 同名, 位于 /lib/systemd; /etc 下新 unit 优先)
systemctl stop ztncui 2>/dev/null || true
systemctl daemon-reload
systemctl enable ztncui
systemctl restart ztncui
sleep 1
systemctl --no-pager --full status ztncui || true
echo "------------------------------------------------------------"
echo "完成。默认访问: http://$(hostname -I | awk '{print $1}'):3000"
echo "登录账号沿用旧版本 (已迁移 passwd)。"
echo "如需停用旧 deb 包: apt-get remove ztncui (确认新服务正常后再做)"
