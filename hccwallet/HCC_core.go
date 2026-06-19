package hccwallet

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Wallet struct {
	Label   string `json:"label"`
	Secret  string `json:"secret"`  // HCC private key (Bearer)
	Address string `json:"address"` // derived from secret
}

type Config struct {
	APIBaseURL string `json:"api_base_url"`
}

type Store struct {
	Config  Config   `json:"config"`
	Wallets []Wallet `json:"wallets"`
}

// CoinMeta describes a single redeemable coin as returned by /config.
type CoinMeta struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Short    string `json:"short"`
	Homepage string `json:"homepage"`
	MinTip   string `json:"min_tip"`
	MaxTip   string `json:"max_tip"`
}

// ConfigOut mirrors the /config response from the HashCash backend.
type ConfigOut struct {
	ClaimBits         int                 `json:"claim_bits"`
	SignupBits        int                 `json:"signup_bits"`
	StampTTLSec       int                 `json:"stamp_ttl_sec"`
	CooldownSec       int                 `json:"cooldown_sec"`
	DailyEarnCap      int                 `json:"daily_earn_cap"`
	MinRedeemCredits  int                 `json:"min_redeem_credits"`
	RedeemCostCredits int                 `json:"redeem_cost_credits"`
	SupportedCoins    []string            `json:"supported_currencies"`
	Coins             map[string]CoinMeta `json:"coins"`
}

func AddressFromSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])[:40]
}

func defaultStorePath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "."
	}
	return filepath.Join(home, ".hccwallet.json")
}

func LoadStore(path string) (*Store, error) {
	if path == "" {
		path = defaultStorePath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// default
			return &Store{
				Config: Config{
					APIBaseURL: "https://hashcashfaucet.com/api",
				},
				Wallets: []Wallet{},
			}, nil
		}
		return nil, err
	}
	var st Store
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (s *Store) Save(path string) error {
	if path == "" {
		path = defaultStorePath()
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600) // private!
}

func (s *Store) AddWallet(label, secret string) *Wallet {
	addr := AddressFromSecret(secret)
	w := Wallet{
		Label:   label,
		Secret:  secret,
		Address: addr,
	}
	s.Wallets = append(s.Wallets, w)
	return &w
}

// HTTP client
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(baseURL string) *Client {
	// Normalize base URL to avoid trailing slashes like "https://.../"
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type MeResponse struct {
	AccountID     string `json:"account_id"`
	Credits       int    `json:"credits"`
	CooldownUntil int    `json:"cooldown_until"`
	EarnedToday   int    `json:"earned_today"`
	DailyEarnCap  int    `json:"daily_earn_cap"`
	NextSeq       int    `json:"next_seq"`
	ServerTime    int    `json:"server_time"`
}

func (c *Client) GetMe(secret string) (*MeResponse, error) {
	url := c.BaseURL + "/me"
	fmt.Println("DEBUG GetMe URL:", url)
	req, err := http.NewRequest("GET", c.BaseURL+"/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secret)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("GET /me status %d: %s", resp.StatusCode, string(body))
	}

	var out MeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetConfig fetches public configuration and coin metadata from the backend.
func (c *Client) GetConfig() (*ConfigOut, error) {
	url := c.BaseURL + "/config"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("GET /config status %d: %s", resp.StatusCode, string(body))
	}

	var out ConfigOut
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// transfer
type TransferIn struct {
	ToAddress string `json:"to_address"`
	Amount    int    `json:"amount"`
}

type TransferOut struct {
	OK          bool `json:"ok"`
	FromCredits int  `json:"from_credits"`
	ToCredits   int  `json:"to_credits"`
}

type RedeemRequestIn struct {
	TipAddress string `json:"tip_address"`
	Currency   string `json:"currency,omitempty"`
}

type RedeemRequestOut struct {
	OK          bool     `json:"ok"`
	Message     string   `json:"message"`
	CreditsLeft int      `json:"credits_left"`
	MinCredits  int      `json:"min_credits"`
	Currency    string   `json:"currency,omitempty"`
	TipAmount   *float64 `json:"tip_amount,omitempty"`
	Txid        *string  `json:"txid,omitempty"`
	RPCError    *string  `json:"rpc_error,omitempty"`
}

func (c *Client) Transfer(secret string, fromAddr string, toAddr string, amount int) (*TransferOut, error) {
	body := TransferIn{
		ToAddress: toAddr,
		Amount:    amount,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", c.BaseURL+"/transfer", io.NopCloser(bytes.NewReader(buf)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("POST /transfer status %d: %s", resp.StatusCode, string(b))
	}

	var out TransferOut
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, errors.New("transfer returned ok=false")
	}
	return &out, nil
}

// Redeem requests a tip/redeem from the backend.
func (c *Client) Redeem(secret, tipAddress, currency string) (*RedeemRequestOut, error) {
	tipAddress = strings.TrimSpace(tipAddress)
	if tipAddress == "" {
		return nil, errors.New("tip address must not be empty")
	}

	body := RedeemRequestIn{
		TipAddress: tipAddress,
		Currency:   currency,
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", c.BaseURL+"/redeem_request", io.NopCloser(bytes.NewReader(buf)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("POST /redeem_request status %d: %s", resp.StatusCode, string(b))
	}

	var out RedeemRequestOut
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.OK {
		// The backend uses ok=false for logical errors (e.g. insufficient credits).
		return &out, fmt.Errorf("redeem failed: %s", out.Message)
	}
	return &out, nil
}

// multi-wallet support
// MultiWalletSend attempts to send amount from multiple wallets to toAddr.
// It simply calls /transfer repeatedly until amount is depleted.
func MultiWalletSend(c *Client, store *Store, toAddr string, amount int) error {
	if amount <= 0 {
		return fmt.Errorf("amount must be > 0")
	}

	// 1. fetch balances
	type wBal struct {
		W   *Wallet
		Bal int
	}
	var walletsWithBal []wBal
	total := 0

	for i := range store.Wallets {
		w := &store.Wallets[i]
		me, err := c.GetMe(w.Secret)
		if err != nil {
			return fmt.Errorf("failed to get balance for %s: %w", w.Label, err)
		}
		walletsWithBal = append(walletsWithBal, wBal{W: w, Bal: me.Credits})
		total += me.Credits
	}

	if total < amount {
		return fmt.Errorf("insufficient total credits: have %d, need %d", total, amount)
	}

	remaining := amount
	for _, wb := range walletsWithBal {
		if remaining <= 0 {
			break
		}
		if wb.Bal <= 0 {
			continue
		}
		sendNow := wb.Bal
		if sendNow > remaining {
			sendNow = remaining
		}

		_, err := c.Transfer(wb.W.Secret, wb.W.Address, toAddr, sendNow)
		if err != nil {
			return fmt.Errorf("transfer from %s failed: %w", wb.W.Label, err)
		}
		remaining -= sendNow
	}

	if remaining != 0 {
		return fmt.Errorf("internal error: remaining=%d after transfers", remaining)
	}
	return nil
}
