package governance

import (
	"context"
	"fmt"
	"strings"

	"github.com/open-policy-agent/opa/v1/rego"
)

type Evaluator struct {
	query rego.PreparedEvalQuery
}

// NewEvaluator creates a new OPA evaluator loaded with the given rego policy.
func NewEvaluator(ctx context.Context, policyPath string) (*Evaluator, error) {
	r := rego.New(
		rego.Query("data.chat"),
		rego.Load([]string{policyPath}, nil),
	)

	query, err := r.PrepareForEval(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare rego query: %w", err)
	}

	return &Evaluator{query: query}, nil
}

// Evaluate checks if the message is allowed and returns false with a reason if denied.
func (e *Evaluator) Evaluate(ctx context.Context, message string) (bool, string, error) {
	input := map[string]any{
		"message": message,
	}

	results, err := e.query.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return false, "", fmt.Errorf("failed to evaluate policy: %w", err)
	}

	if len(results) == 0 {
		return false, "no results from policy evaluation", nil
	}

	// The policy package is 'chat', so results[0].Expressions[0].Value is a map containing 'allow' and 'deny'
	resultMap, ok := results[0].Expressions[0].Value.(map[string]any)
	if !ok {
		return false, "unexpected policy evaluation result format", nil
	}

	allow, ok := resultMap["allow"].(bool)
	if !ok {
		return false, "allow rule missing or not a boolean", nil
	}

	if allow {
		return true, "", nil
	}

	// Get deny reasons if any
	denyReasons, ok := resultMap["deny"].([]any)
	var reasons []string
	if ok {
		for _, r := range denyReasons {
			if strReason, ok := r.(string); ok {
				reasons = append(reasons, strReason)
			}
		}
	}

	if len(reasons) > 0 {
		return false, strings.Join(reasons, ", "), nil
	}

	return false, "policy denied the request without providing a reason", nil
}
