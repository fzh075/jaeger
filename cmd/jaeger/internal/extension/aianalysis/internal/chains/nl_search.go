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

	allowedNLSearchFields = map[string]struct{}{
		"service_name":   {},
		"operation_name": {},
		"duration_min":   {},
		"duration_max":   {},
		"lookback":       {},
		"start_time_min": {},
		"start_time_max": {},
		"tags":           {},
		"limit":          {},
		"confidence":     {},
		"explanation":    {},
	}
	forbiddenNLSearchFields = map[string]struct{}{ // TODO(fzh075) delete?
		"attributes": {},
		"tag_keys":   {},
		"has_errors": {},
		"language":   {},
	}
	responseFieldAliases = map[string]string{
		"service":        "service_name",
		"servicename":    "service_name",
		"operation":      "operation_name",
		"operationname":  "operation_name",
		"span":           "operation_name",
		"spanname":       "operation_name",
		"minduration":    "duration_min",
		"maxduration":    "duration_max",
		"starttime":      "start_time_min",
		"endtime":        "start_time_max",
		"start":          "start_time_min",
		"end":            "start_time_max",
		"lookback":       "lookback",
		"lookbacktime":   "lookback",
		"service_name":   "service_name",
		"operation_name": "operation_name",
		"span_name":      "operation_name",
		"duration_min":   "duration_min",
		"duration_max":   "duration_max",
		"start_time_min": "start_time_min",
		"start_time_max": "start_time_max",
		"confidence":     "confidence",
		"explanation":    "explanation",
		"limit":          "limit",
		"tags":           "tags",
		"tag":            "tags",
		"attributes":     "attributes",
		"tag_keys":       "tag_keys",
		"has_errors":     "has_errors",
		"language":       "language",
	}
)

// TODO(fzh075) tags.error
const nlSearchPromptTemplate = `You are a deterministic parser for Jaeger trace search.

Convert the natural language query into structured search parameters JSON.

IMPORTANT RULES:
1. Extract only explicitly mentioned parameters.
2. Use candidate values for service_name/operation_name/lookback/tags keys when candidates are provided. If you are not sure, leave the field empty.
3. Do NOT output has_errors. For error intent, output tags.error="true".
4. Duration format must be one of: us, ms, s, m, h (examples: "500ms", "2s", "1m").
5. Prefer lookback over explicit start/end when possible.
6. Use lookback="custom" only when query explicitly requests a concrete time range.
7. If lookback is not "custom", keep start_time_min and start_time_max empty.
8. If lookback is "custom", both start_time_min and start_time_max must be present.
9. Default limit is 20 if not specified.

OUTPUT FORMAT (JSON only, no prose):
{
  "service_name": "string or empty",
  "operation_name": "string or empty",
  "duration_min": "string or empty",
  "duration_max": "string or empty",
  "lookback": "string or empty",
  "start_time_min": "string or empty",
  "start_time_max": "string or empty",
  "tags": {"key": "value"} or {},
  "limit": number,
  "confidence": 0.0-1.0,
  "explanation": "optional short reasoning"
}

EXAMPLES:
Query: "Show me 500 errors from payment-service > 2s"
{"service_name":"payment-service","operation_name":"","duration_min":"2s","duration_max":"","lookback":"","start_time_min":"","start_time_max":"","tags":{"http.status_code":"500","error":"true"},"limit":20,"confidence":0.92,"explanation":"HTTP 500 implies errors with lower-bound latency"}

Query: "frontend service last 1 hour errors"
{"service_name":"frontend","operation_name":"","duration_min":"","duration_max":"","lookback":"1h","start_time_min":"","start_time_max":"","tags":{"error":"true"},"limit":20,"confidence":0.90,"explanation":""}

Query: "Find checkout operation taking between 500ms and 1s"
{"service_name":"","operation_name":"checkout","duration_min":"500ms","duration_max":"1s","lookback":"","start_time_min":"","start_time_max":"","tags":{},"limit":20,"confidence":0.88,"explanation":""}

Query: "mysql queries in order-service"
{"service_name":"order-service","operation_name":"","duration_min":"","duration_max":"","lookback":"","start_time_min":"","start_time_max":"","tags":{"db.system":"mysql"},"limit":20,"confidence":0.83,"explanation":""}

CANDIDATES (must follow when provided):
%s

Now parse this query:
Query: "%s"
`

const nlSearchRepairPromptTemplate = `Fix the invalid Jaeger NL search JSON.

Return JSON object only and strictly follow this schema:
{
  "service_name": "string or empty",
  "operation_name": "string or empty",
  "duration_min": "string or empty",
  "duration_max": "string or empty",
  "lookback": "string or empty",
  "start_time_min": "string or empty",
  "start_time_max": "string or empty",
  "tags": {"key": "value"} or {},
  "limit": number,
  "confidence": 0.0-1.0,
  "explanation": "optional short reasoning"
}

STRICT RULES:
1. Use candidates when provided.
2. Do NOT output attributes / tag_keys / has_errors / language.
3. Put error intent into tags.error="true".
4. Use lookback first; start/end only for lookback="custom".
5. No markdown, no prose, JSON only.

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
	return fmt.Sprintf(nlSearchPromptTemplate, formatCandidatesForPrompt(req.Candidates), req.Query)
}

func buildNLSearchRepairPrompt(req types.NLSearchRequest, invalidOutput string, parseErr error) string {
	return fmt.Sprintf(
		nlSearchRepairPromptTemplate,
		formatCandidatesForPrompt(req.Candidates),
		req.Query,
		parseErr.Error(),
		invalidOutput,
	)
}

func formatCandidatesForPrompt(candidates types.NLSearchCandidates) string {
	payload := map[string][]string{
		"service_name":   normalizeCandidateList(candidates.ServiceName),
		"operation_name": normalizeCandidateList(candidates.OperationName),
		"lookback":       normalizeCandidateList(candidates.Lookback),
		"tags":           normalizeCandidateList(candidates.Tags),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func parseAndNormalizeNLResponse(response string, candidates types.NLSearchCandidates) (types.NLSearchResponse, error) {
	// TODO(fzh075)
	fmt.Printf("[fzh] formatCandidatesForPrompt(candidates) = %+v\n", formatCandidatesForPrompt(candidates))
	// TODO(fzh075)
	fmt.Printf("[fzh] response = %+v\n", response)

	jsonStr := extractJSON(response)

	// TODO(fzh075)
	fmt.Printf("[fzh] jsonStr = %+v\n", jsonStr)

	normalizedJSON, err := normalizeResponseFieldAliases(jsonStr)

	if err != nil {
		return types.NLSearchResponse{}, fmt.Errorf("unmarshal model response: %w", err)
	}
	if err := validateNLSearchOutputFields(normalizedJSON); err != nil {
		return types.NLSearchResponse{}, err
	}

	var parsed ParsedQueryWithConfidence
	if err := json.Unmarshal([]byte(normalizedJSON), &parsed); err != nil {
		return types.NLSearchResponse{}, fmt.Errorf("unmarshal model response: %w", err)
	}
	if parsed.Tags == nil {
		parsed.Tags = map[string]string{}
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

	parsed.OperationName = strings.TrimSpace(parsed.OperationName)
	if parsed.OperationName != "" {
		canonical, ok := lookup.matchOperation(parsed.OperationName)
		if !ok {
			return fmt.Errorf("operation_name %q is not in candidates", parsed.OperationName)
		}
		parsed.OperationName = canonical
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
		if !startParsed.Before(endParsed) {
			return fmt.Errorf("start_time_min must be earlier than start_time_max")
		}
		parsed.StartTimeMin = startParsed.Format(time.RFC3339)
		parsed.StartTimeMax = endParsed.Format(time.RFC3339)
	}

	normalizedTags := make(map[string]string)
	for k, v := range parsed.Tags {
		key := strings.TrimSpace(strings.ToLower(k))
		if key == "" {
			continue
		}
		canonical, ok := lookup.matchTagKey(key)
		if !ok {
			return fmt.Errorf("tag key %q is not in candidates", key)
		}
		normalizedTags[canonical] = strings.TrimSpace(v)
	}
	parsed.Tags = normalizedTags

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
	tags       map[string]string
}

func newCandidateLookup(candidates types.NLSearchCandidates) candidateLookup {
	lookbacks := normalizeCandidateMap(candidates.Lookback)
	if len(lookbacks) > 0 {
		lookbacks["custom"] = "custom"
	}
	return candidateLookup{
		services:   normalizeCandidateMap(candidates.ServiceName),
		operations: normalizeCandidateMap(candidates.OperationName),
		lookbacks:  lookbacks,
		tags:       normalizeCandidateMap(candidates.Tags),
	}
}

func normalizeCandidateMap(values []string) map[string]string {
	result := make(map[string]string)
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if _, exists := result[lower]; exists {
			continue
		}
		result[lower] = trimmed
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
	return matchCandidate(value, l.tags)
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

func validateNLSearchOutputFields(jsonStr string) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return fmt.Errorf("unmarshal model response: %w", err)
	}

	for key := range raw {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if _, forbidden := forbiddenNLSearchFields[normalized]; forbidden {
			return fmt.Errorf("field %q is not allowed", key)
		}
		if _, ok := allowedNLSearchFields[normalized]; !ok {
			return fmt.Errorf("unknown field %q", key)
		}
	}

	return nil
}

func normalizeResponseFieldAliases(jsonStr string) (string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return "", err
	}

	normalized := make(map[string]json.RawMessage, len(raw))
	for key, value := range raw {
		mappedKey := strings.ToLower(strings.TrimSpace(key))
		if alias, ok := responseFieldAliases[canonicalFieldName(key)]; ok {
			mappedKey = alias
		}
		if _, exists := normalized[mappedKey]; exists {
			continue
		}
		normalized[mappedKey] = value
	}

	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func canonicalFieldName(key string) string {
	normalized := strings.ToLower(strings.TrimSpace(key))
	replacer := strings.NewReplacer("_", "", "-", "", " ", "")
	return replacer.Replace(normalized)
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
