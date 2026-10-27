package bgp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Template is a command line parsed once at startup into an argv. It is run
// with exec, never through a shell: placeholder values are substituted inside
// individual arguments, so no value can ever add an argument, a pipe or a
// second command. Quoting follows POSIX sh for the cases templates use:
// 'single quotes' are literal, "double quotes" honour \" and \\, a backslash
// outside quotes escapes the next character.
type Template struct {
	raw  string
	args []string
}

// knownPlaceholders are the tokens a template may contain. Anything else in
// braces is a configuration error (a typo would otherwise reach the daemon
// literally — the old docs used an unsupported {peer_as}).
var knownPlaceholders = map[string]bool{
	"ip": true, "prefix_len": true, "prefix": true, "family": true,
	"community": true, "communities": true, "next_hop": true,
	"peer_address": true, "peer_as": true, "local_as": true, "action": true,
}

var placeholderRe = regexp.MustCompile(`\{([a-z_]+)\}`)

// shellOperators are unquoted tokens that only mean something to a shell.
// Since templates are no longer run through "sh -c", a template using them
// would silently pass them as arguments; refuse it at startup instead.
var shellOperators = map[string]bool{"|": true, "||": true, "&&": true, ";": true, "&": true, ">": true, ">>": true, "<": true, "2>&1": true}

// ParseTemplate splits a command template into arguments.
func ParseTemplate(raw string) (Template, error) {
	args, err := splitArgs(raw)
	if err != nil {
		return Template{}, err
	}
	if len(args) == 0 {
		return Template{}, errors.New("empty command")
	}
	for _, a := range args {
		for _, m := range placeholderRe.FindAllStringSubmatch(a, -1) {
			if !knownPlaceholders[m[1]] {
				return Template{}, fmt.Errorf("unknown placeholder {%s} in %q", m[1], raw)
			}
		}
	}
	if strings.ContainsAny(args[0], "{}") {
		return Template{}, fmt.Errorf("the program name cannot be a placeholder in %q", raw)
	}
	return Template{raw: raw, args: args}, nil
}

// IsZero reports whether the template is unset.
func (t Template) IsZero() bool { return len(t.args) == 0 }

// String returns the template as configured.
func (t Template) String() string { return t.raw }

// UsesPlaceholder reports whether any argument contains {name}.
func (t Template) UsesPlaceholder(name string) bool {
	tok := "{" + name + "}"
	for _, a := range t.args {
		if strings.Contains(a, tok) {
			return true
		}
	}
	return false
}

// Expand substitutes placeholders argument by argument.
func (t Template) Expand(vars map[string]string) []string {
	out := make([]string, len(t.args))
	for i, a := range t.args {
		out[i] = placeholderRe.ReplaceAllStringFunc(a, func(m string) string {
			return vars[m[1:len(m)-1]]
		})
	}
	return out
}

func splitArgs(s string) ([]string, error) {
	var (
		args    []string
		cur     strings.Builder
		inArg   bool
		quoted  bool // current arg had quotes (so "" is a real empty arg)
		meta    bool // current arg has an unquoted shell metacharacter
		inS     bool
		inD     bool
		escaped bool
	)
	flush := func() error {
		if inArg {
			a := cur.String()
			if (!quoted && shellOperators[a]) || meta {
				return fmt.Errorf("shell operator in %q: templates run without a shell — quote it, put pipelines in a script, or call sh -c explicitly", a)
			}
			args = append(args, a)
		}
		cur.Reset()
		inArg, quoted, meta = false, false, false
		return nil
	}
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case inS:
			if r == '\'' {
				inS = false
			} else {
				cur.WriteRune(r)
			}
		case inD:
			switch r {
			case '"':
				inD = false
			case '\\':
				escaped = true
			default:
				cur.WriteRune(r)
			}
		case r == '\\':
			escaped, inArg = true, true
		case r == '\'':
			inS, inArg, quoted = true, true, true
		case r == '"':
			inD, inArg, quoted = true, true, true
		case r == ' ' || r == '\t' || r == '\n':
			if err := flush(); err != nil {
				return nil, err
			}
		default:
			if strings.ContainsRune(";|&<>`$", r) {
				meta = true
			}
			cur.WriteRune(r)
			inArg = true
		}
	}
	if inS || inD || escaped {
		return nil, fmt.Errorf("unbalanced quote or trailing backslash in %q", s)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return args, nil
}

// maxCommandOutput bounds what a daemon CLI can make us buffer.
const maxCommandOutput = 64 << 10

// limitedBuffer keeps the first maxCommandOutput bytes and drops the rest.
type limitedBuffer struct {
	buf       bytes.Buffer
	truncated bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := maxCommandOutput - l.buf.Len(); room > 0 {
		if len(p) > room {
			l.buf.Write(p[:room])
			l.truncated = true
		} else {
			l.buf.Write(p)
		}
	} else {
		l.truncated = true
	}
	return len(p), nil
}

// CommandError is a failed daemon command. Its Error() is safe to log; Short
// is safe to return to API clients (no command output, which may echo
// configuration).
type CommandError struct {
	Argv   []string
	Err    error
	Output string
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("command %q failed: %v (output: %s)", strings.Join(e.Argv, " "), e.Err, e.Output)
}

// Short describes the failure without the command output.
func (e *CommandError) Short() string {
	name := ""
	if len(e.Argv) > 0 {
		name = e.Argv[0]
	}
	if errors.Is(e.Err, context.DeadlineExceeded) {
		return name + " timed out"
	}
	return fmt.Sprintf("%s failed: %v", name, e.Err)
}

func (e *CommandError) Unwrap() error { return e.Err }

// commandPath is the PATH given to daemon commands. The API's own environment
// (ClickHouse password, OIDC secret, BGP_API_TOKEN…) is never inherited.
func commandPath() string {
	if p := os.Getenv("PATH"); p != "" {
		return p
	}
	return "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
}

// runCommand executes argv with a minimal environment and a hard timeout.
// WaitDelay makes sure a child that keeps the output pipe open (a wrapper
// script whose daemon CLI hangs) cannot block us past the timeout.
func runCommand(ctx context.Context, timeout time.Duration, argv []string, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append([]string{"PATH=" + commandPath(), "LANG=C", "LC_ALL=C"}, env...)
	cmd.WaitDelay = 2 * time.Second
	var out limitedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	text := strings.TrimSpace(out.buf.String())
	if err != nil {
		if ctx.Err() != nil {
			err = fmt.Errorf("%w after %s", context.DeadlineExceeded, timeout)
		}
		return text, &CommandError{Argv: argv, Err: err, Output: truncate(text, 500)}
	}
	return text, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
