_agent_observer()
{
	local cur prev words cword
	_init_completion || return

	local commands="watch doctor"
	local flags="--tasks-dir --teams-dir --max-file-size --refresh-interval --debug --dump-json --dump-text --diagnostics --version --shell --no-shell --redact --log-file --log-level --telemetry --telemetry-endpoint --focus --help"

	case "$prev" in
		--log-level)
			COMPREPLY=($(compgen -W "debug info warn error" -- "$cur"))
			return
			;;
		--focus)
			COMPREPLY=($(compgen -W "all active blocked warnings" -- "$cur"))
			return
			;;
		--telemetry)
			COMPREPLY=($(compgen -W "off on" -- "$cur"))
			return
			;;
		--tasks-dir|--teams-dir|--log-file)
			_filedir
			return
			;;
	esac

	if [[ $cword -eq 1 ]]; then
		COMPREPLY=($(compgen -W "$commands $flags" -- "$cur"))
	else
		COMPREPLY=($(compgen -W "$flags" -- "$cur"))
	fi
}

complete -F _agent_observer agent-observer
