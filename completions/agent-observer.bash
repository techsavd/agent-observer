_agent_observer()
{
	local cur prev words cword
	_init_completion || return

	local commands="watch doctor"
	local flags="--providers --claude-dir --codex-dir --cursor-dir --plugins-dir --tasks-dir --teams-dir --max-file-size --refresh-interval --poll-interval --no-watch --debug --dump-json --dump-text --diagnostics --version --shell --no-shell --act --no-act --redact --log-file --log-level --telemetry --telemetry-endpoint --focus --help"

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
		--providers)
			COMPREPLY=($(compgen -W "claude codex cursor plugins" -- "$cur"))
			return
			;;
		--tasks-dir|--teams-dir|--claude-dir|--codex-dir|--cursor-dir|--plugins-dir|--log-file)
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
