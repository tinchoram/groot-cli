package tool

import (
	"path"
	"regexp"
	"slices"
	"strings"
)

// DestructiveKind names why a command is destructive.
type DestructiveKind string

const (
	DestructiveDelete   DestructiveKind = "delete"   // recursive or broad file removal
	DestructiveGit      DestructiveKind = "git"      // history or work-tree destroyers
	DestructiveDatabase DestructiveKind = "database" // DROP/TRUNCATE/DELETE/FLUSH through a DB client
	DestructiveCluster  DestructiveKind = "cluster"  // kubectl/helm/terraform/docker/cloud CLIs destroying resources
)

const (
	maxReasonRunes  = 80
	maxCommandDepth = 8
)

// ClassifyDestructive reports the first destructive command found in cmd and why.
// It sees through wrappers, treats quoted text and heredoc bodies as data, and
// never reports a read-only command. ok is false when nothing destructive is found.
func ClassifyDestructive(cmd string) (kind DestructiveKind, reason string, ok bool) {
	return classifyCommand(cmd, 0)
}

// classifyCommand classifies a whole command line. Command substitutions run
// before the line is split, so their inner commands are checked too.
func classifyCommand(cmd string, depth int) (DestructiveKind, string, bool) {
	if depth > maxCommandDepth || strings.TrimSpace(cmd) == "" {
		return "", "", false
	}
	text, subs := extractSubstitutions(stripHeredocs(cmd))
	for _, s := range subs {
		if kind, reason, ok := classifyCommand(s, depth+1); ok {
			return kind, reason, ok
		}
	}
	for _, seg := range splitSegments(flattenParens(text)) {
		if kind, reason, ok := classifyTokens(tokenize(seg), depth); ok {
			return kind, reason, ok
		}
	}
	return "", "", false
}

// classifyTokens classifies one simple command, after its wrappers are seen through.
func classifyTokens(toks []string, depth int) (DestructiveKind, string, bool) {
	if depth > maxCommandDepth {
		return "", "", false
	}
	toks = skipLeading(toks)
	if len(toks) == 0 {
		return "", "", false
	}
	prog, args := path.Base(toks[0]), toks[1:]
	switch prog {
	case "sh", "bash", "zsh", "dash", "ksh":
		if s, ok := shellCommandString(args); ok {
			return classifyCommand(s, depth+1)
		}
		return "", "", false
	case "eval":
		return classifyCommand(strings.Join(args, " "), depth+1)
	}
	if rest, ok := wrappedCommand(prog, args); ok {
		return classifyTokens(rest, depth+1)
	}
	kind, ok := classifyLeaf(prog, args)
	if !ok {
		return "", "", false
	}
	return kind, reasonOf(toks), true
}

// classifyLeaf decides a program that runs no other command.
func classifyLeaf(prog string, args []string) (DestructiveKind, bool) {
	switch {
	case prog == "rm":
		return DestructiveDelete, rmDestroys(args)
	case prog == "shred":
		return DestructiveDelete, true
	case prog == "find":
		return DestructiveDelete, findDestroys(args)
	case prog == "git":
		return DestructiveGit, gitDestroys(args)
	case prog == "dropdb":
		return DestructiveDatabase, true
	case prog == "mysqladmin":
		return DestructiveDatabase, hasArgExact(args, "drop")
	case dbClients[prog]:
		return DestructiveDatabase, sqlDestroys(strings.Join(args, " "))
	case prog == "kubectl":
		return DestructiveCluster, kubectlDestroys(args)
	case prog == "helm":
		return DestructiveCluster, helmDestroys(args)
	case prog == "terraform" || prog == "tofu":
		return DestructiveCluster, terraformDestroys(args)
	case prog == "docker":
		return DestructiveCluster, dockerDestroys(args)
	case prog == "docker-compose":
		return DestructiveCluster, composeDown(args)
	case prog == "aws" || prog == "gcloud" || prog == "az":
		return DestructiveCluster, cloudDestroys(args)
	}
	return "", false
}

// dbClients are the database command-line clients whose inline statements are checked.
var dbClients = map[string]bool{
	"psql": true, "mysql": true, "mariadb": true, "sqlite3": true, "mongosh": true,
	"mongo": true, "redis-cli": true, "clickhouse-client": true, "cqlsh": true,
}

// sqlPattern matches the statements that destroy data, as words, in any case.
var sqlPattern = regexp.MustCompile(`(?i)\b(DROP|TRUNCATE|FLUSHALL|FLUSHDB|dropDatabase|deleteMany)\b|\bDELETE\s+FROM\b|\bdrop\(\)`)

func sqlDestroys(text string) bool { return sqlPattern.MatchString(text) }

// broadTargets are the removal targets that take a whole home, system or tree.
var broadTargets = []string{"/", "/*", "~", "~/", "$HOME", "${HOME}", "..", "*", "."}

func rmDestroys(args []string) bool {
	for _, a := range args {
		switch {
		case a == "--recursive":
			return true
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-") && len(a) > 1:
			if strings.ContainsAny(a[1:], "rR") {
				return true
			}
		case slices.Contains(broadTargets, a):
			return true
		}
	}
	return false
}

func findDestroys(args []string) bool {
	if hasArgExact(args, "-delete") {
		return true
	}
	for i := 0; i+1 < len(args); i++ {
		if (args[i] == "-exec" || args[i] == "-execdir") && path.Base(args[i+1]) == "rm" {
			return true
		}
	}
	return false
}

// gitGlobalValueOpts are the git options that take their value as the next word.
var gitGlobalValueOpts = []string{"-C", "-c", "--git-dir", "--work-tree", "--namespace", "--super-prefix", "--config-env"}

// gitSubcommand returns the first word that is not a global option, and the words after it.
func gitSubcommand(args []string) (string, []string, bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return a, args[i+1:], true
		}
		if slices.Contains(gitGlobalValueOpts, a) {
			i++
		}
	}
	return "", nil, false
}

func gitDestroys(args []string) bool {
	sub, rest, ok := gitSubcommand(args)
	if !ok {
		return false
	}
	switch sub {
	case "push":
		return hasFlag(rest, "--force", "--force-with-lease", "--force-if-includes", "--delete") ||
			shortHas(rest, "fd") || hasForcingRefspec(rest)
	case "reset":
		return hasFlag(rest, "--hard")
	case "clean":
		dryRun := hasFlag(rest, "--dry-run") || shortHas(rest, "n")
		return (hasFlag(rest, "--force") || shortHas(rest, "fdx")) && !dryRun
	case "checkout":
		return hasArgExact(rest, ".")
	case "restore":
		return hasArgExact(rest, ".") && !hasFlag(rest, "--staged", "-S")
	case "branch":
		return hasArgExact(rest, "-D") || (hasFlag(rest, "--delete") && hasFlag(rest, "--force", "-f"))
	case "tag":
		return hasFlag(rest, "-d", "--delete")
	case "stash":
		p := positionals(rest)
		return len(p) > 0 && (p[0] == "drop" || p[0] == "clear")
	case "reflog":
		p := positionals(rest)
		return len(p) > 0 && p[0] == "expire"
	case "gc":
		return hasFlag(rest, "--prune=now")
	case "filter-branch", "filter-repo":
		return true
	case "update-ref":
		return hasArgExact(rest, "-d")
	}
	return false
}

// hasForcingRefspec reports a push refspec that deletes (":ref") or forces ("+ref").
func hasForcingRefspec(args []string) bool {
	for _, a := range positionals(args) {
		if strings.HasPrefix(a, ":") || strings.HasPrefix(a, "+") {
			return true
		}
	}
	return false
}

// kubectlValueFlags are the global kubectl options that take a value.
var kubectlValueFlags = []string{"-n", "--namespace", "--context", "--kubeconfig", "-s", "--server",
	"--token", "--cluster", "--user", "--as", "--as-group", "--request-timeout", "-v"}

func kubectlDestroys(args []string) bool {
	rest := skipFlags(args, kubectlValueFlags...)
	pos := positionals(rest)
	if len(pos) == 0 {
		return false
	}
	switch pos[0] {
	case "delete", "drain", "taint", "uncordon":
		return true
	case "replace":
		return hasFlag(rest, "--force")
	case "rollout":
		return len(pos) > 1 && pos[1] == "undo"
	}
	return false
}

func helmDestroys(args []string) bool {
	rest := skipFlags(args, "-n", "--namespace", "--kube-context", "--kubeconfig", "--kube-apiserver", "--kube-token")
	pos := positionals(rest)
	return len(pos) > 0 && slices.Contains([]string{"uninstall", "delete", "rollback"}, pos[0])
}

func terraformDestroys(args []string) bool {
	pos := positionals(args)
	if len(pos) == 0 {
		return false
	}
	switch pos[0] {
	case "destroy":
		return true
	case "apply":
		return hasFlag(args, "-auto-approve", "--auto-approve")
	}
	return false
}

// dockerValueFlags are the global docker options that take a value.
var dockerValueFlags = []string{"-H", "--host", "-c", "--context", "-l", "--log-level", "--config"}

func dockerDestroys(args []string) bool {
	rest := skipFlags(args, dockerValueFlags...)
	pos := positionals(rest)
	if len(pos) == 0 {
		return false
	}
	second := ""
	if len(pos) > 1 {
		second = pos[1]
	}
	switch pos[0] {
	case "rm", "rmi":
		return true
	case "compose":
		return second == "down" && hasFlag(rest, "-v", "--volumes")
	case "system", "image":
		return second == "prune"
	case "volume":
		return second == "rm" || second == "prune"
	case "container":
		return second == "prune" || second == "rm"
	}
	return false
}

func composeDown(args []string) bool {
	pos := positionals(args)
	return len(pos) > 0 && pos[0] == "down" && hasFlag(args, "-v", "--volumes")
}

// cloudDestroys reports whether an aws, gcloud or az command removes resources:
// one of its words, outside an option's value, is a destroying verb.
func cloudDestroys(args []string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			// An option takes the next word as its value unless it is a known switch.
			if !strings.Contains(a, "=") && !slices.Contains(cloudSwitches, a) &&
				i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
			continue
		}
		if a == "delete" || a == "destroy" || a == "purge" || a == "remove" ||
			strings.HasPrefix(a, "delete-") || strings.HasPrefix(a, "terminate-") {
			return true
		}
	}
	return false
}

// cloudSwitches are CLI options that take no value.
var cloudSwitches = []string{"--debug", "--dry-run", "--no-paginate", "--no-cli-pager", "--no-verify-ssl",
	"--no-sign-request", "--quiet", "-q", "--verbose", "--yes", "-y", "--force", "-f", "--help", "-h"}

// wrappedCommand returns the words of the command that a wrapper runs, or false
// when prog is not a wrapper.
func wrappedCommand(prog string, args []string) ([]string, bool) {
	switch prog {
	case "sudo", "doas":
		return skipFlags(args, "-u", "-g", "-C", "-h", "-p", "-r", "-t", "-T", "-U",
			"--user", "--group", "--host", "--prompt", "--role", "--type", "--close-from", "--chdir"), true
	case "env":
		for {
			n := len(args)
			args = skipLeading(skipFlags(args, "-u", "--unset", "-C", "--chdir", "-S", "--split-string"))
			if len(args) == n {
				return args, true
			}
		}
	case "nice":
		return skipFlags(args, "-n", "--adjustment"), true
	case "nohup", "command", "builtin":
		return skipFlags(args), true
	case "time":
		return skipFlags(args, "-f", "--format", "-o", "--output"), true
	case "exec":
		return skipFlags(args, "-a"), true
	case "timeout":
		rest := skipFlags(args, "-s", "--signal", "-k", "--kill-after")
		if len(rest) == 0 {
			return nil, false
		}
		return rest[1:], true // the duration
	case "xargs":
		return skipFlags(args, "-I", "-n", "-P", "-L", "-s", "-d", "-E", "-a",
			"--max-args", "--max-procs", "--max-lines", "--max-chars", "--delimiter", "--arg-file"), true
	}
	return nil, false
}

// shellCommandString returns the string of `sh -c "<string>"`.
func shellCommandString(args []string) (string, bool) {
	for i, a := range args {
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.ContainsRune(a[1:], 'c') {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", false
		}
	}
	return "", false
}

var assignPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// reservedWords are the shell keywords that may precede a simple command.
var reservedWords = []string{"{", "!", "do", "then", "else", "elif", "if", "while", "until"}

// skipLeading drops the VAR=value assignments and the keywords before a command.
func skipLeading(toks []string) []string {
	for len(toks) > 0 && (assignPattern.MatchString(toks[0]) || slices.Contains(reservedWords, toks[0])) {
		toks = toks[1:]
	}
	return toks
}

// skipFlags drops the leading options. An option named in valueFlags takes the next word too.
func skipFlags(args []string, valueFlags ...string) []string {
	for len(args) > 0 {
		a := args[0]
		if a == "--" {
			return args[1:]
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			return args
		}
		args = args[1:]
		if slices.Contains(valueFlags, a) && len(args) > 0 {
			args = args[1:]
		}
	}
	return args
}

// positionals returns the words that are not options.
func positionals(args []string) []string {
	var out []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
}

// hasFlag reports whether one of the names appears as an option, alone or as name=value.
func hasFlag(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n || strings.HasPrefix(a, n+"=") {
				return true
			}
		}
	}
	return false
}

// shortHas reports whether a short option cluster such as -rf contains one of the letters.
func shortHas(args []string, letters string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && len(a) > 1 &&
			strings.ContainsAny(a[1:], letters) {
			return true
		}
	}
	return false
}

func hasArgExact(args []string, v string) bool { return slices.Contains(args, v) }

// reasonOf is the simple command, joined and trimmed for display.
func reasonOf(toks []string) string {
	r := []rune(strings.Join(toks, " "))
	if len(r) > maxReasonRunes {
		return string(r[:maxReasonRunes-3]) + "..."
	}
	return string(r)
}

// tokenize splits one simple command into words. Quotes group words and are
// removed; redirections and their targets are dropped.
func tokenize(seg string) []string {
	var (
		toks []string
		cur  strings.Builder
		has  bool
		drop bool // the next word is a redirection target
		rs   = []rune(seg)
	)
	flush := func() {
		if !has {
			return
		}
		if drop {
			drop = false
		} else {
			toks = append(toks, cur.String())
		}
		cur.Reset()
		has = false
	}
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '\'':
			has = true
			for i++; i < len(rs) && rs[i] != '\''; i++ {
				cur.WriteRune(rs[i])
			}
		case r == '"':
			has = true
			for i++; i < len(rs) && rs[i] != '"'; i++ {
				if rs[i] == '\\' && i+1 < len(rs) && strings.ContainsRune("\"\\$`", rs[i+1]) {
					i++
				}
				cur.WriteRune(rs[i])
			}
		case r == '\\' && i+1 < len(rs):
			has = true
			i++
			cur.WriteRune(rs[i])
		case r == ' ' || r == '\t' || r == '\r':
			flush()
		case r == '<' || r == '>':
			if has && isDigits(cur.String()) {
				cur.Reset()
				has = false
			} else {
				flush()
			}
			for i+1 < len(rs) && strings.ContainsRune("<>&-", rs[i+1]) {
				i++
			}
			drop = true
		default:
			has = true
			cur.WriteRune(r)
		}
	}
	flush()
	return toks
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// heredocOp matches a heredoc operator. The first group is the character before
// it, which keeps here-strings (<<<) out.
var heredocOp = regexp.MustCompile(`(^|[^<])<<(-?)[ \t]*(['"]?)([A-Za-z_][A-Za-z0-9_]*)['"]?`)

// stripHeredocs blanks the bodies of heredocs. The body of a heredoc that feeds
// a database client is kept, as one quoted word, so its statements are checked.
func stripHeredocs(cmd string) string {
	if !strings.Contains(cmd, "<<") {
		return cmd
	}
	lines := strings.Split(cmd, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		locs := heredocOp.FindAllStringSubmatchIndex(line, -1)
		locs = slices.DeleteFunc(locs, func(loc []int) bool { return insideQuotes(line, loc[0]) })
		if len(locs) == 0 {
			out = append(out, line)
			continue
		}
		var (
			kept   strings.Builder
			last   int
			bodies []string
			next   = i + 1
		)
		for _, loc := range locs {
			kept.WriteString(line[last : loc[0]+(loc[3]-loc[2])])
			last = loc[1]
			body, after := heredocBody(lines, next, line[loc[8]:loc[9]], loc[5] > loc[4])
			bodies = append(bodies, body)
			next = after
		}
		kept.WriteString(line[last:])
		text := kept.String()
		if feedsDatabase(line) {
			for _, b := range bodies {
				text += " " + quoteSQL(b)
			}
		}
		out = append(out, text)
		i = next - 1
	}
	return strings.Join(out, "\n")
}

// heredocBody returns the body that starts at lines[from] and ends at the
// delimiter line, and the index of the line after the terminator.
func heredocBody(lines []string, from int, delim string, dash bool) (string, int) {
	var body []string
	for j := from; j < len(lines); j++ {
		check := strings.TrimRight(lines[j], "\r")
		if dash {
			check = strings.TrimLeft(check, "\t")
		}
		if check == delim {
			return strings.Join(body, "\n"), j + 1
		}
		body = append(body, lines[j])
	}
	return strings.Join(body, "\n"), len(lines)
}

// feedsDatabase reports whether a line has a heredoc into a database client.
func feedsDatabase(line string) bool {
	for _, seg := range splitSegments(flattenParens(line)) {
		if !strings.Contains(seg, "<<") {
			continue
		}
		toks := skipLeading(tokenize(seg))
		if len(toks) > 0 && dbClients[path.Base(toks[0])] {
			return true
		}
	}
	return false
}

// quoteSQL makes a heredoc body one single-quoted word. Only its words matter
// to the check, so quotes and newlines become spaces.
func quoteSQL(body string) string {
	flat := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\'' {
			return ' '
		}
		return r
	}, body)
	return "'" + flat + "'"
}

// insideQuotes reports whether byte position pos of line is inside a quoted string.
func insideQuotes(line string, pos int) bool {
	var quote rune
	for i, r := range line {
		if i >= pos {
			break
		}
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		}
	}
	return quote != 0
}

// extractSubstitutions removes $(...) and `...` from the line and returns their
// inner commands. Single-quoted text is left alone.
func extractSubstitutions(s string) (string, []string) {
	var (
		out      strings.Builder
		subs     []string
		rs       = []rune(s)
		inSingle bool
		inDouble bool
	)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case inSingle:
			if r == '\'' {
				inSingle = false
			}
		case r == '\'' && !inDouble:
			inSingle = true
		case r == '"':
			inDouble = !inDouble
		case r == '\\' && i+1 < len(rs):
			out.WriteRune(r)
			i++
			r = rs[i]
		case r == '$' && i+1 < len(rs) && rs[i+1] == '(':
			end := matchingParen(rs, i+1)
			if end < 0 {
				subs = append(subs, string(rs[i+2:]))
				i = len(rs)
			} else {
				subs = append(subs, string(rs[i+2:end]))
				i = end
			}
			out.WriteRune(' ')
			continue
		case r == '`':
			end := -1
			for j := i + 1; j < len(rs); j++ {
				if rs[j] == '`' {
					end = j
					break
				}
			}
			if end < 0 {
				subs = append(subs, string(rs[i+1:]))
				i = len(rs)
			} else {
				subs = append(subs, string(rs[i+1:end]))
				i = end
			}
			out.WriteRune(' ')
			continue
		}
		out.WriteRune(r)
	}
	return out.String(), subs
}

// matchingParen returns the index of the parenthesis that closes the one at open, or -1.
func matchingParen(rs []rune, open int) int {
	depth := 0
	for j := open; j < len(rs); j++ {
		switch rs[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

// flattenParens turns unquoted subshell parentheses into word breaks, so that
// "(cd x && rm -rf y)" splits into its simple commands.
func flattenParens(s string) string {
	var (
		b        strings.Builder
		inSingle bool
		inDouble bool
	)
	for _, r := range s {
		switch {
		case inSingle:
			if r == '\'' {
				inSingle = false
			}
		case inDouble:
			if r == '"' {
				inDouble = false
			}
		case r == '\'':
			inSingle = true
		case r == '"':
			inDouble = true
		case r == '(' || r == ')':
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
