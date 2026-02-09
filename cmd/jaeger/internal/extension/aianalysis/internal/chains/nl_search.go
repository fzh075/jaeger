// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package chains

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/llm"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/types"
)

const (
	defaultNLSearchLimit = 20
	maxNLSearchLimit     = 1000
)

var (
	durationPattern = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)(us|ms|s|m|h)$`)
	relativePattern = regexp.MustCompile(`^-(\d+)([smhdw])$`)
)

const nlSearchPromptTemplate = `You are a deterministic parser for Jaeger trace search.

Convert the natural language query into structured search parameters JSON.

IMPORTANT RULES:
1. Extract only explicitly mentioned parameters
2. Output service_name / span_name / lookback from candidates when candidates are provided
3. For error intents, use attributes.error="true" (do NOT output has_errors)
4. Duration format must be one of: us, ms, s, m, h (e.g. "500ms", "2s", "1m")
5. Prefer lookback over explicit start/end when possible
6. Use lookback="custom" only when query explicitly requests concrete time range
7. If lookback is not "custom", keep start_time_min and start_time_max empty
8. Default limit is 20 if not specified

LANGUAGE PREFERENCE:
%s

OUTPUT FORMAT (JSON only, no explanation):
{
  "service_name": "string or empty",
  "span_name": "string or empty",
  "duration_min": "string or empty",
  "duration_max": "string or empty",
  "lookback": "string or empty",
  "start_time_min": "string or empty",
  "start_time_max": "string or empty",
  "attributes": {"key": "value"} or {},
  "limit": number,
  "confidence": 0.0-1.0,
  "explanation": "optional short reasoning"
}

CANDIDATES (must follow when provided):
%s

Now parse this query:
Query: "%s"
`

const nlSearchRepairPromptTemplate = `Fix the invalid Jaeger NL search JSON.

Return JSON object only and strictly follow this schema:
{
  "service_name": "string or empty",
  "span_name": "string or empty",
  "duration_min": "string or empty",
  "duration_max": "string or empty",
  "lookback": "string or empty",
  "start_time_min": "string or empty",
  "start_time_max": "string or empty",
  "attributes": {"key": "value"} or {},
  "limit": number,
  "confidence": 0.0-1.0,
  "explanation": "optional short reasoning"
}

RULES:
1. Use candidates when provided
2. Put error intent into attributes.error="true"
3. Use lookback first; start/end only for lookback="custom"
4. No markdown, no prose, JSON only

LANGUAGE PREFERENCE:
%s

CANDIDATES:
%s

USER QUERY:
%s

VALIDATION ERROR:
%s

INVALID OUTPUT:
%s
`

// NLSearchChain handles natural language to search parameter conversion.
type NLSearchChain struct {
	provider llm.Provider
}

// NewNLSearchChain creates a new NL search chain.
func NewNLSearchChain(provider llm.Provider) *NLSearchChain {
	return &NLSearchChain{provider: provider}
}

// ParsedQueryWithConfidence extends ParsedQuery with confidence.
type ParsedQueryWithConfidence struct {
	types.ParsedQuery
	Confidence  float64 `json:"confidence"`
	Explanation string  `json:"explanation"`
}

// Parse converts a natural language query to structured search parameters.
func (c *NLSearchChain) Parse(ctx context.Context, req types.NLSearchRequest) (types.NLSearchResponse, error) {
	if c.provider == nil {
		return types.NLSearchResponse{}, ErrProviderUnavailable
	}

	prompt := buildNLSearchPrompt(req)
	response, err := c.provider.Generate(ctx, prompt)
	if err != nil {
		return types.NLSearchResponse{}, fmt.Errorf("%w: %w", ErrLLMGeneration, err)
	}

	// TODO(fzh075)
	fmt.Printf("[fzh] response = %+v\n", response)

	parsed, parseErr := parseAndNormalizeNLResponse(response, req.Candidates)
	if parseErr == nil {
		return parsed, nil
	}

	repairPrompt := buildNLSearchRepairPrompt(req, response, parseErr)
	repairedResponse, err := c.provider.Generate(ctx, repairPrompt)
	if err != nil {
		return types.NLSearchResponse{}, fmt.Errorf("%w: %w", ErrLLMGeneration, err)
	}

	parsed, parseErr = parseAndNormalizeNLResponse(repairedResponse, req.Candidates)
	if parseErr != nil {
		return types.NLSearchResponse{}, fmt.Errorf("%w: %w", ErrInvalidLLMResponse, parseErr)
	}

	return parsed, nil
}

func buildNLSearchPrompt(req types.NLSearchRequest) string {
	language := strings.TrimSpace(req.Language)
	if language == "" {
		language = "auto"
	}
	return fmt.Sprintf(nlSearchPromptTemplate, language, formatCandidatesForPrompt(req.Candidates), req.Query)
}

func buildNLSearchRepairPrompt(req types.NLSearchRequest, invalidOutput string, parseErr error) string {
	language := strings.TrimSpace(req.Language)
	if language == "" {
		language = "auto"
	}
	return fmt.Sprintf(
		nlSearchRepairPromptTemplate,
		language,
		formatCandidatesForPrompt(req.Candidates),
		req.Query,
		parseErr.Error(),
		invalidOutput,
	)
}

func formatCandidatesForPrompt(candidates types.NLSearchCandidates) string {
	payload := map[string][]string{
		"services":   normalizeCandidateList(candidates.Services),
		"operations": normalizeCandidateList(candidates.Operations),
		"lookbacks":  normalizeCandidateList(candidates.Lookbacks),
		"tag_keys":   normalizeCandidateList(candidates.TagKeys),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func parseAndNormalizeNLResponse(response string, candidates types.NLSearchCandidates) (types.NLSearchResponse, error) {
	jsonStr := extractJSON(response)

	// TODO(fzh075)
	fmt.Printf("[fzh] jsonStr = %+v\n", jsonStr)

	var parsed ParsedQueryWithConfidence
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return types.NLSearchResponse{}, fmt.Errorf("unmarshal model response: %w", err)
	}

	if err := normalizeAndValidateParsedQuery(&parsed.ParsedQuery, candidates); err != nil {
		return types.NLSearchResponse{}, err
	}

	if parsed.Confidence < 0 {
		parsed.Confidence = 0
	} else if parsed.Confidence > 1 {
		parsed.Confidence = 1
	}
	if parsed.Explanation != "" {
		parsed.Explanation = strings.TrimSpace(parsed.Explanation)
	}

	return types.NLSearchResponse{
		ParsedQuery: parsed.ParsedQuery,
		Confidence:  parsed.Confidence,
		Explanation: parsed.Explanation,
	}, nil
}

func normalizeAndValidateParsedQuery(parsed *types.ParsedQuery, candidates types.NLSearchCandidates) error {
	lookup := newCandidateLookup(candidates)

	parsed.ServiceName = strings.TrimSpace(parsed.ServiceName)
	if parsed.ServiceName != "" {
		canonical, ok := lookup.matchService(parsed.ServiceName)
		if !ok {
			return fmt.Errorf("service_name %q is not in candidates", parsed.ServiceName)
		}
		parsed.ServiceName = canonical
	}

	parsed.SpanName = strings.TrimSpace(parsed.SpanName)
	if parsed.SpanName != "" {
		canonical, ok := lookup.matchOperation(parsed.SpanName)
		if !ok {
			return fmt.Errorf("span_name %q is not in candidates", parsed.SpanName)
		}
		parsed.SpanName = canonical
	}

	minDuration, minValue, err := normalizeDuration(parsed.DurationMin)
	if err != nil {
		return fmt.Errorf("duration_min: %w", err)
	}
	maxDuration, maxValue, err := normalizeDuration(parsed.DurationMax)
	if err != nil {
		return fmt.Errorf("duration_max: %w", err)
	}
	if minValue > 0 && maxValue > 0 && maxValue < minValue {
		return fmt.Errorf("duration_max must be greater than or equal to duration_min")
	}
	parsed.DurationMin = minDuration
	parsed.DurationMax = maxDuration

	parsed.Lookback = strings.TrimSpace(parsed.Lookback)
	if parsed.Lookback != "" {
		canonical, ok := lookup.matchLookback(parsed.Lookback)
		if !ok {
			return fmt.Errorf("lookback %q is not in candidates", parsed.Lookback)
		}
		parsed.Lookback = canonical
	}
	if parsed.Lookback == "" && (strings.TrimSpace(parsed.StartTimeMin) != "" || strings.TrimSpace(parsed.StartTimeMax) != "") {
		parsed.Lookback = "custom"
	}

	startTime := strings.TrimSpace(parsed.StartTimeMin)
	endTime := strings.TrimSpace(parsed.StartTimeMax)
	if parsed.Lookback != "custom" {
		parsed.StartTimeMin = ""
		parsed.StartTimeMax = ""
	} else {
		if startTime == "" || endTime == "" {
			return fmt.Errorf("lookback custom requires both start_time_min and start_time_max")
		}
		startParsed, err := normalizeTime(startTime, time.Now().UTC())
		if err != nil {
			return fmt.Errorf("start_time_min: %w", err)
		}
		endParsed, err := normalizeTime(endTime, time.Now().UTC())
		if err != nil {
			return fmt.Errorf("start_time_max: %w", err)
		}
		if endParsed.Before(startParsed) {
			return fmt.Errorf("start_time_min must be earlier than start_time_max")
		}
		parsed.StartTimeMin = startParsed.Format(time.RFC3339)
		parsed.StartTimeMax = endParsed.Format(time.RFC3339)
	}

	normalizedAttrs := make(map[string]string)
	for k, v := range parsed.Attributes {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		canonical, ok := lookup.matchTagKey(key)
		if !ok {
			return fmt.Errorf("attribute key %q is not in candidates", key)
		}
		normalizedAttrs[canonical] = strings.TrimSpace(v)
	}
	parsed.Attributes = normalizedAttrs

	if parsed.Limit <= 0 {
		parsed.Limit = defaultNLSearchLimit
	}
	if parsed.Limit > maxNLSearchLimit {
		parsed.Limit = maxNLSearchLimit
	}

	return nil
}

func normalizeDuration(raw string) (string, time.Duration, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return "", 0, nil
	}
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, "µ", "u")
	value = strings.ReplaceAll(value, "μ", "u")
	if !durationPattern.MatchString(value) {
		return "", 0, fmt.Errorf("invalid duration format %q", raw)
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return "", 0, fmt.Errorf("parse duration: %w", err)
	}
	return value, parsed, nil
}

func normalizeTime(raw string, now time.Time) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, fmt.Errorf("value is empty")
	}
	if strings.EqualFold(value, "now") {
		return now, nil
	}
	if strings.HasPrefix(value, "-") {
		relative, err := parseRelativeDuration(value)
		if err != nil {
			return time.Time{}, err
		}
		return now.Add(-relative), nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), nil
	}
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time format %q", raw)
}

func parseRelativeDuration(raw string) (time.Duration, error) {
	matches := relativePattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(raw)))
	if len(matches) != 3 {
		return 0, fmt.Errorf("invalid relative time %q", raw)
	}
	value, err := strconv.Atoi(matches[1])
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid relative time %q", raw)
	}
	switch matches[2] {
	case "s":
		return time.Duration(value) * time.Second, nil
	case "m":
		return time.Duration(value) * time.Minute, nil
	case "h":
		return time.Duration(value) * time.Hour, nil
	case "d":
		return time.Duration(value) * 24 * time.Hour, nil
	case "w":
		return time.Duration(value) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid relative time %q", raw)
	}
}

type candidateLookup struct {
	services   map[string]string
	operations map[string]string
	lookbacks  map[string]string
	tagKeys    map[string]string
}

func newCandidateLookup(candidates types.NLSearchCandidates) candidateLookup {
	lookbacks := normalizeCandidateMap(candidates.Lookbacks)
	if len(lookbacks) > 0 {
		lookbacks["custom"] = "custom"
	}
	return candidateLookup{
		services:   normalizeCandidateMap(candidates.Services),
		operations: normalizeCandidateMap(candidates.Operations),
		lookbacks:  lookbacks,
		tagKeys:    normalizeCandidateMap(candidates.TagKeys),
	}
}

func normalizeCandidateMap(values []string) map[string]string {
	result := make(map[string]string)
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		result[strings.ToLower(trimmed)] = trimmed
	}
	return result
}

func normalizeCandidateList(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func (l candidateLookup) matchService(value string) (string, bool) {
	return matchCandidate(value, l.services)
}

func (l candidateLookup) matchOperation(value string) (string, bool) {
	return matchCandidate(value, l.operations)
}

func (l candidateLookup) matchLookback(value string) (string, bool) {
	return matchCandidate(strings.ToLower(strings.TrimSpace(value)), l.lookbacks)
}

func (l candidateLookup) matchTagKey(value string) (string, bool) {
	return matchCandidate(value, l.tagKeys)
}

func matchCandidate(value string, candidates map[string]string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", true
	}
	if len(candidates) == 0 {
		return trimmed, true
	}
	matched, ok := candidates[strings.ToLower(trimmed)]
	return matched, ok
}

// extractJSON extracts JSON from a response that may contain Markdown code blocks.
func extractJSON(response string) string {
	response = strings.TrimSpace(response)

	// Try to find JSON in code block
	if idx := strings.Index(response, "```json"); idx != -1 {
		start := idx + 7
		end := strings.Index(response[start:], "```")
		if end != -1 {
			return strings.TrimSpace(response[start : start+end])
		}
	}

	// Try to find JSON in generic code block
	if idx := strings.Index(response, "```"); idx != -1 {
		start := idx + 3
		// Skip language identifier line
		if nlIdx := strings.Index(response[start:], "\n"); nlIdx != -1 {
			start = start + nlIdx + 1
		}
		end := strings.Index(response[start:], "```")
		if end != -1 {
			return strings.TrimSpace(response[start : start+end])
		}
	}

	// Find the first JSON structure (object or array)
	objIdx := strings.Index(response, "{")
	arrIdx := strings.Index(response, "[")

	// Determine which comes first
	if arrIdx != -1 && (objIdx == -1 || arrIdx < objIdx) {
		// Array comes first
		lastIdx := strings.LastIndex(response, "]")
		if lastIdx > arrIdx {
			return response[arrIdx : lastIdx+1]
		}
	} else if objIdx != -1 {
		// Object comes first
		lastIdx := strings.LastIndex(response, "}")
		if lastIdx > objIdx {
			return response[objIdx : lastIdx+1]
		}
	}

	return response
}
