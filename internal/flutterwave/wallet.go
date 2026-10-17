package flutterwave

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ErrRejected means Flutterwave definitively refused the request (a 4xx):
// nothing was created on their side, so the caller may safely undo its own
// side effects. Any other error (timeout, 5xx, unreadable body) is
// ambiguous — the request may or may not have been processed.
var ErrRejected = errors.New("flutterwave: request rejected")

// RejectedError carries Flutterwave's message for a rejected request.
type RejectedError struct{ Message string }

func (e *RejectedError) Error() string { return "flutterwave: request rejected: " + e.Message }
func (e *RejectedError) Unwrap() error { return ErrRejected }

// WalletGateway is the set of Flutterwave operations the wallet, top-up and
// payout features need, kept apart from Gateway so subscription code and its
// fakes are unaffected.
type WalletGateway interface {
	// CreateVirtualAccount generates a one-time bank account the payer
	// transfers exactly `Amount` to.
	CreateVirtualAccount(ctx context.Context, req VirtualAccountRequest) (*VirtualAccount, error)
	// VerifyByReference fetches a collection by OUR tx_ref.
	VerifyByReference(ctx context.Context, txRef string) (*VerifyResult, error)
	VerifyTransaction(ctx context.Context, transactionID string) (*VerifyResult, error)
	ListBanks(ctx context.Context) ([]Bank, error)
	ResolveAccount(ctx context.Context, accountNumber, bankCode string) (accountName string, err error)
	CreateTransfer(ctx context.Context, req TransferRequest) (*Transfer, error)
	GetTransfer(ctx context.Context, transferID string) (*Transfer, error)
}

// VirtualAccountRequest creates a one-time account for a top-up.
type VirtualAccountRequest struct {
	TxRef       string
	AmountKobo  int64
	Email       string
	PhoneNumber string
	FullName    string
	Narration   string
}

// VirtualAccount is the account the payer must transfer to.
type VirtualAccount struct {
	AccountNumber string
	BankName      string
	ExpiresAt     string // as returned (usually "2006-01-02 3:04:05 PM"); parsed by the caller
	AmountKobo    int64  // exact amount Flutterwave expects
	Note          string
}

// Bank is a Nigerian bank from Flutterwave's list.
type Bank struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// TransferRequest is a bank payout.
type TransferRequest struct {
	Reference     string
	BankCode      string
	AccountNumber string
	AmountKobo    int64
	Narration     string
}

// Transfer is a payout as reported by Flutterwave.
type Transfer struct {
	ID         string
	Reference  string
	Status     string // NEW, PENDING, SUCCESSFUL, FAILED
	Message    string
	AmountKobo int64
}

func (c *Client) do(ctx context.Context, method, path string, body interface{}) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("flutterwave: failed to encode request: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return 0, nil, fmt.Errorf("flutterwave: failed to build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.secretKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("flutterwave: request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("flutterwave: failed to read response: %w", err)
	}
	return resp.StatusCode, data, nil
}

type envelope struct {
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Meta    json.RawMessage `json:"meta"`
}

// decode returns the envelope, mapping 4xx to RejectedError and other
// non-2xx to an ambiguous error.
func decode(status int, body []byte) (*envelope, error) {
	var env envelope
	_ = json.Unmarshal(body, &env)
	switch {
	case status >= 200 && status < 300:
		if env.Status != "success" {
			return nil, &RejectedError{Message: env.Message}
		}
		return &env, nil
	case status >= 400 && status < 500:
		msg := env.Message
		if msg == "" {
			msg = fmt.Sprintf("status %d", status)
		}
		return nil, &RejectedError{Message: msg}
	default:
		return nil, fmt.Errorf("flutterwave: unexpected status %d: %s", status, truncate(string(body), 300))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// CreateVirtualAccount implements WalletGateway.
func (c *Client) CreateVirtualAccount(ctx context.Context, req VirtualAccountRequest) (*VirtualAccount, error) {
	payload := map[string]interface{}{
		"tx_ref":       req.TxRef,
		"amount":       formatNaira(req.AmountKobo),
		"currency":     "NGN",
		"email":        req.Email,
		"phone_number": req.PhoneNumber,
		"fullname":     req.FullName,
		"is_permanent": false,
		"narration":    req.Narration,
	}
	status, body, err := c.do(ctx, http.MethodPost, "/charges?type=bank_transfer", payload)
	if err != nil {
		return nil, err
	}
	env, err := decode(status, body)
	if err != nil {
		return nil, err
	}
	var meta struct {
		Authorization struct {
			TransferAccount   string      `json:"transfer_account"`
			TransferBank      string      `json:"transfer_bank"`
			AccountExpiration string      `json:"account_expiration"`
			TransferNote      string      `json:"transfer_note"`
			TransferAmount    json.Number `json:"transfer_amount"`
		} `json:"authorization"`
	}
	if err := json.Unmarshal(env.Meta, &meta); err != nil || meta.Authorization.TransferAccount == "" {
		return nil, fmt.Errorf("flutterwave: charge response had no transfer account")
	}
	amountKobo := req.AmountKobo
	if f, err := meta.Authorization.TransferAmount.Float64(); err == nil && f > 0 {
		amountKobo = int64(f*100 + 0.5)
	}
	return &VirtualAccount{
		AccountNumber: meta.Authorization.TransferAccount, BankName: meta.Authorization.TransferBank,
		ExpiresAt: meta.Authorization.AccountExpiration, AmountKobo: amountKobo, Note: meta.Authorization.TransferNote,
	}, nil
}

func parseVerify(env *envelope) (*VerifyResult, error) {
	var d struct {
		ID       int64   `json:"id"`
		TxRef    string  `json:"tx_ref"`
		Status   string  `json:"status"`
		Amount   float64 `json:"amount"`
		Currency string  `json:"currency"`
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		return nil, fmt.Errorf("flutterwave: failed to decode transaction: %w", err)
	}
	return &VerifyResult{
		FlwTransactionID: fmt.Sprintf("%d", d.ID), TxRef: d.TxRef, Status: d.Status,
		AmountKobo: int64(d.Amount*100 + 0.5), Currency: d.Currency,
	}, nil
}

// VerifyByReference implements WalletGateway.
func (c *Client) VerifyByReference(ctx context.Context, txRef string) (*VerifyResult, error) {
	status, body, err := c.do(ctx, http.MethodGet, "/transactions/verify_by_reference?tx_ref="+url.QueryEscape(txRef), nil)
	if err != nil {
		return nil, err
	}
	env, err := decode(status, body)
	if err != nil {
		return nil, err
	}
	return parseVerify(env)
}

// ListBanks implements WalletGateway.
func (c *Client) ListBanks(ctx context.Context) ([]Bank, error) {
	status, body, err := c.do(ctx, http.MethodGet, "/banks/NG", nil)
	if err != nil {
		return nil, err
	}
	env, err := decode(status, body)
	if err != nil {
		return nil, err
	}
	var banks []Bank
	if err := json.Unmarshal(env.Data, &banks); err != nil {
		return nil, fmt.Errorf("flutterwave: failed to decode banks: %w", err)
	}
	return banks, nil
}

// ResolveAccount implements WalletGateway.
func (c *Client) ResolveAccount(ctx context.Context, accountNumber, bankCode string) (string, error) {
	status, body, err := c.do(ctx, http.MethodPost, "/accounts/resolve",
		map[string]string{"account_number": accountNumber, "account_bank": bankCode})
	if err != nil {
		return "", err
	}
	env, err := decode(status, body)
	if err != nil {
		return "", err
	}
	var d struct {
		AccountName string `json:"account_name"`
	}
	if err := json.Unmarshal(env.Data, &d); err != nil || strings.TrimSpace(d.AccountName) == "" {
		return "", &RejectedError{Message: "account could not be resolved"}
	}
	return strings.TrimSpace(d.AccountName), nil
}

func parseTransfer(env *envelope) (*Transfer, error) {
	var d struct {
		ID              int64   `json:"id"`
		Reference       string  `json:"reference"`
		Status          string  `json:"status"`
		CompleteMessage string  `json:"complete_message"`
		Amount          float64 `json:"amount"`
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		return nil, fmt.Errorf("flutterwave: failed to decode transfer: %w", err)
	}
	return &Transfer{
		ID: fmt.Sprintf("%d", d.ID), Reference: d.Reference, Status: strings.ToUpper(d.Status),
		Message: d.CompleteMessage, AmountKobo: int64(d.Amount*100 + 0.5),
	}, nil
}

// CreateTransfer implements WalletGateway.
func (c *Client) CreateTransfer(ctx context.Context, req TransferRequest) (*Transfer, error) {
	status, body, err := c.do(ctx, http.MethodPost, "/transfers", map[string]interface{}{
		"account_bank":   req.BankCode,
		"account_number": req.AccountNumber,
		"amount":         formatNaira(req.AmountKobo),
		"narration":      req.Narration,
		"currency":       "NGN",
		"reference":      req.Reference,
		"debit_currency": "NGN",
	})
	if err != nil {
		return nil, err
	}
	env, err := decode(status, body)
	if err != nil {
		return nil, err
	}
	return parseTransfer(env)
}

// GetTransfer implements WalletGateway.
func (c *Client) GetTransfer(ctx context.Context, transferID string) (*Transfer, error) {
	status, body, err := c.do(ctx, http.MethodGet, "/transfers/"+url.PathEscape(transferID), nil)
	if err != nil {
		return nil, err
	}
	env, err := decode(status, body)
	if err != nil {
		return nil, err
	}
	return parseTransfer(env)
}

func formatNaira(kobo int64) string {
	return fmt.Sprintf("%d.%02d", kobo/100, kobo%100)
}
