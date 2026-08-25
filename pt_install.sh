#!/bin/bash
#From https://github.com/oneclickvirt/pingtest
#2024.06.29

rm -rf /usr/bin/pt
rm -rf pt
os=$(uname -s)
arch=$(uname -m)

# Bootstrap Cloudflare DoH with fixed public addresses when this host is online
# but cannot use its configured system resolver. Normal curl and wget paths stay
# first so existing installations keep their current behavior.
DOH_URL="https://cloudflare-dns.com/dns-query"
DOH_RESOLVE="cloudflare-dns.com:443:1.1.1.1,1.0.0.1"

curl_supports_doh() {
  command -v curl >/dev/null 2>&1 && curl --help all 2>/dev/null | grep -q -- "--doh-url"
}

curl_fetch() {
  if curl "$@"; then
    return 0
  fi
  if curl_supports_doh; then
    curl --doh-url "$DOH_URL" --resolve "$DOH_RESOLVE" "$@"
    return $?
  fi
  return 1
}

download_file() {
  local output=$1
  local url=$2
  if command -v wget >/dev/null 2>&1; then
    if wget -O "$output" "$url"; then
      return 0
    fi
    echo "wget download failed, falling back to curl"
  fi
  if command -v curl >/dev/null 2>&1 && curl_fetch -fL -o "$output" "$url"; then
    return 0
  fi
  echo "Unable to download $url" >&2
  return 1
}

check_cdn() {
  local o_url=$1
  for cdn_url in "${cdn_urls[@]}"; do
    if curl_fetch -sL -k "$cdn_url$o_url" --max-time 6 2>/dev/null | grep -q "success" >/dev/null 2>&1; then
      export cdn_success_url="$cdn_url"
      return
    fi
    sleep 0.5
  done
  export cdn_success_url=""
}

check_cdn_file() {
  check_cdn "https://raw.githubusercontent.com/spiritLHLS/ecs/main/back/test"
  if [ -n "$cdn_success_url" ]; then
    echo "CDN available, using CDN"
  else
    echo "No CDN available, no use CDN"
  fi
}

cdn_urls=("https://cdn0.spiritlhl.top/" "http://cdn3.spiritlhl.net/" "http://cdn1.spiritlhl.net/" "http://cdn2.spiritlhl.net/")
check_cdn_file

case $os in
Linux)
  case $arch in
  "x86_64" | "x86" | "amd64" | "x64")
    download_file pt "${cdn_success_url}https://github.com/oneclickvirt/pingtest/releases/download/output/pingtest-linux-amd64" || exit 1
    ;;
  "i386" | "i686")
    download_file pt "${cdn_success_url}https://github.com/oneclickvirt/pingtest/releases/download/output/pingtest-linux-386" || exit 1
    ;;
  "armv7l" | "armv8" | "armv8l" | "aarch64" | "arm64")
    download_file pt "${cdn_success_url}https://github.com/oneclickvirt/pingtest/releases/download/output/pingtest-linux-arm64" || exit 1
    ;;
  *)
    echo "Unsupported architecture: $arch"
    exit 1
    ;;
  esac
  ;;
Darwin)
  case $arch in
  "x86_64" | "x86" | "amd64" | "x64")
    download_file pt "${cdn_success_url}https://github.com/oneclickvirt/pingtest/releases/download/output/pingtest-darwin-amd64" || exit 1
    ;;
  "i386" | "i686")
    download_file pt "${cdn_success_url}https://github.com/oneclickvirt/pingtest/releases/download/output/pingtest-darwin-386" || exit 1
    ;;
  "armv7l" | "armv8" | "armv8l" | "aarch64" | "arm64")
    download_file pt "${cdn_success_url}https://github.com/oneclickvirt/pingtest/releases/download/output/pingtest-darwin-arm64" || exit 1
    ;;
  *)
    echo "Unsupported architecture: $arch"
    exit 1
    ;;
  esac
  ;;
FreeBSD)
  case $arch in
  amd64)
    download_file pt "${cdn_success_url}https://github.com/oneclickvirt/pingtest/releases/download/output/pingtest-freebsd-amd64" || exit 1
    ;;
  "i386" | "i686")
    download_file pt "${cdn_success_url}https://github.com/oneclickvirt/pingtest/releases/download/output/pingtest-freebsd-386" || exit 1
    ;;
  "armv7l" | "armv8" | "armv8l" | "aarch64" | "arm64")
    download_file pt "${cdn_success_url}https://github.com/oneclickvirt/pingtest/releases/download/output/pingtest-freebsd-arm64" || exit 1
    ;;
  *)
    echo "Unsupported architecture: $arch"
    exit 1
    ;;
  esac
  ;;
*)
  echo "Unsupported operating system: $os"
  exit 1
  ;;
esac

chmod 777 pt
PARAM="net.ipv4.ping_group_range"
NEW_VALUE="0 2147483647"
CURRENT_VALUE=$(sysctl -n "$PARAM" 2>/dev/null)
if [ -f /etc/sysctl.conf ] && [ "$CURRENT_VALUE" != "$NEW_VALUE" ]; then
    if grep -q "^$PARAM" /etc/sysctl.conf; then
        sudo sed -i "s/^$PARAM.*/$PARAM = $NEW_VALUE/" /etc/sysctl.conf
    else
        echo "$PARAM = $NEW_VALUE" | sudo tee -a /etc/sysctl.conf
    fi
    sudo sysctl -p
fi
setcap cap_net_raw=+ep pt
cp pt /usr/bin/pt
setcap cap_net_raw=+ep /usr/bin/pt
