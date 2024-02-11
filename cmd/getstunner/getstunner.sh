#!/bin/sh

# STUNner tools downloader script
#
# inspired by https://raw.githubusercontent.com/istio/istio/master/release/downloadIstioCtl.sh


REPO=https://github.com/l7mp/stunner

# Determine OS
OS="${TARGET_OS:-$(uname)}"
if [ "${OS}" = "Darwin" ] ; then
  OSEXT="darwin"
else
  OSEXT="linux"
fi

# Determine the latest STUNner version
if [ "${STUNNER_VERSION}" = "" ] ; then
  STUNNER_VERSION="$(curl -sL ${REPO}/releases | \
                  grep -o 'releases/tag/v[0-9]*.[0-9]*.[0-9]*' | sort -V | \
                  tail -1 | awk -F'/' '{ print $3}')"
  STUNNER_VERSION="${STUNNER_VERSION##*/}"
fi

# Determine build params
if [ "${STUNNER_VERSION}" = "" ] ; then
  printf "Unable to get latest Stunner version. Set STUNNER_VERSION env var and re-run. For example: export STUNNER_VERSION=0.18.0"
  exit 1;
fi

LOCAL_ARCH=$(uname -m)
if [ "${TARGET_ARCH}" ]; then
    LOCAL_ARCH=${TARGET_ARCH}
fi

case "${LOCAL_ARCH}" in
  x86_64|amd64)
    STUNNER_ARCH=amd64
    ;;
  armv8*|aarch64*|arm64)
    STUNNER_ARCH=arm64
    ;;
  *)
    echo "This system's architecture, ${LOCAL_ARCH}, isn't supported"
    exit 1
    ;;
esac

# Download binaries
progs="stunnerctl turncat"
tmp=$(mktemp -d /tmp/stunner.XXXXXX)

for prog in $progs; do
    NAME="${prog}-${STUNNER_VERSION}"
    URL="${REPO}/releases/download/${STUNNER_VERSION}/${prog}-${STUNNER_VERSION}-${OSEXT}-${STUNNER_ARCH}"
    filename="${prog}-${STUNNER_VERSION}-${OSEXT}-${STUNNER_ARCH}"

    printf "\nDownloading %s from %s ...\n" "${NAME}" "$URL"
    if ! curl -o /dev/null -sIf "$URL"; then
	printf "\n%s is not found, please specify a valid STUNNER_VERSION and TARGET_ARCH\n" "$URL"
	exit 1
    fi
    curl -fsL -o "${tmp}/${filename}" "$URL"
    printf "%s download complete!\n" "${filename}"

    mkdir -p "$HOME/.l7mp/bin"
    mv "${tmp}/${filename}" "$HOME/.l7mp/bin/${prog}"
    chmod +x "$HOME/.l7mp/bin/${prog}"
done

rm -r "${tmp}"

# Print final message
printf "\n"
printf "Add stunner tools to your path with:"
printf "\n"
printf "  export PATH=\$HOME/.l7mp/bin:\$PATH \n"
printf "\n"
printf "Need more information? Visit https://docs.l7mp.io/en/${STUNNER_VERSION}/ \n"
