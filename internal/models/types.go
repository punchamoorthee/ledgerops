package models

import "encoding/json"

// Account represents a user's ledger account.
type Account struct {
	ID      int64 `json:"id"`
	Balance int64 `json:"balance"`
}

// TransferRequest is the payload from the client.
type TransferRequest struct {
	FromAccountID int64 `json:"from_account_id"`
	ToAccountID   int64 `json:"to_account_id"`
	Amount        int64 `json:"amount"`
}

// Transfer represents the immutable record of intent.
type Transfer struct {
	ID            int64  `json:"id"`
	FromAccountID int64  `json:"from_account_id"`
	ToAccountID   int64  `json:"to_account_id"`
	Amount        int64  `json:"amount"`
	Status        string `json:"status"`
}

// LedgerEntry represents one leg of the double-entry accounting.
type LedgerEntry struct {
	AccountID int64 `json:"account_id"`
	Delta     int64 `json:"delta"`
}

// TransferResponse is the canonical response structure.
type TransferResponse struct {
	Transfer Transfer      `json:"transfer"`
	Entries  []LedgerEntry `json:"entries"`
}

// IdempotencyRecord holds the state of a request key.
type IdempotencyRecord struct {
	Key            string
	RequestHash    string
	Status         string
	ResponseBody   json.RawMessage
	ResponseStatus int
}
