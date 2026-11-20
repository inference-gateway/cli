package storage

import (
	"encoding/json"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

// tokenStats assembles the token tally of a conversations row, deriving
// TotalTokens from its input and output halves.
func tokenStats(input, output, requests int) convdomain.SessionTokenStats {
	return convdomain.SessionTokenStats{
		TotalInputTokens:  input,
		TotalOutputTokens: output,
		TotalTokens:       input + output,
		RequestCount:      requests,
	}
}

// decodeCostStats unmarshals a cost_stats column - the empty and empty-object
// sentinels as well as corrupt JSON decode to the zero value.
func decodeCostStats(raw string) convdomain.SessionCostStats {
	if raw == "" || raw == "{}" {
		return convdomain.SessionCostStats{}
	}

	var stats convdomain.SessionCostStats
	if err := json.Unmarshal([]byte(raw), &stats); err != nil {
		return convdomain.SessionCostStats{}
	}
	return stats
}

// firstModel unmarshals a models column - the empty sentinels, corrupt JSON
// and an empty list yield "".
func firstModel(raw string) string {
	if raw == "" || raw == "[]" {
		return ""
	}

	var models []string
	if err := json.Unmarshal([]byte(raw), &models); err != nil || len(models) == 0 {
		return ""
	}
	return models[0]
}

// decodeTags unmarshals a tags column, discarding any error: the empty
// sentinels and corrupt JSON leave Tags nil, whatever decoded in place stays.
func decodeTags(raw string) []string {
	if raw == "" || raw == "[]" {
		return nil
	}

	var tags []string
	_ = json.Unmarshal([]byte(raw), &tags)
	return tags
}
