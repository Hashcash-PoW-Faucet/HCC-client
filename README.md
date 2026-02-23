# HCC Desktop Client (GUI)

A simple cross‑platform GUI client for managing multiple Hashcash (HCC) addresses.

The app talks to a Hashcash PoW faucet backend via its public HTTP API
(`/me`, `/transfer`, `/redeem_request`, `/config`) and lets you:

- Import one or more HCC wallets (via private key)
- See the balance of each wallet
- See your **total HCC balance** across all wallets
- Refresh balances with a single click
- Send HCC from a selected wallet
- Create redeem requests for payout coins
- Edit labels / keys and remove wallets

> ⚠️ **Important:** This is an experimental client.  
> Always back up your HCC private keys before using it.

---

## Project layout

```text
HCC-client/
├── go.mod                 # Go module definition
├── go.sum                 # Go dependencies
├── hccwallet/
│   └── HCC_core.go        # Core HTTP client for the HashCash backend API
└── cmd/
    └── hccwalletgui/
        └── main.go        # Fyne-based desktop GUI
```

The GUI uses the `hccwallet` package (HCC_core.go) for all network calls.

---

## Requirements

- Go 1.21+ (recommended: latest stable)
- A running HashCash backend (the PoW faucet API)
- The API base URL configured in `hccwallet/HCC_core.go`  
- Fyne dependencies for GUI builds

### Fyne system dependencies

On **Debian/Ubuntu** for example:

```bash
sudo apt update
sudo apt install -y build-essential libgl1-mesa-dev xorg-dev
```

On **macOS** (with Xcode command line tools):
- Nothing special beyond Go – Fyne uses the native toolkit.

For other platforms see the Fyne docs: https://developer.fyne.io/started/

---

## Building the GUI

From the repository root:

```bash
# 1) Make sure dependencies are up‑to‑date
go mod tidy

# 2) Build the GUI binary
go build -o hccwalletgui ./cmd/hccwalletgui
```

This will produce a binary named `hccwalletgui` (or `hccwalletgui.exe` on Windows)
in the project root.

### Example: run on the same machine

```bash
./hccwalletgui
```

### Example: cross‑compile for Windows (from macOS/Linux)

```bash
GOOS=windows GOARCH=amd64 go build -o hccwalletgui.exe ./cmd/hccwalletgui
```

You can then ship the resulting `hccwalletgui.exe` to Windows users.

---

## Configuration

The backend API base URL is hard‑coded in `HCC_core.go` in the `hccwallet`
package. Look for a constant like:

```go
const defaultAPIBase = "https://your-hashcash-host/api"
```

Adjust this to point to your own Hashcash faucet backend, then rebuild.

> All wallets in the GUI talk to the same backend. If you run multiple
> backends, you would build one binary per backend URL.

---

## Usage

1. **Start the app**  
   Run `hccwalletgui`. On first start the wallet list will be empty.

2. **Add a wallet**  
   Click **“Add address”** and enter:
   - A label (e.g. `main miner`, `discord bot`, …)
   - The HCC private key
   - (Optional) an address override, otherwise the address is derived via the API

   The wallet will appear in the list on the left.

3. **Refresh balances**  
   Click **“Refresh all”** (or the refresh button) to query `/me` for each wallet.
   - Each row shows the current HCC balance for that address.
   - The **Total balance** at the top sums all wallets.

4. **Send HCC**  
   Select a wallet, click **“Send”** and enter:
   - Destination HCC address
   - Amount of HCC to send

   The client calls the backend `/transfer` endpoint for the selected wallet.

5. **Redeem HCC (optional)**  
   Select a wallet, click **“Redeem”** and choose:
   - The payout currency from a dropdown (based on `/config`)
   - A payout address on that chain

   The client calls `/redeem_request` for the selected wallet.  
   The backend decides whether and when to send an actual payout.

6. **Edit or remove wallets**  
   - **Edit** lets you change label and private key.
   - **Remove** deletes the wallet entry from the local store (it does **not**
     touch the backend account – HCC credits remain on the backend).

---

## Safety notes

- This client **never** stores keys on a server; everything stays on your machine.
- The backend sees only hashes / derived account IDs and signed requests via its API.
- There is **no guarantee of crypto payouts** for redeems; the faucet is strictly
  discretionary, just like on the web interface.

If you run your own instance, consider keeping your backend behind HTTPS and
rate‑limiting access.

---

## License

MIT
