#!/usr/bin/env bash
set -euo pipefail

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
root=$(cd -- "$here/../../.." && pwd)
assets=${ZAPRET2_ASSETS_DIR:-"$here/../assets"}
mkdir -p "$root/bin"
work=$(mktemp -d "$root/bin/zapret2-build.XXXXXXXX")
trap 'rm -rf -- "$work"' EXIT
cache=${ZAPRET2_DOWNLOAD_DIR:-"$work"}

download() {
    local name=$1 sha=$2 url=$3
    if [[ ! -f "$cache/$name" ]]; then
        curl --retry 3 -fLsS -o "$cache/$name" "$url"
    fi
    printf '%s  %s\n' "$sha" "$cache/$name" | sha256sum -c -
}

download zapret2-v1.0.7.zip 35e14e4e1f345b870c3b87269fa45353efd597241a25304d38375c46b9fd2967 \
    https://github.com/bol-van/zapret2/releases/download/v1.0.7/zapret2-v1.0.7.zip
download wasi.tar.gz b761e3a0721dbae9c09a0059e5fdb2bf917d1b4a8a7b430fb3b5aafb0984b2c4 \
    https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-34/wasi-sdk-34.0-x86_64-linux.tar.gz
download lua.tar.gz 4f18ddae154e793e46eeab727c59ef1c0c0c2b744e7b94219710d76f530629ae \
    https://www.lua.org/ftp/lua-5.4.8.tar.gz
download zlib.tar.gz 9a93b2b7dfdac77ceba5a558a580e74667dd6fede4585b91eefb60f03b72df23 \
    https://github.com/madler/zlib/releases/download/v1.3.1/zlib-1.3.1.tar.gz

unzip -q "$cache/zapret2-v1.0.7.zip" -d "$work"
for archive in wasi lua zlib; do
    tar -xf "$cache/$archive.tar.gz" -C "$work"
done
zapret="$work/zapret2-v1.0.7"
src="$zapret/nfq2"
lua="$work/lua-5.4.8/src"
zlib="$work/zlib-1.3.1"
sdk="$work/wasi-sdk-34.0-x86_64-linux"
patch -d "$src" -p1 --fuzz=0 < "$here/upstream-wasi.patch"
mkdir -p "$work/headers/sys" "$assets/files"
libbsd_include=$(pkg-config --variable=includedir libbsd)
cp "$libbsd_include/bsd/sys/queue.h" "$work/headers/sys/queue.h"
lua_sources=()
for file in "$lua"/l*.c; do
    case "${file##*/}" in
        lua.c|luac.c) ;;
        *) lua_sources+=("$file") ;;
    esac
done

"$sdk/bin/clang" --target=wasm32-wasip1 -O2 -mexec-model=reactor \
    -mllvm -wasm-enable-sjlj -mllvm -wasm-use-legacy-eh=false \
    -D_WASI_EMULATED_SIGNAL -D_WASI_EMULATED_GETPID -D_WASI_EMULATED_PROCESS_CLOCKS \
    -DLIBBSD_OVERLAY -DL_tmpnam=32 -DZAPRET_GH_VER=v1.0.7 -DZAPRET_GH_HASH=038c97e \
    -I"$here/compat" -I"$work/headers" -I"$src" -I"$lua" -I"$zlib" -include wasi.h \
    -Wl,--export=malloc,--export=zapret_init,--export=zapret_input,--export=zapret_output \
    -Wl,--export=zapret_output_length,--export=zapret_process,-z,stack-size=1048576,--strip-debug \
    -o "$work/engine.wasm" \
    "$src"/{checksum,conntrack,darkmagic,desync,filter,gzip,helpers,hostlist,ipset,lua,nfqws,packet_queue,params,pools,protocol,random,timer}.c \
    "$src"/crypto/*.c "$here/compat/host.c" "${lua_sources[@]}" \
    "$zlib"/{adler32,crc32,deflate,inflate,inftrees,inffast,trees,zutil}.c \
    -lwasi-emulated-getpid -lwasi-emulated-signal -lsetjmp -lwasi-emulated-process-clocks

mv "$work/engine.wasm" "$assets/engine.wasm"
cp -R "$zapret/lua" "$assets/"
cp "$zapret/files/fake/quic_initial_www_google_com.bin" "$assets/files/"

if [[ $# == 1 ]]; then
    mkdir -p "$1"
    cp "$zapret/docs/LICENSE.txt" "$1/zapret2.txt"
    cp "$work/lua-5.4.8/doc/readme.html" "$1/lua.html"
    cp "$zlib/LICENSE" "$1/zlib.txt"
    sed '/^ \*\//q' "$work/headers/sys/queue.h" > "$1/queue-h.txt"
fi
