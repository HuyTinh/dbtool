package integrity

import (
	"regexp"
	"strings"
)

type FilterMatchResult struct {
	IncludeTableMatched    []string
	IncludeTableUnmatched  []string
	ExcludeTableMatched    []string
	ExcludeTableUnmatched  []string
	IncludeSchemaMatched   []string
	IncludeSchemaUnmatched []string
	ExcludeSchemaMatched   []string
	ExcludeSchemaUnmatched []string
}

func (r FilterMatchResult) HasUnmatched() bool {
	return len(r.IncludeTableUnmatched) > 0 ||
		len(r.ExcludeTableUnmatched) > 0 ||
		len(r.IncludeSchemaUnmatched) > 0 ||
		len(r.ExcludeSchemaUnmatched) > 0
}

func (r FilterMatchResult) UnmatchedPatterns() []string {
	var all []string
	all = append(all, r.IncludeTableUnmatched...)
	all = append(all, r.ExcludeTableUnmatched...)
	all = append(all, r.IncludeSchemaUnmatched...)
	all = append(all, r.ExcludeSchemaUnmatched...)
	return all
}

func MatchFilters(toc TOCEntryList, includeTable, excludeTable, includeSchema, excludeSchema []string) FilterMatchResult {
	tables := toc.Tables()

	return FilterMatchResult{
		IncludeTableMatched:    matchTablePatterns(includeTable, tables),
		IncludeTableUnmatched:  unmatchedPatterns(includeTable, tables),
		ExcludeTableMatched:    matchTablePatterns(excludeTable, tables),
		ExcludeTableUnmatched:  unmatchedPatterns(excludeTable, tables),
		IncludeSchemaMatched:   matchSchemaPatterns(includeSchema, toc.Schemas()),
		IncludeSchemaUnmatched: unmatchedSchemaPatterns(includeSchema, toc.Schemas()),
		ExcludeSchemaMatched:   matchSchemaPatterns(excludeSchema, toc.Schemas()),
		ExcludeSchemaUnmatched: unmatchedSchemaPatterns(excludeSchema, toc.Schemas()),
	}
}

func patternToRegex(pattern string) *regexp.Regexp {
	p := strings.Trim(pattern, `"`)

	var schemaPat, tablePat string
	if idx := strings.Index(p, "."); idx >= 0 {
		schemaPat = p[:idx]
		tablePat = p[idx+1:]
	} else {
		tablePat = p
	}

	toRegexp := func(s string) string {
		s = strings.Trim(s, `"`)
		var b strings.Builder
		for _, c := range s {
			switch c {
			case '*':
				b.WriteString(".*")
			case '?':
				b.WriteString(".")
			default:
				b.WriteString(regexp.QuoteMeta(string(c)))
			}
		}
		return b.String()
	}

	var full string
	if schemaPat != "" {
		full = "^" + toRegexp(schemaPat) + `\.` + toRegexp(tablePat) + "$"
	} else {
		full = "^" + toRegexp(tablePat) + "$"
	}

	re, err := regexp.Compile(full)
	if err != nil {
		return nil
	}
	return re
}

func matchTablePatterns(patterns []string, tables []TOCEntry) []string {
	var matched []string
	for _, pat := range patterns {
		re := patternToRegex(pat)
		if re == nil {
			continue
		}
		for _, t := range tables {
			fullName := t.Schema + "." + t.Name
			if re.MatchString(fullName) || re.MatchString(t.Name) {
				matched = append(matched, pat)
				break
			}
		}
	}
	return matched
}

func unmatchedPatterns(patterns []string, tables []TOCEntry) []string {
	var unmatched []string
	for _, pat := range patterns {
		re := patternToRegex(pat)
		if re == nil {
			unmatched = append(unmatched, pat)
			continue
		}
		found := false
		for _, t := range tables {
			fullName := t.Schema + "." + t.Name
			if re.MatchString(fullName) || re.MatchString(t.Name) {
				found = true
				break
			}
		}
		if !found {
			unmatched = append(unmatched, pat)
		}
	}
	return unmatched
}

func matchSchemaPatterns(patterns []string, schemas []string) []string {
	var matched []string
	for _, pat := range patterns {
		re := schemaPatternToRegex(pat)
		if re == nil {
			continue
		}
		for _, s := range schemas {
			if re.MatchString(s) {
				matched = append(matched, pat)
				break
			}
		}
	}
	return matched
}

func unmatchedSchemaPatterns(patterns []string, schemas []string) []string {
	var unmatched []string
	for _, pat := range patterns {
		re := schemaPatternToRegex(pat)
		if re == nil {
			unmatched = append(unmatched, pat)
			continue
		}
		found := false
		for _, s := range schemas {
			if re.MatchString(s) {
				found = true
				break
			}
		}
		if !found {
			unmatched = append(unmatched, pat)
		}
	}
	return unmatched
}

func schemaPatternToRegex(pattern string) *regexp.Regexp {
	p := strings.Trim(pattern, `"`)
	// If pattern has schema.table format, extract just the schema part
	if idx := strings.Index(p, "."); idx >= 0 {
		p = p[:idx]
	}

	var b strings.Builder
	b.WriteString("^")
	for _, c := range p {
		switch c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")

	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	return re
}
