// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/types"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/jaegerquery/querysvc"
	"github.com/jaegertracing/jaeger/internal/storage/v2/api/tracestore"
)

const (
	defaultNLTagCacheTTL     = 2 * time.Minute
	defaultNLTagSearchDepth  = 50
	defaultNLTagSpanScanMax  = 500
	defaultNLTagCandidateMax = 100
)

var (
	lookbackPattern         = regexp.MustCompile(`^(\d+)([smhdw])$`)
	defaultNLFallbackTagSet = []string{"error", "http.status_code", "db.system", "span.kind", "http.method"}
)

type queryServiceAccessor interface {
	QueryService() *querysvc.QueryService
}

type tagCandidateCacheEntry struct {
	tags      []string
	expiresAt time.Time
}

type queryBackedNLSearchCandidatesProvider struct {
	accessor queryServiceAccessor
	logger   *zap.Logger

	now              func() time.Time
	cacheTTL         time.Duration
	traceSearchDepth int
	spanScanLimit    int
	maxTags          int

	cacheMu sync.Mutex
	cache   map[string]tagCandidateCacheEntry
}

// NewQueryBackedNLSearchCandidatesProvider creates an NLSearch candidates provider
// that discovers tag keys from backend traces.
func NewQueryBackedNLSearchCandidatesProvider(accessor queryServiceAccessor, logger *zap.Logger) NLSearchCandidatesProvider {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &queryBackedNLSearchCandidatesProvider{
		accessor:         accessor,
		logger:           logger,
		now:              time.Now,
		cacheTTL:         defaultNLTagCacheTTL,
		traceSearchDepth: defaultNLTagSearchDepth,
		spanScanLimit:    defaultNLTagSpanScanMax,
		maxTags:          defaultNLTagCandidateMax,
		cache:            make(map[string]tagCandidateCacheEntry),
	}
}

func (p *queryBackedNLSearchCandidatesProvider) Enrich(ctx context.Context, req types.NLSearchRequest) (types.NLSearchCandidates, error) {
	candidates := req.Candidates
	tags, err := p.resolveTags(ctx, req)
	if err != nil {
		p.logger.Warn("Failed to resolve NL search tag candidates from backend, using fallback set", zap.Error(err))
		candidates.Tags = cloneStringSlice(defaultNLFallbackTagSet)
		return candidates, nil
	}
	candidates.Tags = tags
	return candidates, nil
}

func (p *queryBackedNLSearchCandidatesProvider) resolveTags(ctx context.Context, req types.NLSearchRequest) ([]string, error) {
	svc := p.currentQueryService()
	if svc == nil {
		return cloneStringSlice(defaultNLFallbackTagSet), nil
	}

	serviceHint := inferServiceHint(req.Query, req.Candidates.Services)
	cacheKey := strings.ToLower(strings.TrimSpace(serviceHint))
	if tags, ok := p.readCache(cacheKey); ok {
		return tags, nil
	}

	lookback := pickSamplingLookback(req.Candidates.Lookbacks)
	end := p.now().UTC()
	query := querysvc.TraceQueryParams{
		TraceQueryParams: tracestore.TraceQueryParams{
			ServiceName:   serviceHint,
			Attributes:    pcommon.NewMap(),
			StartTimeMin:  end.Add(-lookback),
			StartTimeMax:  end,
			DurationMin:   0,
			DurationMax:   0,
			OperationName: "",
			SearchDepth:   p.traceSearchDepth,
		},
	}

	counts := make(map[string]int)
	processedSpans := 0
	processedTraces := 0
	var iterErr error

	svc.FindTraces(ctx, query)(func(traces []ptrace.Traces, err error) bool {
		if err != nil {
			iterErr = err
			return false
		}
		for _, trace := range traces {
			processedTraces++
			processedSpans = collectTagKeysFromTrace(trace, counts, processedSpans, p.spanScanLimit)
			if processedSpans >= p.spanScanLimit || processedTraces >= p.traceSearchDepth {
				return false
			}
		}
		return true
	})
	if iterErr != nil {
		return nil, fmt.Errorf("find traces for tag candidates: %w", iterErr)
	}

	tags := sortTagKeysByFrequency(counts)
	if len(tags) > p.maxTags {
		tags = tags[:p.maxTags]
	}
	tags = mergeFallbackTags(tags)
	p.writeCache(cacheKey, tags)
	return tags, nil
}

func (p *queryBackedNLSearchCandidatesProvider) currentQueryService() *querysvc.QueryService {
	if p.accessor == nil {
		return nil
	}
	return p.accessor.QueryService()
}

func (p *queryBackedNLSearchCandidatesProvider) readCache(key string) ([]string, bool) {
	now := p.now()
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	entry, ok := p.cache[key]
	if !ok {
		return nil, false
	}
	if now.After(entry.expiresAt) {
		delete(p.cache, key)
		return nil, false
	}
	return cloneStringSlice(entry.tags), true
}

func (p *queryBackedNLSearchCandidatesProvider) writeCache(key string, tags []string) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	p.cache[key] = tagCandidateCacheEntry{
		tags:      cloneStringSlice(tags),
		expiresAt: p.now().Add(p.cacheTTL),
	}
}

func inferServiceHint(query string, services []string) string {
	if len(services) == 1 {
		return strings.TrimSpace(services[0])
	}
	lowerQuery := strings.ToLower(query)
	bestMatch := ""
	for _, service := range services {
		s := strings.TrimSpace(service)
		if s == "" {
			continue
		}
		if strings.Contains(lowerQuery, strings.ToLower(s)) {
			if len(s) > len(bestMatch) {
				bestMatch = s
			}
		}
	}
	return bestMatch
}

func pickSamplingLookback(lookbacks []string) time.Duration {
	for _, lookback := range lookbacks {
		if strings.EqualFold(strings.TrimSpace(lookback), "1h") {
			return time.Hour
		}
	}
	for _, lookback := range lookbacks {
		if duration, ok := parseLookbackDuration(lookback); ok {
			return duration
		}
	}
	return time.Hour
}

func parseLookbackDuration(raw string) (time.Duration, bool) {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	matches := lookbackPattern.FindStringSubmatch(trimmed)
	if len(matches) != 3 {
		return 0, false
	}
	value, err := time.ParseDuration(matches[1] + matches[2])
	if err == nil {
		return value, true
	}
	if matches[2] == "d" {
		if v, convErr := time.ParseDuration(matches[1] + "h"); convErr == nil {
			return v * 24, true
		}
	}
	if matches[2] == "w" {
		if v, convErr := time.ParseDuration(matches[1] + "h"); convErr == nil {
			return v * 24 * 7, true
		}
	}
	return 0, false
}

func collectTagKeysFromTrace(trace ptrace.Traces, counts map[string]int, processedSpans, spanLimit int) int {
	resourceSpans := trace.ResourceSpans()
	for i := 0; i < resourceSpans.Len(); i++ {
		resourceSpan := resourceSpans.At(i)
		collectTagKeysFromMap(resourceSpan.Resource().Attributes(), counts)

		scopeSpans := resourceSpan.ScopeSpans()
		for j := 0; j < scopeSpans.Len(); j++ {
			spans := scopeSpans.At(j).Spans()
			for k := 0; k < spans.Len(); k++ {
				processedSpans++
				span := spans.At(k)
				collectTagKeysFromMap(span.Attributes(), counts)
				events := span.Events()
				for e := 0; e < events.Len(); e++ {
					collectTagKeysFromMap(events.At(e).Attributes(), counts)
				}
				if processedSpans >= spanLimit {
					return processedSpans
				}
			}
		}
	}
	return processedSpans
}

func collectTagKeysFromMap(attrs pcommon.Map, counts map[string]int) {
	attrs.Range(func(key string, _ pcommon.Value) bool {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if normalized == "" {
			return true
		}
		counts[normalized]++
		return true
	})
}

func sortTagKeysByFrequency(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] == counts[keys[j]] {
			return keys[i] < keys[j]
		}
		return counts[keys[i]] > counts[keys[j]]
	})
	return keys
}

func mergeFallbackTags(tags []string) []string {
	result := make([]string, 0, len(tags)+len(defaultNLFallbackTagSet))
	seen := make(map[string]struct{}, len(tags)+len(defaultNLFallbackTagSet))
	for _, tag := range tags {
		normalized := strings.ToLower(strings.TrimSpace(tag))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	for _, fallback := range defaultNLFallbackTagSet {
		if _, ok := seen[fallback]; ok {
			continue
		}
		seen[fallback] = struct{}{}
		result = append(result, fallback)
	}
	return result
}

func cloneStringSlice(values []string) []string {
	out := make([]string, len(values))
	copy(out, values)
	return out
}
