package api

import (
	"net/http"
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
		WriteError(w, http.StatusBadRequest, "Email already registered")
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
		UpdatedAt:            time.Now().UTC().Format(time.RFC3339Nano),
	}
	a.UserID = stringPtr(uid)
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
