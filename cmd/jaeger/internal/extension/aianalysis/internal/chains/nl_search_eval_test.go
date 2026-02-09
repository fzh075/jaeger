// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package chains

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/types"
)

const (
	offlineEvalPath         = "testdata/nlsearch_eval/golden.jsonl"
	offlineEvalMinSlotScore = 0.90
)

type offlineEvalCase struct {
	ID          string                   `json:"id"`
	Query       string                   `json:"query"`
	Candidates  types.NLSearchCandidates `json:"candidates"`
	ModelOutput string                   `json:"model_output"`
	Expected    types.ParsedQuery        `json:"expected"`
}

func TestNLSearchOfflineEvalGolden(t *testing.T) {
	cases := loadOfflineEvalCases(t, offlineEvalPath)
	require.NotEmpty(t, cases)

	var matchedSlots int
	var totalSlots int
	var executableCount int

	for _, tc := range cases {
		provider := &sequenceProvider{responses: []string{tc.ModelOutput}}
		chain := NewNLSearchChain(provider)
		resp, err := chain.Parse(context.Background(), types.NLSearchRequest{
			Query:      tc.Query,
			Candidates: tc.Candidates,
		})
		require.NoErrorf(t, err, "case %s should be executable", tc.ID)
		executableCount++

		matchedSlots += countMatchedSlots(tc.Expected, resp.ParsedQuery)
		totalSlots += 9
	}

	slotAccuracy := float64(matchedSlots) / float64(totalSlots)
	executableRate := float64(executableCount) / float64(len(cases))
	t.Logf("offline eval: cases=%d slot_accuracy=%.3f executable_rate=%.3f", len(cases), slotAccuracy, executableRate)
	require.GreaterOrEqualf(t, slotAccuracy, offlineEvalMinSlotScore, "slot accuracy should be >= %.2f", offlineEvalMinSlotScore)
}

func loadOfflineEvalCases(t *testing.T, path string) []offlineEvalCase {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()

	cases := make([]offlineEvalCase, 0)
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		text := scanner.Text()
		if text == "" {
			continue
		}
		var tc offlineEvalCase
		err := json.Unmarshal([]byte(text), &tc)
		require.NoErrorf(t, err, "invalid jsonl at line %d", line)
		require.NotEmptyf(t, tc.ID, "missing id at line %d", line)
		require.NotEmptyf(t, tc.Query, "missing query for %s", tc.ID)
		require.NotEmptyf(t, tc.ModelOutput, "missing model_output for %s", tc.ID)
		cases = append(cases, tc)
	}
	require.NoError(t, scanner.Err())
	return cases
}

func countMatchedSlots(expected, actual types.ParsedQuery) int {
	matched := 0
	if expected.ServiceName == actual.ServiceName {
		matched++
	}
	if expected.SpanName == actual.SpanName {
		matched++
	}
	if expected.Lookback == actual.Lookback {
		matched++
	}
	if expected.StartTimeMin == actual.StartTimeMin {
		matched++
	}
	if expected.StartTimeMax == actual.StartTimeMax {
		matched++
	}
	if expected.DurationMin == actual.DurationMin {
		matched++
	}
	if expected.DurationMax == actual.DurationMax {
		matched++
	}
	if expected.Limit == actual.Limit {
		matched++
	}
	if reflect.DeepEqual(expected.Attributes, actual.Attributes) {
		matched++
	}
	return matched
}
