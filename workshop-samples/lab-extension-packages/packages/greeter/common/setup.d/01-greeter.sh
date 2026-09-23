#!/bin/sh
#
# Runs once when the workshop session starts. Setup scripts must carry the
# execute bit and be named with a .sh suffix, or they are skipped.
#
# The package itself may be mounted read only, so anything generated at
# startup is written somewhere writable rather than back into the package.

set -e

mkdir -p "$HOME/.local/share/greeter"

cat > "$HOME/.local/share/greeter/config" <<EOF
# Written by the greeter package's setup script when the session started.
greeting=Hello
subject=workshop
EOF

echo "greeter: wrote $HOME/.local/share/greeter/config"

# The package's own bin directory is already on the PATH by the time setup
# scripts run, so a setup script can use what its package ships.
echo "greeter: the packaged program reports $(greeter --which)"
