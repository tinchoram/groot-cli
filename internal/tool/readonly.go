package tool

import (
	"slices"
	"strings"
)

// ClassifyShell reports whether a shell command only reads. It returns RiskRead
// only when every pipeline segment runs a program from the read-only allowlist,
// and the command has no redirection, no substitution, and no flag that writes
// or runs another program. Anything it does not recognise is RiskExec.
func ClassifyShell(cmd string) Risk {
	if strings.TrimSpace(cmd) == "" {
		return RiskExec
	}
	// Substitution, process substitution and any redirection are never read-only,
	// even inside quotes: the check is deliberately conservative.
	if strings.Contains(cmd, "$(") || strings.Contains(cmd, "`") ||
		strings.Contains(cmd, "<(") || strings.Contains(cmd, ">") {
		return RiskExec
	}
	for _, seg := range splitSegments(cmd) {
		if strings.TrimSpace(seg) == "" {
			continue
		}
		if !readOnlySegment(seg) {
			return RiskExec
		}
	}
	return RiskRead
}

// splitSegments cuts a command at | || && ; and newlines that are outside quotes.
func splitSegments(cmd string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	runes := []rune(cmd)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if quote != 0 {
			cur.WriteRune(r)
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
			cur.WriteRune(r)
		case '|', '&', ';', '\n':
			if (r == '|' || r == '&') && i+1 < len(runes) && runes[i+1] == r {
				i++
			}
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	return append(out, cur.String())
}

// readOnlySegment checks one simple command.
func readOnlySegment(seg string) bool {
	fields := strings.Fields(seg)
	if len(fields) == 0 {
		return true
	}
	prog, args := fields[0], fields[1:]
	if hasAnyToken(args, "--force", "-f") && prog == "git" {
		return false
	}
	switch prog {
	case "ls", "eza", "cat", "bat", "head", "tail", "less", "wc", "grep", "pwd",
		"echo", "printf", "which", "type", "file", "stat", "du", "df", "uname",
		"whoami", "id", "jq", "yq", "uniq", "cut", "diff", "md5", "shasum", "base64":
		return true
	case "sort", "tree":
		// -o writes its result to a file.
		return !hasAnyToken(args, "-o", "--output")
	case "date":
		// -s sets the clock.
		return !hasAnyToken(args, "-s", "--set")
	case "rg":
		return !hasAnyToken(args, "--pre")
	case "fd":
		return !hasAnyToken(args, "-x", "--exec", "-X", "--exec-batch")
	case "find":
		return !hasAnyToken(args, "-delete", "-exec", "-execdir", "-ok", "-okdir",
			"-fprint", "-fprint0", "-fls", "-fprintf")
	case "env":
		return len(args) == 0
	case "sed":
		return !hasAnyToken(args, "-i", "--in-place") && !hasWriteCommand(args)
	case "awk":
		joined := strings.Join(args, " ")
		return !strings.Contains(joined, "system") && !strings.Contains(joined, "getline") &&
			!hasAnyToken(args, "-i")
	case "git":
		return gitReadOnly(args)
	case "kubectl":
		return kubectlReadOnly(args)
	case "docker":
		return len(args) > 0 && slices.Contains([]string{"ps", "images", "logs", "inspect"}, args[0])
	case "go":
		return len(args) > 0 && slices.Contains([]string{"version", "list"}, args[0]) ||
			len(args) > 0 && args[0] == "env" && !hasAnyToken(args, "-w", "-u")
	case "aws":
		return hasSubcommand(args, "sts", "get-caller-identity") || hasSubcommand(args, "configure", "list")
	case "gcloud":
		return hasSubcommand(args, "config", "list") || hasSubcommand(args, "auth", "list")
	}
	return false
}

// gitReadOnly allows the read subcommands. Branch, tag and remote only read
// without options that change them.
func gitReadOnly(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "status", "log", "show", "rev-parse", "blame", "ls-files", "describe":
		return !hasAnyToken(args, "--output")
	case "diff":
		return !hasAnyToken(args, "--output")
	case "branch":
		return len(args) == 1 || hasAnyToken(args[1:], "-a", "--all", "-r", "--remotes",
			"-l", "--list", "--show-current", "-v", "-vv", "--verbose")
	case "tag":
		return len(args) == 1 || hasAnyToken(args[1:], "-l", "--list")
	case "remote":
		return len(args) == 1 || (len(args) == 2 && args[1] == "-v")
	}
	return false
}

// kubectlReadOnly allows read verbs and the read-only config commands.
func kubectlReadOnly(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "get", "describe", "logs", "top", "version", "api-resources", "explain":
		return true
	case "config":
		return len(args) > 1 && slices.Contains([]string{"current-context", "get-contexts", "view"}, args[1])
	}
	return false
}

// hasSubcommand reports whether args start with the given two words.
func hasSubcommand(args []string, first, second string) bool {
	return len(args) >= 2 && args[0] == first && args[1] == second
}

// hasWriteCommand reports whether a sed script contains a write (w) or execute
// (e) command, which changes files or runs programs.
func hasWriteCommand(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		for _, part := range strings.Split(a, ";") {
			p := strings.TrimSpace(part)
			if strings.HasPrefix(p, "w ") || strings.HasPrefix(p, "e ") || p == "w" || p == "e" {
				return true
			}
		}
	}
	return false
}

func hasAnyToken(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n || strings.HasPrefix(a, n+"=") {
				return true
			}
		}
	}
	return false
}
