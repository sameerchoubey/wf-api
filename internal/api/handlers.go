package api

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"wealthflow/backend/internal/auth"
	"wealthflow/backend/internal/config"
	"wealthflow/backend/internal/middleware"
	"wealthflow/backend/internal/models"
	"wealthflow/backend/internal/service"
	"wealthflow/backend/internal/store"
)

type Handler struct {
	Config   config.Config
	Store    *store.Store
	Snapshot *service.Snapshot
	Rates    *service.Rates
	Crypto   *service.CryptoPrices
	MF       *service.MFNavs
	FX       *service.FXRevaluer
}

// RunDailyJobs is the wake-and-work endpoint for the external scheduler:
// the Fly machine sleeps through the in-process crons, so a GitHub Actions
// cron POSTs here nightly and the work happens inside this request —
// prices/NAVs refresh (which revalues portfolios), then a snapshot is
// written for every user. Guarded by a shared token; disabled when unset.
func (h *Handler) RunDailyJobs(w http.ResponseWriter, r *http.Request) {
	if h.Config.JobToken == "" {
		WriteError(w, http.StatusNotFound, "Not found")
		return
	}
	token := r.Header.Get("X-Job-Token")
	if subtle.ConstantTimeCompare([]byte(token), []byte(h.Config.JobToken)) != 1 {
		WriteError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	ctx := r.Context()
	// Fresh market data first so the snapshots capture today's values.
	// FX comes first: the crypto and FX revaluations convert with it.
	if err := h.Rates.Refresh(ctx); err != nil {
		WriteError(w, http.StatusInternalServerError, "fx rates refresh failed")
		return
	}
	if err := h.Crypto.Refresh(ctx); err != nil {
		WriteError(w, http.StatusInternalServerError, "crypto refresh failed")
		return
	}
	if err := h.MF.Refresh(ctx); err != nil {
		WriteError(w, http.StatusInternalServerError, "mf refresh failed")
		return
	}
	if err := h.FX.Revalue(ctx); err != nil {
		WriteError(w, http.StatusInternalServerError, "fx revaluation failed")
		return
	}
	if err := h.Snapshot.CreateSnapshotsForAllUsers(ctx); err != nil {
		WriteError(w, http.StatusInternalServerError, "snapshots failed")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// MFSearch proxies mutual-fund scheme search for the holdings editor.
func (h *Handler) MFSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 3 {
		WriteJSON(w, http.StatusOK, []models.MFSearchResult{})
		return
	}
	results, err := h.MF.Search(r.Context(), q)
	if err != nil {
		WriteError(w, http.StatusBadGateway, "Fund search is unavailable right now")
		return
	}
	if results == nil {
		results = []models.MFSearchResult{}
	}
	WriteJSON(w, http.StatusOK, results)
}

// MFNav returns one scheme's latest NAV (cached, fetched on demand) so
// the holdings editor can preview values before saving.
func (h *Handler) MFNav(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		WriteError(w, http.StatusBadRequest, "code required")
		return
	}
	doc, err := h.MF.NavFor(r.Context(), code)
	if err != nil {
		WriteError(w, http.StatusBadGateway, "Could not fetch NAV for this fund")
		return
	}
	WriteJSON(w, http.StatusOK, doc)
}

// priceMFAsset validates a mutual-fund portfolio and fills in the
// server-computed value; client-sent values are ignored.
func (h *Handler) priceMFAsset(ctx context.Context, a *models.Asset) error {
	if len(a.MFHoldings) == 0 {
		return fmt.Errorf("add at least one fund")
	}
	inr, normalized, err := h.MF.ValuePortfolio(ctx, a.MFHoldings)
	if err != nil {
		return err
	}
	a.MFHoldings = normalized
	a.CurrentValue = inr
	return nil
}

// priceCryptoAsset validates a crypto portfolio's holdings and fills in the
// server-computed value fields. Client-sent values are ignored for crypto —
// the market price is the only source of truth.
func (h *Handler) priceCryptoAsset(ctx context.Context, a *models.Asset) error {
	if len(a.CryptoHoldings) == 0 {
		return fmt.Errorf("add at least one coin")
	}
	usd, inr, normalized, err := h.Crypto.ValuePortfolio(ctx, a.CryptoHoldings)
	if err != nil {
		return err
	}
	a.CryptoHoldings = normalized
	a.CurrentValue = inr
	a.TotalValueUSD = &usd
	return nil
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var body models.UserRegister
	if err := decodeJSON(r, &body); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if body.Email == "" || body.Password == "" {
		WriteError(w, http.StatusBadRequest, "Email and password required")
		return
	}
	ctx := r.Context()
	existing, err := h.Store.FindUserByEmail(ctx, body.Email)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Database error")
		return
	}
	if existing != nil {
		// Deliberately generic: a specific "already registered" message
		// would confirm which emails have accounts.
		WriteError(w, http.StatusBadRequest, "Could not create account. Please try signing in instead.")
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Could not hash password")
		return
	}
	u := &models.User{
		ID:           newID(),
		Email:        body.Email,
		PasswordHash: hash,
		IsVerified:   true,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := h.Store.InsertUser(ctx, u); err != nil {
		WriteError(w, http.StatusInternalServerError, "Could not create user")
		return
	}
	token, err := auth.SignToken(u.ID, h.Config.JWTSecret, h.Config.JWTExpiration())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Could not issue token")
		return
	}
	msg := "Account created successfully!"
	WriteJSON(w, http.StatusOK, models.TokenResponse{
		AccessToken: token,
		TokenType:   "bearer",
		Message:     &msg,
	})
}

// AuthLookup powers the one-door auth flow: given an email, it reports
// whether an account exists so the client can show either the sign-in or
// the create-password step. It is rate-limited (see router) since it is
// an intentional, throttled email-existence check.
func (h *Handler) AuthLookup(w http.ResponseWriter, r *http.Request) {
	var body models.EmailLookup
	if err := decodeJSON(r, &body); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if body.Email == "" {
		WriteError(w, http.StatusBadRequest, "Email required")
		return
	}
	u, err := h.Store.FindUserByEmail(r.Context(), body.Email)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Database error")
		return
	}
	WriteJSON(w, http.StatusOK, models.EmailLookupResponse{Exists: u != nil})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var body models.UserLogin
	if err := decodeJSON(r, &body); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	ctx := r.Context()
	u, err := h.Store.FindUserByEmail(ctx, body.Email)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Database error")
		return
	}
	if u == nil || !auth.VerifyPassword(body.Password, u.PasswordHash) {
		WriteError(w, http.StatusUnauthorized, "Invalid email or password")
		return
	}
	if !u.IsVerified {
		WriteError(w, http.StatusUnauthorized, "Email not verified")
		return
	}
	token, err := auth.SignToken(u.ID, h.Config.JWTSecret, h.Config.JWTExpiration())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Could not issue token")
		return
	}
	WriteJSON(w, http.StatusOK, models.TokenResponse{AccessToken: token, TokenType: "bearer"})
}

func (h *Handler) ListAssets(w http.ResponseWriter, r *http.Request) {
	uid := middleware.UserID(r.Context())
	assets, err := h.Store.ListAssetsByUser(r.Context(), uid)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Database error")
		return
	}
	if assets == nil {
		assets = []models.Asset{}
	}
	WriteJSON(w, http.StatusOK, assets)
}

func (h *Handler) CreateAsset(w http.ResponseWriter, r *http.Request) {
	var in models.AssetCreate
	if err := decodeJSON(r, &in); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	uid := middleware.UserID(r.Context())
	a := models.Asset{
		ID:                   newID(),
		Name:                 in.Name,
		Category:             in.Category,
		CurrentValue:         in.CurrentValue,
		IsForeign:            in.IsForeign,
		ForeignCurrency:      in.ForeignCurrency,
		ForeignAmount:        in.ForeignAmount,
		AssetType:            in.AssetType,
		TravelPointsPrograms: in.TravelPointsPrograms,
		TotalValueUSD:        in.TotalValueUSD,
		TotalValueINR:        in.TotalValueINR,
		CryptoHoldings:       in.CryptoHoldings,
		MFHoldings:           in.MFHoldings,
		UpdatedAt:            time.Now().UTC().Format(time.RFC3339Nano),
	}
	a.UserID = stringPtr(uid)
	if a.AssetType != nil && *a.AssetType == "crypto" {
		if err := h.priceCryptoAsset(r.Context(), &a); err != nil {
			WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if a.AssetType != nil && *a.AssetType == "mutual_fund" {
		if err := h.priceMFAsset(r.Context(), &a); err != nil {
			WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := h.Store.InsertAsset(r.Context(), &a); err != nil {
		WriteError(w, http.StatusInternalServerError, "Could not create asset")
		return
	}
	_ = h.Snapshot.CreateDailySnapshot(r.Context(), uid)
	WriteJSON(w, http.StatusOK, a)
}

func (h *Handler) UpdateAsset(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "assetID")
	var in models.AssetUpdate
	if err := decodeJSON(r, &in); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	uid := middleware.UserID(r.Context())
	existing, err := h.Store.FindAsset(r.Context(), id, uid)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Database error")
		return
	}
	if existing == nil {
		WriteError(w, http.StatusNotFound, "Asset not found")
		return
	}
	update := assetUpdateBSON(in)
	// If the asset is (or becomes) a units-based crypto holding, reprice it
	// server-side from the merged symbol/units instead of trusting the client.
	effType := existing.AssetType
	if in.AssetType != nil {
		effType = in.AssetType
	}
	if effType != nil && *effType == "crypto" {
		merged := *existing
		merged.AssetType = effType
		if in.CryptoHoldings != nil {
			merged.CryptoHoldings = in.CryptoHoldings
		}
		if err := h.priceCryptoAsset(r.Context(), &merged); err != nil {
			WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		update["crypto_holdings"] = merged.CryptoHoldings
		update["current_value"] = merged.CurrentValue
		update["total_value_usd"] = *merged.TotalValueUSD
	}
	if effType != nil && *effType == "mutual_fund" {
		merged := *existing
		merged.AssetType = effType
		if in.MFHoldings != nil {
			merged.MFHoldings = in.MFHoldings
		}
		if err := h.priceMFAsset(r.Context(), &merged); err != nil {
			WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		update["mf_holdings"] = merged.MFHoldings
		update["current_value"] = merged.CurrentValue
	}
	update["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	if err := h.Store.UpdateAsset(r.Context(), id, uid, update); err != nil {
		WriteError(w, http.StatusInternalServerError, "Could not update asset")
		return
	}
	out, err := h.Store.FindAsset(r.Context(), id, uid)
	if err != nil || out == nil {
		WriteError(w, http.StatusInternalServerError, "Could not load asset")
		return
	}
	_ = h.Snapshot.CreateDailySnapshot(r.Context(), uid)
	WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) DeleteAsset(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "assetID")
	uid := middleware.UserID(r.Context())
	ok, err := h.Store.DeleteAsset(r.Context(), id, uid)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Database error")
		return
	}
	if !ok {
		WriteError(w, http.StatusNotFound, "Asset not found")
		return
	}
	_ = h.Snapshot.CreateDailySnapshot(r.Context(), uid)
	WriteJSON(w, http.StatusOK, map[string]string{"message": "Asset deleted"})
}

func (h *Handler) ListLiabilities(w http.ResponseWriter, r *http.Request) {
	uid := middleware.UserID(r.Context())
	list, err := h.Store.ListLiabilitiesByUser(r.Context(), uid)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Database error")
		return
	}
	if list == nil {
		list = []models.Liability{}
	}
	WriteJSON(w, http.StatusOK, list)
}

func (h *Handler) CreateLiability(w http.ResponseWriter, r *http.Request) {
	var in models.LiabilityCreate
	if err := decodeJSON(r, &in); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	uid := middleware.UserID(r.Context())
	l := models.Liability{
		ID:        newID(),
		Name:      in.Name,
		Category:  in.Category,
		Amount:    in.Amount,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	l.UserID = stringPtr(uid)
	if err := h.Store.InsertLiability(r.Context(), &l); err != nil {
		WriteError(w, http.StatusInternalServerError, "Could not create liability")
		return
	}
	_ = h.Snapshot.CreateDailySnapshot(r.Context(), uid)
	WriteJSON(w, http.StatusOK, l)
}

func (h *Handler) UpdateLiability(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "liabilityID")
	var in models.LiabilityUpdate
	if err := decodeJSON(r, &in); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	uid := middleware.UserID(r.Context())
	existing, err := h.Store.FindLiability(r.Context(), id, uid)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Database error")
		return
	}
	if existing == nil {
		WriteError(w, http.StatusNotFound, "Liability not found")
		return
	}
	update := liabilityUpdateBSON(in)
	update["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	if err := h.Store.UpdateLiability(r.Context(), id, uid, update); err != nil {
		WriteError(w, http.StatusInternalServerError, "Could not update liability")
		return
	}
	out, err := h.Store.FindLiability(r.Context(), id, uid)
	if err != nil || out == nil {
		WriteError(w, http.StatusInternalServerError, "Could not load liability")
		return
	}
	_ = h.Snapshot.CreateDailySnapshot(r.Context(), uid)
	WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) DeleteLiability(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "liabilityID")
	uid := middleware.UserID(r.Context())
	ok, err := h.Store.DeleteLiability(r.Context(), id, uid)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Database error")
		return
	}
	if !ok {
		WriteError(w, http.StatusNotFound, "Liability not found")
		return
	}
	_ = h.Snapshot.CreateDailySnapshot(r.Context(), uid)
	WriteJSON(w, http.StatusOK, map[string]string{"message": "Liability deleted"})
}

func (h *Handler) ListSnapshots(w http.ResponseWriter, r *http.Request) {
	uid := middleware.UserID(r.Context())
	q := r.URL.Query()
	var start, end *string
	if v := q.Get("start_date"); v != "" {
		start = &v
	}
	if v := q.Get("end_date"); v != "" {
		end = &v
	}
	list, err := h.Store.ListSnapshots(r.Context(), uid, start, end)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Database error")
		return
	}
	if list == nil {
		list = []models.Snapshot{}
	}
	WriteJSON(w, http.StatusOK, list)
}

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	// Refresh-on-read: the Fly machine sleeps when idle, so crons alone
	// can't keep prices current. Reading the dashboard refreshes market
	// data when it's stale — at most once per window, so browsing never
	// hammers the external APIs. Synchronous (bounded by the timeout) so
	// this very response reflects the fresh values; on timeout the stale
	// cache is served instead.
	refreshCtx, cancelRefresh := context.WithTimeout(r.Context(), 10*time.Second)
	h.Crypto.RefreshIfStale(refreshCtx, time.Hour)
	h.MF.RefreshIfStale(refreshCtx, 12*time.Hour)
	h.FX.RevalueIfStale(refreshCtx, 12*time.Hour)
	cancelRefresh()

	uid := middleware.UserID(r.Context())
	assets, err := h.Store.ListAssetsByUser(r.Context(), uid)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Database error")
		return
	}
	liabs, err := h.Store.ListLiabilitiesByUser(r.Context(), uid)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Database error")
		return
	}
	var totalAssets float64
	for _, a := range assets {
		totalAssets += a.CurrentValue
	}
	var totalUSD float64
	for _, a := range assets {
		if a.AssetType != nil && *a.AssetType == "travel_points" {
			if a.TotalValueUSD != nil {
				totalUSD += *a.TotalValueUSD
			}
		}
	}
	var totalLiab float64
	for _, l := range liabs {
		totalLiab += l.Amount
	}
	WriteJSON(w, http.StatusOK, models.DashboardData{
		Assets:           assets,
		Liabilities:      liabs,
		TotalAssets:      totalAssets,
		TotalAssetsUSD:   totalUSD,
		TotalLiabilities: totalLiab,
		NetWorth:         totalAssets - totalLiab,
	})
}

func (h *Handler) ExchangeRates(w http.ResponseWriter, r *http.Request) {
	rates, err := h.Rates.Get(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Could not load exchange rates")
		return
	}
	WriteJSON(w, http.StatusOK, models.ExchangeRatesResponse{
		Rates:     rates,
		Base:      "INR",
		Timestamp: time.Now().UTC(),
	})
}

func (h *Handler) CryptoPrices(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Store.ListCryptoPrices(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Could not load crypto prices")
		return
	}
	WriteJSON(w, http.StatusOK, rows)
}