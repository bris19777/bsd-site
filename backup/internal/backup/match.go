package backup

import (
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// DefaultExcludes skips files that are never worth backing up on Windows.
var DefaultExcludes = []string{
	"pagefile.sys", "hiberfil.sys", "swapfile.sys", "DumpStack.log.tmp",
	"**/$Recycle.Bin/**", "**/System Volume Information/**",
	"**/AppData/Local/Temp/**", "**/AppData/Local/Microsoft/Windows/INetCache/**",
	"*.tmp", "~$*", "Thumbs.db",
}

// Matcher tests paths against glob patterns. "*" matches a run of
// characters other than '/', "?" one such character, and "**" anything
// including '/'. A pattern without '/' matches the base name anywhere
// ("*.tmp"); one with '/' matches the whole path
// ("C:/Users/*/Downloads/**"). Paths are compared with forward slashes,
// case-insensitively on Windows.
type Matcher struct {
	base []*regexp.Regexp
	full []*regexp.Regexp
}

func NewMatcher(patterns []string) *Matcher {
	m := &Matcher{}
	for _, p := range patterns {
		p = strings.TrimSpace(filepath.ToSlash(p))
		if p == "" {
			continue
		}
		re := regexp.MustCompile(globToRegexp(p))
		if strings.Contains(p, "/") {
			m.full = append(m.full, re)
		} else {
			m.base = append(m.base, re)
		}
	}
	return m
}

func (m *Matcher) Empty() bool { return len(m.base) == 0 && len(m.full) == 0 }

// Match reports whether a file path matches.
func (m *Matcher) Match(path string) bool {
	p := normPath(path)
	base := p[strings.LastIndexByte(p, '/')+1:]
	for _, re := range m.base {
		if re.MatchString(base) {
			return true
		}
	}
	for _, re := range m.full {
		if re.MatchString(p) {
			return true
		}
	}
	return false
}

// MatchDir reports whether a directory (and so everything below it) matches.
func (m *Matcher) MatchDir(path string) bool {
	return m.Match(path) || m.Match(strings.TrimSuffix(path, string(filepath.Separator))+"/")
}

func normPath(p string) string {
	p = filepath.ToSlash(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

func globToRegexp(p string) string {
	var b strings.Builder
	b.WriteString("^")
	if runtime.GOOS == "windows" {
		b.WriteString("(?i)")
	}
	for i := 0; i < len(p); i++ {
		switch c := p[i]; c {
		case '*':
			if i+1 < len(p) && p[i+1] == '*' {
				i++
				// "**/" also matches zero directories.
				if i+1 < len(p) && p[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return b.String()
}
