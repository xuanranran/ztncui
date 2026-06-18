#!/bin/bash

set -e

THISDIR=`pwd`
if [ `basename $THISDIR`  != 'build' ]; then
  echo "Execute `basename $0` from the build directory"
  exit 1
fi

BASE_DIR=`dirname $THISDIR`
SRC_DIR=$BASE_DIR/src
BUILD_DIR=$BASE_DIR/build
PKG_DIR=$BASE_DIR/Release
STAGING_DIR=$BASE_DIR/Staging

NAME='ztncui'
DESCRIPTION='ZeroTier network controller user interface'
VERSION=`grep version ../src/package.json | cut -f4 -d'"'`
VENDOR='xuanranran'
MAINTAINER='xuanranran <https://github.com/xuanranran/ztncui>'
URL='https://github.com/xuanranran/ztncui'
LICENSE='GPLv3'

BINDINGGYP='node_modules/argon2/binding.gyp'

# Build with Node 20 LTS. 'pkg' here is the maintained fork @yao-pkg/pkg
# (install with: npm i -g @yao-pkg/pkg). It keeps the same 'pkg' CLI/config.
NODE_VER='v20'

# Locate the static libstdc++ to bundle into the pkg binary (distro-agnostic).
LIBSTDCXX_STATIC=`g++ -print-file-name=libstdc++.a`
if [ ! -f "$LIBSTDCXX_STATIC" ]; then
  echo "You must install a static libstdc++ (e.g. apt-get install g++ libstdc++-*-dev)"
  exit 1
fi

DEPS="dpkg-deb fpm pkg npm node"

for DEP in ${DEPS}; do
  if ! which ${DEP}; then
    echo "Missing dependency ${DEP}"
    exit 1
  fi
done

rm -fr $STAGING_DIR && mkdir $STAGING_DIR
rm -fr $PKG_DIR && mkdir $PKG_DIR

pushd .
cd ../src
pushd .

NVER=`node --version`
if [[ ${NVER%%.*} != ${NODE_VER} ]]; then
  echo "Missing dependency node ${NODE_VER}"
  exit 1
fi

[[ -d ../src/node_modules ]] && rm -fr ../src/node_modules

npm install

# Inject the detected static libstdc++ path into argon2's binding.gyp.
ARGON2_PATCH=`mktemp`
sed "s#@LIBSTDCXX_STATIC@#${LIBSTDCXX_STATIC}#g" $BUILD_DIR/binding.gyp.patch > $ARGON2_PATCH
if patch --forward --dry-run --silent $BINDINGGYP $ARGON2_PATCH; then
  echo "Applying patch to $BINDINGGYP..."
  patch --forward $BINDINGGYP $ARGON2_PATCH
fi
rm -f $ARGON2_PATCH

cd node_modules/argon2/
node-gyp rebuild
if [ $? -ne 0 ]; then
  echo "Failed to rebuild argon2"
  exit 1
fi

popd
pkg -c ./package.json -t node20-linux-x64 bin/www -o $BUILD_DIR/ztncui

popd

install -m 755 -d $STAGING_DIR/opt
install -m 750 -d $STAGING_DIR/opt/ztncui
install -m 750 -d $STAGING_DIR/opt/ztncui/etc
install -m 750 -d $STAGING_DIR/opt/ztncui/etc/tls
install -m 750 -d $STAGING_DIR/opt/ztncui/node_modules/argon2/build/Release
install -m 755 -d $STAGING_DIR/lib/systemd/system
install -m 600 $SRC_DIR/etc/default.passwd $STAGING_DIR/opt/ztncui/etc/default.passwd
install -m 755 $SRC_DIR/node_modules/argon2/build/Release/argon2.node $STAGING_DIR/opt/ztncui/node_modules/argon2/build/Release/
install -m 755 $BUILD_DIR/ztncui $STAGING_DIR/opt/ztncui/
install -m 644 $BUILD_DIR/ztncui.service $STAGING_DIR/lib/systemd/system
install -m 640 $BUILD_DIR/ztncui.conf $STAGING_DIR/opt/ztncui/etc/ztncui.conf

rm -f $BUILD_DIR/ztncui

GENERAL_FPM_FLAGS="
  --name $NAME
  --version $VERSION
  --url $URL
  --license $LICENSE
  --chdir $STAGING_DIR
  --package $PKG_DIR
  --directories /opt/ztncui
  --depends zerotier-one
  --depends openssl
  --config-files /opt/ztncui/etc/ztncui.conf
  --before-install before-install.sh
  --after-install after-install.sh
  --before-remove before-remove.sh
  --after-remove after-remove.sh
  --before-upgrade before-upgrade.sh
  --after-upgrade after-upgrade.sh
"

fpm -s dir -t deb \
  $GENERAL_FPM_FLAGS \
  --vendor "$VENDOR" \
  --maintainer "$MAINTAINER" \
  --description "$DESCRIPTION" \
  --deb-user ztncui \
  --deb-group ztncui \
  .

# To sign the resulting .deb (optional):
# dpkg-sig --sign builder $PKG_DIR/ztncui*.deb
