#!/bin/bash

if [ x"$ENABLE_SSHD" != x"true" ]; then
    exit 0
fi

# Don't run these steps again if we have already generated settings.

if [ -f /opt/sshd/sshd_config ]; then
    exit 0
fi

# Generate sshd settings.

chmod g-w $HOME

mkdir -p /opt/sshd

ssh-keygen -q -N "" -t rsa -b 4096 -f /opt/sshd/ssh_host_rsa_key
ssh-keygen -q -N "" -t ecdsa -f /opt/sshd/ssh_host_ecdsa_key
ssh-keygen -q -N "" -t ed25519 -f /opt/sshd/ssh_host_ed25519_key

cat > /opt/sshd/sshd_config << EOF
Port 2022
HostKey /opt/sshd/ssh_host_rsa_key
HostKey /opt/sshd/ssh_host_ecdsa_key
HostKey /opt/sshd/ssh_host_ed25519_key
LogLevel INFO
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
AuthenticationMethods publickey
AllowTcpForwarding local
AllowAgentForwarding no
X11Forwarding no
PrintMotd no
PidFile /opt/sshd/sshd.pid
Subsystem sftp internal-sftp
EOF

chmod 600 /opt/sshd/*
