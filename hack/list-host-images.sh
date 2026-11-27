#!/usr/bin/env bash
# List the container images the educates CLI runs as shared host services
# on a developer's Docker, one line each:
#
#   <service> <file> <constant> <image reference>
#
#   registry  the local image registry and the registry mirrors
#   resolver  the local DNS resolver
#   router    the router that serves every local cluster
#
# The images are pinned as Go constants, which no dependency bot reads.
# hack/scan-host-images.sh, the weekly host images scan workflow and the
# educates-upgrade-host-images skill take the list from here, so a host
# image the CLI gains is added once, below. A constant that can't be read
# fails the script, so renaming one can't drop its image from the scan.
set -euo pipefail
cd "$(dirname "$0")/.."

PINS=(
  "registry client-programs/pkg/registry/registry.go RegistryImageV3"
  "resolver client-programs/pkg/resolver/resolver.go dnsmasqImage"
  "router client-programs/pkg/router/router.go Image"
)

for pin in "${PINS[@]}"; do
  read -r service file constant <<<"$pin"
  references=$(sed -n -E "s/^[[:space:]]*(const[[:space:]]+)?${constant}[[:space:]]*=[[:space:]]*\"([^\"]+)\".*/\2/p" "$file")
  if [ -z "$references" ] || [ "$(printf '%s\n' "$references" | wc -l)" -ne 1 ]; then
    echo "list-host-images: cannot read the image of ${service} from ${constant} in ${file}" >&2
    exit 1
  fi
  printf '%s %s %s %s\n' "$service" "$file" "$constant" "$references"
done
