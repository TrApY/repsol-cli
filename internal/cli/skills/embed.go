// Package skills holds the agent-facing skill documents the CLI ships and
// serves with "repsol skills".
package skills

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

// FS holds the embedded skill documents, one directory per skill.
//
//go:embed repsol/SKILL.md
var FS embed.FS

// Skill is the metadata of one skill document, read from its SKILL.md
// frontmatter.
type Skill struct {
	// Name is the skill name agents pass to "repsol skills get".
	Name string `json:"name"`
	// Description is the one-line summary of what the skill does.
	Description string `json:"description"`
}

// frontmatterDelimiter opens and closes the YAML frontmatter of a SKILL.md.
const frontmatterDelimiter = "---"

// List returns the embedded skills sorted by name.
func List() ([]Skill, error) {
	paths, err := fs.Glob(FS, "*/SKILL.md")
	if err != nil {
		return nil, skillError(err)
	}

	list := make([]Skill, 0, len(paths))
	for _, path := range paths {
		data, err := FS.ReadFile(path)
		if err != nil {
			return nil, skillError(err)
		}
		skill, err := parseFrontmatter(data)
		if err != nil {
			return nil, err
		}
		list = append(list, skill)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

// parseFrontmatter reads the name and description of a SKILL.md. The document
// is shipped with the binary, so a malformed one is a build-time bug, not user
// input.
func parseFrontmatter(data []byte) (Skill, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, frontmatterDelimiter+"\n") {
		return Skill{}, skillError(fmt.Errorf("missing frontmatter"))
	}

	rest := strings.TrimPrefix(text, frontmatterDelimiter+"\n")
	end := strings.Index(rest, "\n"+frontmatterDelimiter)
	if end < 0 {
		return Skill{}, skillError(fmt.Errorf("unterminated frontmatter"))
	}

	var skill Skill
	for _, line := range strings.Split(rest[:end], "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "name":
			skill.Name = strings.TrimSpace(value)
		case "description":
			skill.Description = strings.TrimSpace(value)
		}
	}
	if skill.Name == "" || skill.Description == "" {
		return Skill{}, skillError(fmt.Errorf("frontmatter needs a name and a description"))
	}
	return skill, nil
}

// skillError wraps a malformed embedded document. It never carries user input.
func skillError(err error) error {
	return apperr.New(apperr.Generic, "skill_invalid",
		"the embedded skill documents are invalid").WithCause(err)
}
