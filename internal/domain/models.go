package domain

import "encoding/json"

type Account struct {
	ID      int64 `json:"id"`
	Balance int64 `json:"balance"`
}

type TransferRequest struct {
	FromAccountID int64 `json:"from_account_id"`
	ToAccountID   int64 `json:"to_account_id"`
	Amount        int64 `json:"amount"`
}

type Transfer struct {
	ID            int64  `json:"id"`
	FromAccountID int64  `json:"from_account_id"`
	ToAccountID   int64  `json:"to_account_id"`
	Amount        int64  `json:"amount"`
	Status        string `json:"status"`
}

type LedgerEntry struct {
	AccountID int64 `json:"account_id"`
	Delta     int64 `json:"delta"`
}

type TransferResponse struct {
	Transfer Transfer      `json:"transfer"`
	Entries  []LedgerEntry `json:"entries"`
}

// IdempotencyPayload represents the stored state of a request
type IdempotencyPayload struct {
	Status         string          `json:"status"`
	ResponseBody   json.RawMessage `json:"response_body,omitempty"`
	ResponseStatus int             `json:"response_status,omitempty"`
}
