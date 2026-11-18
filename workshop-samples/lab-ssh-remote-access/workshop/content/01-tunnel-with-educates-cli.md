---
title: Connecting with the Educates CLI
---

The Educates CLI provides an experimental `tunnel connect` command that acts as
an SSH `ProxyCommand`, bridging an SSH connection over the websocket tunnel. A
setup script downloaded the CLI for this session's architecture from the
Educates GitHub releases into `~/bin`, which is on the session PATH, so it is
available simply as `educates`.

Confirm it runs and that the `tunnel connect` command is available:

```terminal:execute
command: educates tunnel connect --help
```

## Configure SSH

Set up an SSH client configuration with one host alias per tunnel client. Each
alias targets this same session through the tunnel, differing only in the
`ProxyCommand` used. The tunnel's URL is a `wss://` URL when the session is
served over HTTPS, and a `ws://` URL when it is served over plain HTTP, so the
command takes the scheme from the session's ingress protocol. Write the
configuration now:

```terminal:execute
command: |-
  mkdir -p ~/.ssh
  ingress_protocol={{< param ingress_protocol >}}
  tunnel_url="${ingress_protocol/http/ws}://%h/tunnel/"
  cat > ~/.ssh/config <<EOF
  Host educates-cli
    HostName {{< param session_name >}}.{{< param ingress_domain >}}
    User eduk8s
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
    LogLevel ERROR
    IdentitiesOnly yes
    IdentityFile ~/.ssh/id_rsa
    ProxyCommand educates tunnel connect --url $tunnel_url
  Host python-client
    HostName {{< param session_name >}}.{{< param ingress_domain >}}
    User eduk8s
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
    LogLevel ERROR
    IdentitiesOnly yes
    IdentityFile ~/.ssh/id_rsa
    ProxyCommand python3 ~/exercises/python-client/tunnel.py $tunnel_url
  EOF
  chmod 600 ~/.ssh/config
```

You can view the configuration that was written:

```editor:open-file
file: ~/.ssh/config
```

## Connect through the tunnel

Connect back into this session through the tunnel using the `educates-cli`
alias, running a few commands on the far end to confirm it is a genuine SSH
session reached over the tunnel:

```terminal:execute
command: ssh educates-cli 'echo "Connected over the tunnel as $(whoami) on $(uname -n)"; echo "SSH_CONNECTION=$SSH_CONNECTION"'
```

If the tunnel is working you will see the workshop user and the session pod
hostname reported, along with an `SSH_CONNECTION` value — which is only set
inside a real SSH session — confirming the connection was established over the
websocket tunnelling proxy rather than run locally.
