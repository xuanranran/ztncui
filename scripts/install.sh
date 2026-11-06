#!/bin/bash
# Debian/Ubuntu：安装或更新 Console DEB，仅在显式指定参数时替换 planet。
set -Eeuo pipefail
umask 077
VERSION=1.0.0
# 以下版本只用于编译 planet 生成工具，不是服务器 ZeroTier 的安装版本。
# 固定源码提交可避免上游工具移动或结构变化导致生成错误，勿只修改版本号。
# 已核对稳定版 1.16.2：上游已删除 attic/world/mkworld.cpp 和 build.sh。
# 删除提交：21986038be431d4ac3abc341e5876b6df740348c；因此暂不切换新版源码。
# 已于 2026-11-06 在 Ubuntu 24.04 amd64 隔离网络中实际编译并测试通过：
# 1.14.2 生成 planet，1.16.2 节点识别 PLANET、建立活动路径、上线并在重启后重连。
# 测试构建关闭 SSO；未覆盖 ARM64、业务网络路由、流规则或公网 NAT 场景。
# 测试记录：https://github.com/xuanranran/ztncui/actions/runs/42226024594
WORLD_BUILDER_TAG=1.14.2
WORLD_BUILDER_COMMIT=185a3a2c76e6bf1b1c0415871f43076638eb007c
DEB=
SHA=
IP=
ZT=/var/lib/zerotier-one
WORK=
BACKUP=
RESTART=false
die() { echo "错误：$*" >&2; exit 1; }
help() {
    cat <<'HELP'
用法：sudo bash scripts/install.sh [选项]
  --deb 文件路径          可选，使用本地 DEB；默认从公开 Releases 下载。
  --sha256 校验值         指定 SHA-256，否则需要同目录下的 安装包.sha256 文件。
  --version 版本号        Console 发行版本，默认 1.0.0。
  --generate-planet IPv4  使用指定公网 IPv4 和 9993 端口生成并替换 planet。
  --help                  仅显示帮助，不修改系统。

从 xuanranran/ztncui 的公开 Releases 下载，不需要 GitHub Token。
支持 amd64/arm64，保留现有数据、节点身份和服务配置。
不会自动清理系统软件、重置密码或删除旧版服务。
首次安装默认监听 127.0.0.1:3000，请先通过 SSH 隧道初始化管理员：
  ssh -L 3000:127.0.0.1:3000 root@SERVER
  浏览器打开 http://127.0.0.1:3000
之后可修改 /etc/ztncui-console/console.conf 并重启 ztncui-console。
/etc/systemd/system 中已有的自定义服务配置优先，不会被覆盖。
旧版 Node 界面的数据需要另行离线导入，本脚本不会自动迁移。
生成 planet 会重启 ZeroTier，所有客户端需要手动导入新 planet。
1.14.2 仅为 planet 生成工具源码版本，不会安装或降级该版本的 ZeroTier 服务。
HELP
}
while (($#)); do
    case "$1" in
        --help|-h) help; exit 0 ;;
        --deb|--sha256|--version|--generate-planet)
            (($# >= 2)) || die "选项缺少参数：$1"
            case "$1" in
                --deb) DEB=$2 ;; --sha256) SHA=$2 ;;
                --version) VERSION=$2 ;; --generate-planet) IP=$2 ;;
            esac
            shift 2 ;;
        *) die "未知选项：$1" ;;
    esac
done
[[ $EUID == 0 ]] || die '请使用 root 权限运行。'
[[ -f /etc/debian_version && -d /run/systemd/system ]] || die '需要运行 systemd 的 Debian/Ubuntu 系统。'
[[ $VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die '版本号格式无效。'
ARCH=$(dpkg --print-architecture)
[[ $ARCH == amd64 || $ARCH == arm64 ]] || die "不支持的处理器架构：$ARCH"
# 精简系统可能缺少 Python、文件锁工具或 HTTPS 根证书，按需补齐。
if ! command -v python3 >/dev/null || ! command -v flock >/dev/null ||
   ! dpkg-query -W -f='${Status}' ca-certificates 2>/dev/null | grep -qx 'install ok installed'; then
    apt-get update
    apt-get install -y python3 util-linux ca-certificates
fi
python3 - "$IP" <<'PY'
import ipaddress, sys
if sys.argv[1] and not ipaddress.IPv4Address(sys.argv[1]).is_global:
    raise SystemExit('planet 端点必须使用公网 IPv4 地址')
PY
# 同一时间只允许一个安装进程，防止备份与安装互相覆盖。
exec 9>/run/lock/ztncui-installer.lock
flock -n 9 || die '另一个安装进程正在运行，请稍后重试。'
cleanup() {
    result=$?
    trap - EXIT
    if $RESTART; then systemctl start ztncui-console.service || true; fi
    if ((result != 0)); then echo "操作已停止。备份：${BACKUP:-尚未创建}。请检查错误后重试。" >&2; fi
    [[ -z $WORK ]] || echo "已保留仅限管理员访问的工作文件：$WORK"
    exit "$result"
}
trap cleanup EXIT
backup_existing() {
BACKUP=/var/backups/ztncui-console/$(date -u +%Y%m%dT%H%M%SZ)-installer
install -d -m 0700 "$BACKUP"
if systemctl is-active --quiet ztncui-console.service; then
    RESTART=true
    systemctl stop ztncui-console.service
fi
for path in "$ZT" /var/lib/ztncui-console /etc/ztncui-console /etc/systemd/system/ztncui-console.service /etc/systemd/system/ztncui-console.service.d /opt/ztncui-console /var/lib/ztncui-planet-keys; do
    if [[ -e $path ]]; then
        tar -C / -czf "$BACKUP/$(echo "${path#/}" | tr / _).tar.gz" --exclude='*.sock' "${path#/}"
    fi
done
# 备份管理数据库时暂停界面服务，备份后恢复，避免下载和编译期间持续停机。
if $RESTART; then systemctl start ztncui-console.service; RESTART=false; fi
echo "备份目录：$BACKUP"
}
WORK=$(mktemp -d /var/tmp/ztncui-install.XXXXXXXX)
export VERSION ARCH WORK
if [[ -z $DEB ]]; then
    python3 <<'PY'
import os, shutil, urllib.request
from pathlib import Path
base = 'https://github.com/xuanranran/ztncui/releases/download/v' + os.environ['VERSION']
name = f"ztncui-console_{os.environ['VERSION']}_{os.environ['ARCH']}.deb"
for filename in (name, name + '.sha256'):
    request = urllib.request.Request(base + '/' + filename, headers={'User-Agent': 'ztncui-installer'})
    with urllib.request.urlopen(request, timeout=120) as response:
        with Path(os.environ['WORK'], filename).open('wb') as destination:
            shutil.copyfileobj(response, destination)
PY
    DEB=$WORK/ztncui-console_${VERSION}_${ARCH}.deb
fi
[[ -f $DEB ]] || die '未找到 DEB 安装包。'
DEB=$(realpath -- "$DEB")
if [[ -z $SHA ]]; then
    [[ -f $DEB.sha256 ]] || die '请提供 安装包.sha256 文件，或使用 --sha256 指定校验值。'
    read -r SHA _ < "$DEB.sha256"
fi
[[ $SHA =~ ^[[:xdigit:]]{64}$ ]] || die 'SHA-256 校验值格式无效。'
ACTUAL=$(sha256sum -- "$DEB")
[[ ${ACTUAL%% *} == "${SHA,,}" ]] || die '安装包校验失败，文件可能损坏或被修改。'
[[ $(dpkg-deb -f "$DEB" Package) == ztncui-console ]] || die '安装包名称不正确。'
[[ $(dpkg-deb -f "$DEB" Version) == "$VERSION" ]] || die '安装包版本与指定版本不符。'
[[ $(dpkg-deb -f "$DEB" Architecture) == "$ARCH" ]] || die '安装包架构与系统不符。'
# 下载、校验和架构检查成功后，才暂停现有界面服务进行备份。
backup_existing
apt-get update
apt-get install -y ca-certificates curl adduser iputils-ping
if ! dpkg-query -W -f='${Status}' zerotier-one 2>/dev/null | grep -qx 'install ok installed'; then
    curl --proto '=https' --tlsv1.2 -fsSL https://install.zerotier.com/ -o "$WORK/install-zerotier.sh"
    bash "$WORK/install-zerotier.sh"
fi
systemctl enable --now zerotier-one.service
for ((i=0; i<30; i++)); do
    [[ -s $ZT/identity.public && -s $ZT/authtoken.secret ]] && break
    sleep 1
done
[[ -s $ZT/identity.public && -s $ZT/authtoken.secret ]] || die 'ZeroTier 初始化失败。'
if [[ -n $IP ]]; then
    echo '警告：即将替换 planet，所有客户端都需要导入新 planet！'
    echo "生成工具源码：$WORLD_BUILDER_TAG；这不会更改已安装的 ZeroTier 服务版本。"
    apt-get install -y git build-essential
    git clone --depth 1 --branch "$WORLD_BUILDER_TAG" https://github.com/zerotier/ZeroTierOne.git "$WORK/ZeroTierOne"
    [[ $(git -C "$WORK/ZeroTierOne" rev-parse HEAD) == "$WORLD_BUILDER_COMMIT" ]] || die '生成工具源码提交与预期不符，已停止。'
    WORLD=$WORK/ZeroTierOne/attic/world
    KEYS=/var/lib/ztncui-planet-keys
    install -d -m 0700 "$KEYS"
    if [[ -e $KEYS/previous.c25519 || -e $KEYS/current.c25519 ]]; then
        [[ -s $KEYS/previous.c25519 && -s $KEYS/current.c25519 ]] || die 'planet 签名密钥不完整。'
        cp -p "$KEYS/previous.c25519" "$KEYS/current.c25519" "$WORLD/"
    fi
    python3 - "$WORLD/mkworld.cpp" "$ZT/identity.public" "$IP" <<'PY'
import json, re, sys, time
from pathlib import Path
path = Path(sys.argv[1])
source = path.read_text()
identity = Path(sys.argv[2]).read_text().strip()
if not re.fullmatch(r'[0-9a-f]{10}:0:[0-9a-f]{128}', identity):
    raise SystemExit('ZeroTier 公共身份文件格式不符合预期')
start = source.index('std::vector<World::Root> roots;')
end = source.index('// END WORLD DEFINITION', start)
definition = ('std::vector<World::Root> roots;\n'
              'const uint64_t id = ZT_WORLD_ID_EARTH;\n'
              f'const uint64_t ts = {int(time.time() * 1000)}ULL;\n'
              'roots.push_back(World::Root());\n'
              f'roots.back().identity = Identity({json.dumps(identity)});\n'
              f'roots.back().stableEndpoints.push_back(InetAddress({json.dumps(sys.argv[3] + "/9993")}));\n')
path.write_text(source[:start] + definition + source[end:])
PY
    (cd "$WORLD" && bash build.sh && ./mkworld > "$WORK/world-build.log")
    [[ -s $WORLD/world.bin ]] || die 'planet 构建失败。'
    install -m 0600 "$WORLD/previous.c25519" "$WORLD/current.c25519" "$KEYS/"
    install -m 0644 "$WORLD/world.bin" "$ZT/planet.new"
    chown --reference="$ZT/identity.public" "$ZT/planet.new"
    mv -f "$ZT/planet.new" "$ZT/planet"
    install -m 0644 "$WORLD/world.bin" /root/planet
    systemctl restart zerotier-one.service
    systemctl is-active --quiet zerotier-one.service
    echo '客户端文件：/root/planet。请勿分发签名密钥；防火墙和云安全组需放行 UDP 9993。'
fi
# 保留本地配置文件，以及优先级更高的自定义 systemd 服务配置。
if systemctl is-active --quiet ztncui-console.service; then RESTART=true; fi
DEBIAN_FRONTEND=noninteractive apt-get -y -o Dpkg::Options::=--force-confold install "$DEB"
systemctl daemon-reload
systemctl enable ztncui-console.service
systemctl restart ztncui-console.service
# 等待实际 HTTP 接口可用，不把刚启动便退出的服务误报为成功。
python3 <<'PY'
import json, pathlib, subprocess, time, urllib.request
error = '服务尚未启动'
for _ in range(30):
    try:
        pid = subprocess.check_output(['systemctl', 'show', 'ztncui-console.service', '-p', 'MainPID', '--value'], text=True).strip()
        args = pathlib.Path('/proc', pid, 'cmdline').read_bytes().decode().split('\0')
        address = next((a.split('=', 1)[1] for a in args if a.startswith('--addr=')), None)
        if address is None and '--addr' in args:
            address = args[args.index('--addr') + 1]
        if not address:
            raise ValueError('无法从服务参数确定监听地址')
        host, port = address.rsplit(':', 1)
        host = host.strip('[]')
        host = {'0.0.0.0': '127.0.0.1', '::': '::1', '': '127.0.0.1'}.get(host, host)
        host = '[' + host + ']' if ':' in host else host
        client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        with client.open('http://' + host + ':' + port + '/api/auth', timeout=2) as response:
            status = json.load(response)
        if status.get('demo') is not False or 'setupRequired' not in status:
            raise ValueError('接口返回内容不符合正式 Console 的预期')
        print('HTTP 就绪检查通过，服务监听地址：' + address)
        break
    except (OSError, ValueError, subprocess.SubprocessError) as exc:
        error = str(exc)
        time.sleep(1)
else:
    raise SystemExit('服务就绪检查失败，请查看 journalctl -u ztncui-console：' + error)
PY
RESTART=false
dpkg-query -W -f='${Package} ${Version} ${Architecture}\n' ztncui-console
echo 'Console 安装完成，未重置现有数据、配置和管理员密码。'
echo '首次安装请先通过 SSH 隧道初始化管理员，再按需开启局域网访问。'
echo "备份目录：$BACKUP"
