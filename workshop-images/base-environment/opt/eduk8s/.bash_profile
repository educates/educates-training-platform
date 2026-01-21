# Set prompt to working directory.

export PS1="[\w] $ "

# Set default editor for tools that need it.

export EDITOR=/usr/bin/vim

# Enable kubectl bash completion.

source <(kubectl completion bash)

alias k=kubectl

complete -F __start_kubectl k

alias watch="watch "

# Source profile provided for the workshop.

if [ -f /opt/eduk8s/workshop/profile ]; then
    source /opt/eduk8s/workshop/profile
fi

if [ -f /opt/workshop/profile ]; then
    source /opt/workshop/profile
fi

if [ -f $HOME/workshop/profile ]; then
    source $HOME/workshop/profile
fi

# Source $HOME/.bashrc profile in case used.

if [ -f $HOME/.bashrc ]; then
    source $HOME/.bashrc
fi
