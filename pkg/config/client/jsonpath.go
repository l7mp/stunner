package client

import (
	"fmt"
	"regexp"
	"strings"

	"k8s.io/client-go/util/jsonpath"
)

// ////////////////////////
var (
	jsonRegexp       = regexp.MustCompile(`^\{\.?([^{}]+)\}$|^\.?([^{}]+)$`)
	jsonStringRegexp = regexp.MustCompile(`(\{[^}]*\}|[^\{]+)`)
)

// JSONPathArg parses a `-jsonpath="... {jsonpath} ..."` expression and knows how to evaluate the
// parsed JSONpath query/queries on any data.
type JSONPathArg interface {
	Parse(arg string) (bool, error)
	Evaluate(data interface{}) (string, error)
}

// SegmentType identifies the type of string segment in JSONPath.
type SegmentType int

const (
	// String represents a plain text segment.
	String SegmentType = iota
	// Expression represents a segment enclosed in curly braces.
	Expression
)

// Segment represents a part of a JSONPath string
type Segment struct {
	// String is the actual text of the segment (if any).
	String string
	// JSONQuery is the actual query in the segment.
	JSONQuery *jsonpath.JSONPath
	// Type indicates whether this is a regular string or an expression.
	Type SegmentType
}

type jsonPath struct {
	arg      string
	segments []Segment
}

// NewJSONPath creates a new JSONPathArg instance
func NewJSONPath() JSONPathArg { return &jsonPath{segments: []Segment{}} }

// Parse processes a potential jsonpath argument and extracts the relevant parts
func (jp *jsonPath) Parse(arg string) (bool, error) {
	if !strings.HasPrefix(arg, "jsonpath") {
		return false, nil
	}

	as := strings.Split(arg, "=")
	if len(as) != 2 || as[0] != "jsonpath" {
		return false, fmt.Errorf("invalid jsonpath argument %q", arg)
	}
	jp.arg = as[1]

	// Parse a string into segments of regular text and expressions (text enclosed in curly
	// braces)
	matches := jsonStringRegexp.FindAllString(as[1], -1)
	for _, match := range matches {
		segment := Segment{}

		// Check if this is an expression (starts with '{' and ends with '}')
		if len(match) >= 2 && match[0] == '{' && match[len(match)-1] == '}' {
			jsonQuery := jsonpath.New("arg")

			// Parse and print jsonpath
			fields, err := relaxedJSONPathExpression(match)
			if err != nil {
				return false, fmt.Errorf("invalid jsonpath query: %w", err)
			}

			if err := jsonQuery.Parse(fields); err != nil {
				return false, fmt.Errorf("cannot parse jsonpath query: %w", err)
			}
			segment.Type = Expression
			segment.JSONQuery = jsonQuery

		} else {
			segment.Type = String
			segment.String = match
		}

		jp.segments = append(jp.segments, segment)
	}

	return true, nil
}

// Evaluate applies the parsed JSONPath to the provided data and returns the result
func (jp *jsonPath) Evaluate(data interface{}) (string, error) {
	ret := ""
	for _, s := range jp.segments {
		if s.Type == String {
			ret += s.String
			continue
		}

		// Bug fix: Changed from FindResults(s) to FindResults(data)
		values, err := s.JSONQuery.FindResults(data)
		if err != nil {
			return "", err
		}

		if len(values) == 0 || len(values[0]) == 0 {
			// Changed from printing to stdout to appending to result string
			ret += "<none>"
			continue
		}

		for arrIx := range values {
			for valIx := range values[arrIx] {
				// Remove newline from output to be consistent with String segments
				ret += fmt.Sprintf("%v", values[arrIx][valIx].Interface())
			}
		}
	}

	return ret, nil
}

// relaxedJSONPathExpression normalizes JSONPath expressions to the canonical form
// accepted by the JSONPath parser
func relaxedJSONPathExpression(pathExpression string) (string, error) {
	if len(pathExpression) == 0 {
		return pathExpression, nil
	}
	submatches := jsonRegexp.FindStringSubmatch(pathExpression)
	if submatches == nil {
		return "", fmt.Errorf("unexpected path string, expected a 'name1.name2' or '.name1.name2' or '{name1.name2}' or '{.name1.name2}'")
	}
	if len(submatches) != 3 {
		return "", fmt.Errorf("unexpected submatch list: %v", submatches)
	}
	var fieldSpec string
	if len(submatches[1]) != 0 {
		fieldSpec = submatches[1]
	} else {
		fieldSpec = submatches[2]
	}
	return fmt.Sprintf("{.%s}", fieldSpec), nil
}

// GetSegments returns the parsed segments for testing and inspection
func (jp *jsonPath) GetSegments() []Segment {
	return jp.segments
}
