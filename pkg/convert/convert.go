package convert

import (
	"regexp"
	"strings"
)

// This is a go port of github.com/FokkeZB/J2M

func ToJira(markdown string) (out string) {
	out = markdown

	// remove html comments
	var comment = regexp.MustCompile(`(?s:<!--.*?-->)`)
	out = comment.ReplaceAllString(out, "")

	// multi-line comments
	var multiLineCode = regexp.MustCompile("(?s:`{3}([a-z-]+)?(.*?)`{3})")
	out = multiLineCode.ReplaceAllString(out, "{code:$1}$2{code}")

	// remove unknown `release-note` code block type
	out = strings.Replace(out, "{code:release-note}", "{code}", -1)
	// fix empty syntax blocks
	out = strings.Replace(out, "{code:}", "{code}", -1)

	// bold
	var bold = regexp.MustCompile(`(?s:\*{2}(.*?)\*{2})`)
	out = bold.ReplaceAllString(out, "*$1*")

	return out
}

func ToMD(jira string) string {
	return jira
}
