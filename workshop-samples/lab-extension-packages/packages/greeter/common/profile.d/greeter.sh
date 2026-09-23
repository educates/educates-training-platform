# Sourced into every shell in the workshop session. Profile scripts are named
# with a .sh suffix but do not need the execute bit, since they are sourced
# rather than run.
#
# Note there is no PATH line here. The package's bin directory is added to the
# PATH as a matter of convention, so a package does not set it up itself.

export GREETER_CONFIG="$HOME/.local/share/greeter/config"
