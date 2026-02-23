package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/Hashcash-PoW-Faucet/HCC-client/hccwallet"
)

// AppState holds the UI and wallet state.
type AppState struct {
	store     *hccwallet.Store
	storePath string
	client    *hccwallet.Client

	// Caches for balances (per address index)
	meMu     sync.Mutex
	meCache  map[int]*hccwallet.MeResponse
	selected int

	// Cached /config result so we do not hit the backend all the time.
	configMu sync.Mutex
	config   *hccwallet.ConfigOut
}

func (s *AppState) computeTotalCredits() int {
	s.meMu.Lock()
	defer s.meMu.Unlock()
	total := 0
	for _, me := range s.meCache {
		if me != nil {
			total += me.Credits
		}
	}
	return total
}

func (s *AppState) refreshAllBalancesAsync(
	statusLabel *widget.Label,
	totalLabel *widget.Label,
	updateDetails func(idx int),
	notify bool,
) {
	if len(s.store.Wallets) == 0 {
		statusLabel.SetText("No addresses available.")
		return
	}
	statusLabel.SetText("Refreshing balances...")
	go func() {
		newCache := make(map[int]*hccwallet.MeResponse)
		var firstErr error
		for i := range s.store.Wallets {
			wal := &s.store.Wallets[i]
			me, err := s.client.GetMe(wal.Secret)
			if err != nil {
				fmt.Printf("GetMe error for address %s (%s): %v\n", wal.Label, wal.Address, err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			newCache[i] = me
		}

		s.meMu.Lock()
		s.meCache = newCache
		s.meMu.Unlock()

		fyne.Do(func() {
			if notify {
				fyne.CurrentApp().SendNotification(&fyne.Notification{
					Title:   "HCC Client",
					Content: "Balance refresh complete.",
				})
			}

			if s.selected >= 0 {
				updateDetails(s.selected)
			}

			total := s.computeTotalCredits()
			totalLabel.SetText(fmt.Sprintf("Total credits: %d", total))

			if firstErr != nil {
				statusLabel.SetText(fmt.Sprintf("Refresh done with error: %v", firstErr))
			} else {
				statusLabel.SetText("Refresh done.")
			}
		})
	}()
}

func main() {
	// Load store (.hccwallet.json in home by default)
	store, err := hccwallet.LoadStore("")
	if err != nil {
		fmt.Println("Failed to load store:", err)
		return
	}

	// Build client from config
	baseURL := store.Config.APIBaseURL
	if baseURL == "" {
		baseURL = "https://hashcash-pow-faucet.dynv6.net/api" // fallback
	}
	client := hccwallet.NewClient(baseURL)

	state := &AppState{
		store:     store,
		storePath: "",
		client:    client,
		meCache:   make(map[int]*hccwallet.MeResponse),
		selected:  -1,
	}

	a := app.New()
	a.Settings().SetTheme(theme.DarkTheme())
	w := a.NewWindow("HCC Address Manager")

	w.SetContent(state.buildUI(w))
	w.Resize(fyne.NewSize(900, 500))
	w.ShowAndRun()
}

// buildUI builds the main layout: adresses list on the left, details on the right.
func (s *AppState) buildUI(w fyne.Window) fyne.CanvasObject {
	// --- Left: Adress list ---
	list := widget.NewList(
		func() int {
			return len(s.store.Wallets)
		},
		func() fyne.CanvasObject {
			lbl := widget.NewLabel("")
			lbl.Truncation = fyne.TextTruncateEllipsis
			return lbl
		},
		func(i widget.ListItemID, obj fyne.CanvasObject) {
			lbl := obj.(*widget.Label)
			if i >= 0 && i < len(s.store.Wallets) {
				wal := s.store.Wallets[i]
				shortAddr := wal.Address
				if len(shortAddr) > 12 {
					shortAddr = shortAddr[:12] + "…"
				}
				lbl.SetText(fmt.Sprintf("%s  (%s)", wal.Label, shortAddr))
			} else {
				lbl.SetText("")
			}
		},
	)

	// Right: details
	totalLabel := widget.NewLabel("Total credits: -")
	addrLabel := widget.NewLabel("Address: -")
	balanceLabel := widget.NewLabel("Credits: -")
	earnedLabel := widget.NewLabel("Earned today: - / -")
	cooldownLabel := widget.NewLabel("Cooldown: -")
	statusLabel := widget.NewLabel("") // for small status messages

	updateDetails := func(idx int) {
		if idx < 0 || idx >= len(s.store.Wallets) {
			addrLabel.SetText("Address: -")
			balanceLabel.SetText("Credits: -")
			earnedLabel.SetText("Earned today: - / -")
			cooldownLabel.SetText("Cooldown: -")
			statusLabel.SetText("")
			return
		}
		wal := s.store.Wallets[idx]
		addrLabel.SetText("Address: " + wal.Address)

		s.meMu.Lock()
		me := s.meCache[idx]
		s.meMu.Unlock()

		if me == nil {
			balanceLabel.SetText("Credits: (click Refresh)")
			earnedLabel.SetText("Earned today: ? / ?")
			cooldownLabel.SetText("Cooldown: ?")
		} else {
			balanceLabel.SetText(fmt.Sprintf("Credits: %d", me.Credits))
			earnedLabel.SetText(fmt.Sprintf("Earned today: %d / %d", me.EarnedToday, me.DailyEarnCap))
			if me.CooldownUntil > me.ServerTime {
				cool := me.CooldownUntil - me.ServerTime
				cooldownLabel.SetText(fmt.Sprintf("Cooldown: %d s remaining", cool))
			} else {
				cooldownLabel.SetText("Cooldown: none")
			}
		}
	}

	list.OnSelected = func(id widget.ListItemID) {
		s.selected = int(id)
		updateDetails(s.selected)
	}

	// --- Buttons on the right ---

	// Refresh balances for all Adresses.
	refreshBtn := widget.NewButton("Refresh balances", func() {
		s.refreshAllBalancesAsync(statusLabel, totalLabel, updateDetails, true)
	})

	// Add address
	addBtn := widget.NewButton("Add address", func() {
		labelEntry := widget.NewEntry()
		labelEntry.SetPlaceHolder("Label (e.g. 'Browser miner')")
		secretEntry := widget.NewPasswordEntry()
		secretEntry.SetPlaceHolder("private key")

		form := &widget.Form{
			Items: []*widget.FormItem{
				{Text: "Label", Widget: labelEntry},
				{Text: "Secret", Widget: secretEntry},
			},
			OnSubmit: func() {
				label := strings.TrimSpace(labelEntry.Text)
				secret := strings.TrimSpace(secretEntry.Text)
				if label == "" || secret == "" {
					dialog.ShowError(fmt.Errorf("label and private key must not be empty"), w)
					return
				}
				s.store.AddWallet(label, secret)
				if err := s.store.Save(s.storePath); err != nil {
					dialog.ShowError(fmt.Errorf("failed to save store: %w", err), w)
				}
				list.Refresh()
				statusLabel.SetText("Address added.")
			},
		}

		d := dialog.NewCustom("Add HCC address", "Close", form, w)
		d.Resize(fyne.NewSize(420, 260))
		d.Show()
	})

	// Delete selected address
	delBtn := widget.NewButton("Remove address", func() {
		if s.selected < 0 || s.selected >= len(s.store.Wallets) {
			statusLabel.SetText("No wallet selected.")
			return
		}
		idx := s.selected
		wal := s.store.Wallets[idx]

		dialog.ShowConfirm(
			"Delete address",
			fmt.Sprintf("Delete address '%s' (%s…)?\nThis does NOT delete the address on the server, only from this app.", wal.Label, wal.Address[:8]),
			func(ok bool) {
				if !ok {
					return
				}
				// Remove from slice
				s.store.Wallets = append(s.store.Wallets[:idx], s.store.Wallets[idx+1:]...)
				if err := s.store.Save(s.storePath); err != nil {
					dialog.ShowError(fmt.Errorf("failed to save store: %w", err), w)
				}
				s.meMu.Lock()
				delete(s.meCache, idx)
				s.meMu.Unlock()
				s.selected = -1
				list.Refresh()
				updateDetails(-1)
				statusLabel.SetText("Address deleted.")
			},
			w,
		)
	})

	// Edit selected address
	editBtn := widget.NewButton("Edit address", func() {
		if s.selected < 0 || s.selected >= len(s.store.Wallets) {
			statusLabel.SetText("No address selected.")
			return
		}
		// current address
		wal := &s.store.Wallets[s.selected]

		labelEntry := widget.NewEntry()
		labelEntry.SetText(wal.Label)

		secretEntry := widget.NewPasswordEntry()
		secretEntry.SetText(wal.Secret)

		form := &widget.Form{
			Items: []*widget.FormItem{
				{Text: "Label", Widget: labelEntry},
				{Text: "Secret", Widget: secretEntry},
			},
			OnSubmit: func() {
				newLabel := strings.TrimSpace(labelEntry.Text)
				newSecret := strings.TrimSpace(secretEntry.Text)

				if newLabel == "" || newSecret == "" {
					dialog.ShowError(fmt.Errorf("label and private key must not be empty"), w)
					return
				}

				// Update in store
				wal.Label = newLabel
				wal.Secret = newSecret

				if err := s.store.Save(s.storePath); err != nil {
					dialog.ShowError(fmt.Errorf("failed to save store: %w", err), w)
					return
				}

				// Invalidate cached /me info, it might belong to old secret
				s.meMu.Lock()
				delete(s.meCache, s.selected)
				s.meMu.Unlock()

				list.Refresh()
				updateDetails(s.selected)
				statusLabel.SetText("Address updated.")
			},
		}

		d := dialog.NewCustom("Edit HCC address", "Close", form, w)
		d.Resize(fyne.NewSize(420, 260))
		d.Show()
	})

	// Send from selected address
	sendBtn := widget.NewButton("Send HCC", func() {
		if s.selected < 0 || s.selected >= len(s.store.Wallets) {
			statusLabel.SetText("No address selected.")
			return
		}
		wal := s.store.Wallets[s.selected]

		toEntry := widget.NewEntry()
		toEntry.SetPlaceHolder("Destination HCC address")
		amountEntry := widget.NewEntry()
		amountEntry.SetPlaceHolder("Amount of credits")

		form := &widget.Form{
			Items: []*widget.FormItem{
				{Text: "From address", Widget: widget.NewLabel(wal.Label + " (" + wal.Address[:8] + "…)")},
				{Text: "To address", Widget: toEntry},
				{Text: "Amount", Widget: amountEntry},
			},
			OnSubmit: func() {
				to := strings.TrimSpace(toEntry.Text)
				amtStr := strings.TrimSpace(amountEntry.Text)
				if to == "" || amtStr == "" {
					dialog.ShowError(fmt.Errorf("address and amount must not be empty"), w)
					return
				}
				amt, err := strconv.Atoi(amtStr)
				if err != nil || amt <= 0 {
					dialog.ShowError(fmt.Errorf("invalid amount"), w)
					return
				}

				go func() {
					_, err := s.client.Transfer(wal.Secret, wal.Address, to, amt)
					fyne.Do(func() {
						if err != nil {
							dialog.ShowError(fmt.Errorf("transfer failed: %w", err), w)
							return
						}
						statusLabel.SetText("Transfer successful.")

						// Refresh balances for this address (best-effort)
						go func() {
							me, err := s.client.GetMe(wal.Secret)
							if err == nil {
								s.meMu.Lock()
								s.meCache[s.selected] = me
								s.meMu.Unlock()
								fyne.Do(func() {
									if s.selected >= 0 {
										updateDetails(s.selected)
									}
								})
							}
						}()
					})
				}()
			},
		}

		d := dialog.NewCustom("Send HCC", "Close", form, w)
		d.Resize(fyne.NewSize(420, 260))
		d.Show()
	})

	// Redeem from selected address
	redeemBtn := widget.NewButton("Redeem", func() {
		if s.selected < 0 || s.selected >= len(s.store.Wallets) {
			statusLabel.SetText("No address selected.")
			return
		}
		wal := s.store.Wallets[s.selected]

		addrEntry := widget.NewEntry()
		addrEntry.SetPlaceHolder("Tip address (recipient on target chain)")

		// Try to get supported currencies from cached /config or backend
		var coinSymbols []string
		s.configMu.Lock()
		cfg := s.config
		s.configMu.Unlock()

		if cfg == nil {
			if remoteCfg, err := s.client.GetConfig(); err != nil {
				// Just show a status message; we will fall back to manual entry.
				statusLabel.SetText(fmt.Sprintf("Could not fetch config: %v (enter currency manually).", err))
			} else {
				s.configMu.Lock()
				s.config = remoteCfg
				s.configMu.Unlock()
				cfg = remoteCfg
			}
		}
		if cfg != nil {
			coinSymbols = append(coinSymbols, cfg.SupportedCoins...)
			sort.Strings(coinSymbols)
		}

		var currencyWidget fyne.CanvasObject
		var getCurrency func() string

		if len(coinSymbols) > 0 {
			curSelect := widget.NewSelect(coinSymbols, nil)
			curSelect.PlaceHolder = "Select currency"
			currencyWidget = curSelect
			getCurrency = func() string {
				return strings.TrimSpace(curSelect.Selected)
			}
		} else {
			curEntry := widget.NewEntry()
			curEntry.SetPlaceHolder("Currency symbol (e.g. GRS, SLM, LRGK)")
			currencyWidget = curEntry
			getCurrency = func() string {
				return strings.TrimSpace(curEntry.Text)
			}
		}

		form := &widget.Form{
			Items: []*widget.FormItem{
				{Text: "From address", Widget: widget.NewLabel(wal.Label + " (" + wal.Address[:8] + "…)")},
				{Text: "Tip address", Widget: addrEntry},
				{Text: "Currency", Widget: currencyWidget},
			},
			OnSubmit: func() {
				tipAddr := strings.TrimSpace(addrEntry.Text)
				currency := getCurrency()
				if tipAddr == "" || currency == "" {
					dialog.ShowError(fmt.Errorf("tip address and currency must not be empty"), w)
					return
				}
				go func() {
					out, err := s.client.Redeem(wal.Secret, tipAddr, currency)
					fyne.Do(func() {
						if err != nil {
							msg := ""
							if out != nil {
								msg = out.Message
							}
							dialog.ShowError(fmt.Errorf("redeem failed: %w\n%s", err, msg), w)
							return
						}
						// Success path
						info := out.Message
						if out.TipAmount != nil && out.Txid != nil {
							info = fmt.Sprintf("%s\nTip: %.8f %s\nTXID: %s",
								out.Message, *out.TipAmount, out.Currency, *out.Txid)
						}
						dialog.ShowInformation("Redeem", info, w)
						statusLabel.SetText("Redeem request sent.")
					})
				}()
			},
		}

		d := dialog.NewCustom("Redeem", "Close", form, w)
		d.Resize(fyne.NewSize(420, 260))
		d.Show()
	})

	walletButtonRow := container.NewGridWithColumns(2,
		addBtn,
		refreshBtn,
	)

	addrActionsRow1 := container.NewGridWithColumns(2,
		sendBtn,
		redeemBtn,
	)

	addrActionsRow2 := container.NewGridWithColumns(2,
		editBtn,
		delBtn,
	)

	infoBox := container.NewVBox(
		totalLabel,
		addrLabel,
		balanceLabel,
		earnedLabel,
		cooldownLabel,
	)

	infoCard := widget.NewCard("Address overview", "", infoBox)

	actionsBox := container.NewVBox(
		widget.NewLabelWithStyle("Address management", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		walletButtonRow,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Selected address actions", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		addrActionsRow1,
		addrActionsRow2,
	)

	actionsCard := widget.NewCard("Actions", "Manage your HCC addresses and funds", actionsBox)

	statusBar := container.NewHBox(statusLabel)

	mainContent := container.NewVBox(
		infoCard,
		actionsCard,
	)

	details := container.NewBorder(
		nil,         // top
		statusBar,   // bottom
		nil,         // left
		nil,         // right
		mainContent, // center
	)

	split := container.NewHSplit(
		container.NewBorder(nil, nil, nil, nil, list),
		details,
	)
	split.SetOffset(0.3)

	s.refreshAllBalancesAsync(statusLabel, totalLabel, updateDetails, false)

	return split
}
