#!/usr/bin/env bash
# Scan the container images the educates CLI runs as shared host services
# for vulnerabilities that have a fix, and print a Markdown report.
#
#   hack/scan-host-images.sh                  every image hack/list-host-images.sh lists
#   hack/scan-host-images.sh <reference>...   the images given, such as a candidate pin
#
# It runs Grype from its container image, pinned by digest, and Grype
# reads each image from its registry, so no image is pulled or run and
# the Docker socket stays out of the scanner's reach. The report lists the
# fixable vulnerabilities of every severity; the threshold decides the
# exit status:
#
#   0  no image has a fixable vulnerability at or above SEVERITY
#   1  an image has one
#   2  an image could not be scanned
#
# Environment:
#   SEVERITY          threshold: negligible, low, medium, high (default) or critical
#   PLATFORM          platform of the image to scan (default linux/amd64)
#   GRYPE_CACHE_DIR   folder to keep Grype's vulnerability database in between
#                     runs (default: a temporary folder, removed at the end)
#
# Requires Docker. To move Grype to a newer release, pin its new tag with
# the index digest `docker buildx imagetools inspect` prints for it.
set -euo pipefail
cd "$(dirname "$0")/.."

GRYPE_IMAGE=ghcr.io/anchore/grype:v0.122.1@sha256:661583704a253c0448ed63998b5ea2cd9d5a76ee71eedc4aed439b8ce3e93a1c
SEVERITY=${SEVERITY:-high}
PLATFORM=${PLATFORM:-linux/amd64}

cache=${GRYPE_CACHE_DIR:-}
if [ -z "$cache" ]; then
  cache=$(mktemp -d)
  trap 'rm -rf "$cache"' EXIT
fi
mkdir -p "$cache/db" "$cache/tmp"

# Each target is "<label> <reference>".
targets=()
if [ $# -eq 0 ]; then
  listed=$(hack/list-host-images.sh)
  while read -r service _ _ reference; do
    targets+=("${service} ${reference}")
  done <<<"$listed"
else
  for reference in "$@"; do
    targets+=("${reference} ${reference}")
  done
fi

echo "# Fixable vulnerabilities in the CLI's host container images"
echo
echo "Scanned $(date -u +%Y-%m-%d) for ${PLATFORM} with \`${GRYPE_IMAGE%%@*}\`."
echo "Each table lists the vulnerabilities that have a fix, of every severity;"
echo "an image is flagged when one of them is ${SEVERITY} or above."
echo "\`hack/list-host-images.sh\` says where each image is pinned, and the"
echo "\`educates-upgrade-host-images\` skill moves a pin to a fixed image."

status=0
flagged=()
failed=()
for target in "${targets[@]}"; do
  read -r label reference <<<"$target"
  echo
  echo "## ${label}"
  echo
  echo "\`${reference}\`"
  echo
  set +e
  # Grype runs as the caller, so that nothing it writes to the cache is
  # left owned by root, with its home and temporary files in the cache:
  # only root can write to its image's /tmp. Its table goes to standard
  # output, and its log, shown only when the scan fails, to a file.
  output=$(docker run --rm \
    --user "$(id -u):$(id -g)" \
    --env HOME=/cache \
    --env TMPDIR=/cache/tmp \
    --env GRYPE_DB_CACHE_DIR=/cache/db \
    --volume "${cache}:/cache" \
    "$GRYPE_IMAGE" "registry:${reference}" \
    --platform "$PLATFORM" --only-fixed --fail-on "$SEVERITY" --output table \
    2>"${cache}/tmp/grype.log")
  result=$?
  set -e
  case $result in
    0 | 2)
      if [ -z "$output" ] || [ "$output" = "No vulnerabilities found" ]; then
        echo "No fixable vulnerabilities."
      else
        echo '```text'
        echo "$output"
        echo '```'
      fi
      if [ $result -eq 2 ]; then
        flagged+=("$label")
        [ $status -eq 0 ] && status=1
      fi
      ;;
    *)
      echo "The scan failed:"
      echo
      echo '```text'
      grep -E 'ERROR|WARN|^[[:space:]]+[-*] ' "${cache}/tmp/grype.log" || cat "${cache}/tmp/grype.log"
      echo '```'
      failed+=("$label")
      status=2
      ;;
  esac
done

echo
echo "## Summary"
echo
if [ ${#failed[@]} -gt 0 ]; then
  echo "Could not scan: ${failed[*]}."
fi
if [ ${#flagged[@]} -gt 0 ]; then
  echo "Fixable vulnerabilities at ${SEVERITY} or above: ${flagged[*]}."
elif [ ${#failed[@]} -eq 0 ]; then
  echo "No image has a fixable vulnerability at ${SEVERITY} or above."
fi

exit $status
