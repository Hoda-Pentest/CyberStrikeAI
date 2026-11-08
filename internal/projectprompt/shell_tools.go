package projectprompt

// ShellExecExecuteGuidanceSection is appended to the single/multi-agent system prompt: the division of labor between exec and execute (kept as short as possible).
func ShellExecExecuteGuidanceSection() string {
	return `Shell (exec/execute): prefer a dedicated MCP tool when one exists; use exec for system commands (pipes, workdir, background &); use execute for scripts under skills/ (together with read_file, skill); split multi-step scans into separate calls, and do not chain multiple scanners in one shell command. Long scripts, request bodies, or payloads must first be written to the session working directory with write_file, then run as short commands via exec/execute; do not embed long content in command. Downloads/temporary files must be written to the \"session working directory\" named in the system prompt; do not use /tmp.`
}

// ShellExecExecuteGuidanceReconSuffix is an optional one-line addition for the reconnaissance sub-agent.
func ShellExecExecuteGuidanceReconSuffix() string {
	return `For enumeration, prefer dedicated MCP tools such as subfinder and amass; do not assemble long chains with exec/execute.`
}
