package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Pref is one preference to write back: a key the session changed and its value now.
type Pref struct{ Key, Val string }

// header opens a user file this write creates.
const header = `# ting — your own settings, and the file ting writes a changed one back to.
# Hand-edit it freely: comments and layout are preserved, only values move.
# Precedence: flag > environment > THIS FILE > the shipped <checkout>/config.
`

// writable is whether a value survives the reader as itself: no comment mark, no quote the
// reader would strip, no leading ~/ it would expand, no edge whitespace it would trim.
func writable(v string) bool {
	return v != "" && !strings.ContainsAny(v, "#\"'\n") && !strings.HasPrefix(v, "~/") &&
		strings.TrimFunc(v, isSpace) == v
}

// Rewrite moves each pref's value in place in a config file's text, line by line and with
// no config library (they drop comments between decode and encode): the first assignment of
// a key keeps its own spacing and any comment after the value; a key the file does not have
// yet goes at the end. body is "" with exists false for a file that is not there yet.
func Rewrite(body string, exists bool, prefs []Pref) string {
	var out strings.Builder
	done := map[string]bool{}
	want := map[string]string{}
	for _, p := range prefs {
		if writable(p.Val) {
			want[p.Key] = p.Val
		}
	}
	if !exists {
		out.WriteString(header)
	} else {
		lines := strings.Split(body, "\n")
		if strings.HasSuffix(body, "\n") {
			lines = lines[:len(lines)-1]
		}
		for _, line := range lines {
			if i := strings.IndexByte(line, '='); i >= 0 {
				lhs, rhs := line[:i], line[i+1:]
				key := strings.TrimFunc(lhs, isSpace)
				if v, ok := want[key]; ok && !done[key] {
					pre, comment := rhs, ""
					if j := strings.IndexByte(rhs, '#'); j >= 0 {
						pre, comment = rhs[:j], rhs[j:]
					}
					trimmed := strings.TrimRightFunc(pre, isSpace)
					gap := pre[len(trimmed):]
					bare := strings.TrimLeftFunc(trimmed, isSpace)
					lead := trimmed[:len(trimmed)-len(bare)]
					line = lhs + "=" + lead + v + gap + comment
					done[key] = true
				}
			}
			out.WriteString(line + "\n")
		}
	}
	for _, p := range prefs {
		if v, ok := want[p.Key]; ok && !done[p.Key] {
			out.WriteString(p.Key + "=" + v + "\n")
			done[p.Key] = true
		}
	}
	return out.String()
}

// WriteBack rewrites the user's file with prefs, less the keys the environment pins. It
// writes through a symlink to the file it names, to a temporary beside it that takes the
// original's mode, then renames — a crash leaves the old file or the new one, never half.
func (c *Config) WriteBack(prefs []Pref) error {
	var keep []Pref
	for _, p := range prefs {
		if !c.Pinned(p.Key) {
			keep = append(keep, p)
		}
	}
	if len(keep) == 0 {
		return nil
	}
	dest := c.UserPath
	if r, err := filepath.EvalSymlinks(dest); err == nil {
		dest = r
	}
	mode := os.FileMode(0o644)
	body, err := os.ReadFile(dest)
	exists := err == nil
	switch {
	case exists:
		if st, err := os.Stat(dest); err == nil {
			mode = st.Mode().Perm()
		}
	case os.IsNotExist(err):
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
	default:
		return err
	}
	tmp := dest + ".tmp." + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(tmp, []byte(Rewrite(string(body), exists, keep)), mode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
