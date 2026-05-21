package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type Evaluator struct {
	opaURL string
}

func NewEvaluator(ctx context.Context, opaURL string) (*Evaluator, error) {
	if opaURL == "" {
		return nil, fmt.Errorf("opaURL cannot be empty")
	}
	return &Evaluator{opaURL: opaURL}, nil
}

type OPARequest struct {
	Input map[string]interface{} `json:"input"`
}

type OPAResponse struct {
	Result struct {
		Allow bool     `json:"allow"`
		Deny  []string `json:"deny"`
	} `json:"result"`
}

func (e *Evaluator) Evaluate(ctx context.Context, prompt string) (bool, string, error) {
	reqBody := OPARequest{
		Input: map[string]interface{}{
			"message": prompt,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return false, "", err
	}

	resp, err := http.Post(e.opaURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, "", fmt.Errorf("OPA returned status: %d", resp.StatusCode)
	}

	var opaResp OPAResponse
	if err := json.NewDecoder(resp.Body).Decode(&opaResp); err != nil {
		return false, "", err
	}

	reason := ""
	if len(opaResp.Result.Deny) > 0 {
		reason = opaResp.Result.Deny[0]
	}

	return opaResp.Result.Allow, reason, nil
}
