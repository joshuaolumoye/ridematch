// Package flutterwave is a minimal client for the two Flutterwave v3 REST
// endpoints this platform needs: creating a hosted-checkout payment link,
// and independently re-verifying a transaction server-side before trusting
// a webhook. It intentionally does not depend on Flutterwave's own Go SDK
// — the surface area needed here is small enough that a direct net/http
// client keeps the dependency footprint (and go.mod) minimal.
package flutterwave

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultBaseURL = "https://api.flutterwave.com/v3"

// Gateway is the subset of Flutterwave operations this app depends on,
// expressed as an interface (matching this codebase's repository-pattern
// convention) so PaymentService can be tested against a fake gateway
// without making real network calls.
type Gateway interface {
	// InitiatePayment creates a hosted-checkout session and returns the
	// URL to redirect the payer to.
	InitiatePayment(ctx context.Context, req InitiatePaymentRequest) (paymentLink string, err error)

	// VerifyTransaction independently re-fetches a transaction's status
	// directly from Flutterwave using the secret key — never trust a
	// webhook body alone; always verify server-to-server before crediting
	// anything.
	VerifyTransaction(ctx context.Context, transactionID string) (*VerifyResult, error)
}

// Client is the concrete Gateway implementation backed by Flutterwave's
// real API.
type Client struct {
	secretKey  string
	baseURL    string
	httpClient *http.Client
}

// NewClient constructs a Flutterwave client authenticated with the given
// secret key (starts with "FLWSECK_" — from the Flutterwave dashboard).
func NewClient(secretKey string) *Client {
	return &Client{
		secretKey: secretKey,
		baseURL:   defaultBaseURL,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// WithBaseURL overrides the API base URL (default
// https://api.flutterwave.com/v3). Not needed in normal operation — this
// exists for pointing the client at a local stub server in tests/ops
// tooling, since Flutterwave doesn't offer a separate sandbox hostname for
// this API version (test vs. live mode is controlled by which secret key
// you use, "FLWSECK_TEST-..." vs "FLWSECK-...").
func (c *Client) WithBaseURL(baseURL string) *Client {
	c.baseURL = baseURL
	return c
}

// Customer is the payer's identifying details, shown on the Flutterwave
// checkout page and used for their receipt.
type Customer struct {
	Email       string `json:"email"`
	PhoneNumber string `json:"phonenumber,omitempty"`
	Name        string `json:"name,omitempty"`
}

// Customizations controls branding on the hosted checkout page.
type Customizations struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// InitiatePaymentRequest mirrors the subset of Flutterwave's
// POST /v3/payments body this app uses.
type InitiatePaymentRequest struct {
	// TxRef is OUR reference (must be unique per attempt) — this is the
	// value the webhook and the verify-transaction response both echo
	// back, and it's how PaymentService looks up the corresponding
	// PaymentTransaction row.
	TxRef          string            `json:"tx_ref"`
	Amount         string            `json:"amount"` // main currency unit (Naira), not kobo
	Currency       string            `json:"currency"`
	RedirectURL    string            `json:"redirect_url"`
	Customer       Customer          `json:"customer"`
	Customizations *Customizations   `json:"customizations,omitempty"`
	Meta           map[string]string `json:"meta,omitempty"`
}

type initiatePaymentResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    struct {
		Link string `json:"link"`
	} `json:"data"`
}

// InitiatePayment implements Gateway.
func (c *Client) InitiatePayment(ctx context.Context, req InitiatePaymentRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("flutterwave: failed to encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/payments", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("flutterwave: failed to build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.secretKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("flutterwave: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("flutterwave: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("flutterwave: unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed initiatePaymentResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("flutterwave: failed to decode response: %w", err)
	}
	if parsed.Status != "success" || parsed.Data.Link == "" {
		return "", fmt.Errorf("flutterwave: payment initiation was not successful: %s", parsed.Message)
	}

	return parsed.Data.Link, nil
}

// VerifyResult is the normalized outcome of a transaction-verify call.
type VerifyResult struct {
	FlwTransactionID string
	TxRef            string
	Status           string // "successful", "failed", "pending", etc.
	AmountKobo       int64
	Currency         string
}

type verifyTransactionResponse struct {
	Status string `json:"status"`
	Data   struct {
		ID       int64   `json:"id"`
		TxRef    string  `json:"tx_ref"`
		FlwRef   string  `json:"flw_ref"`
		Status   string  `json:"status"`
		Amount   float64 `json:"amount"`
		Currency string  `json:"currency"`
	} `json:"data"`
}

// VerifyTransaction implements Gateway.
func (c *Client) VerifyTransaction(ctx context.Context, transactionID string) (*VerifyResult, error) {
	url := fmt.Sprintf("%s/transactions/%s/verify", c.baseURL, transactionID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("flutterwave: failed to build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.secretKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("flutterwave: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("flutterwave: failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("flutterwave: unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed verifyTransactionResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("flutterwave: failed to decode response: %w", err)
	}

	return &VerifyResult{
		FlwTransactionID: fmt.Sprintf("%d", parsed.Data.ID),
		TxRef:            parsed.Data.TxRef,
		Status:           parsed.Data.Status,
		AmountKobo:       int64(parsed.Data.Amount * 100),
		Currency:         parsed.Data.Currency,
	}, nil
}
