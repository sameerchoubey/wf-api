package service

import (
	"context"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/google/uuid"

	"wealthflow/backend/internal/models"
	"wealthflow/backend/internal/store"
)

type Snapshot struct {
	Store *store.Store
}

func toBSONSliceAssets(assets []models.Asset) ([]interface{}, error) {
	out := make([]interface{}, len(assets))
	for i := range assets {
		b, err := bson.Marshal(assets[i])
		if err != nil {
			return nil, err
		}
		var m bson.M
		if err := bson.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		out[i] = m
	}
	return out, nil
}

func toBSONSliceLiabilities(liabs []models.Liability) ([]interface{}, error) {
	out := make([]interface{}, len(liabs))
	for i := range liabs {
		b, err := bson.Marshal(liabs[i])
		if err != nil {
			return nil, err
		}
		var m bson.M
		if err := bson.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		out[i] = m
	}
	return out, nil
}

// CreateDailySnapshot creates or updates today's snapshot for a user.
func (s *Snapshot) CreateDailySnapshot(ctx context.Context, userID string) error {
	assets, err := s.Store.ListAssetsByUser(ctx, userID)
	if err != nil {
		return err
	}
	liabs, err := s.Store.ListLiabilitiesByUser(ctx, userID)
	if err != nil {
		return err
	}

	var totalAssets float64
	for _, a := range assets {
		totalAssets += a.CurrentValue
	}
	var totalAssetsUSD float64
	for _, a := range assets {
		if a.AssetType != nil && *a.AssetType == "travel_points" {
			if a.TotalValueUSD != nil {
				totalAssetsUSD += *a.TotalValueUSD
			}
		}
	}
	var totalLiab float64
	for _, l := range liabs {
		totalLiab += l.Amount
	}
	netWorth := totalAssets - totalLiab

	today := time.Now().UTC().Format("2006-01-02")
	now := time.Now().UTC()

	assetBSON, err := toBSONSliceAssets(assets)
	if err != nil {
		return err
	}
	liabBSON, err := toBSONSliceLiabilities(liabs)
	if err != nil {
		return err
	}

	existing, err := s.Store.FindSnapshotByUserDate(ctx, userID, today)
	if err != nil {
		return err
	}
	snapshotID := uuid.New().String()
	if existing != nil && existing.ID != "" {
		snapshotID = existing.ID
	}

	fields := bson.M{
		"id":                snapshotID,
		"user_id":           userID,
		"date":              today,
		"timestamp":         now.Format(time.RFC3339Nano),
		"total_assets":      totalAssets,
		"total_assets_usd":  totalAssetsUSD,
		"total_liabilities": totalLiab,
		"net_worth":         netWorth,
		"assets":            assetBSON,
		"liabilities":       liabBSON,
	}
	return s.Store.SetSnapshot(ctx, userID, today, fields)
}

// CreateSnapshotsForAllUsers runs the scheduled job (distinct user IDs from assets ∪ liabilities).
func (s *Snapshot) CreateSnapshotsForAllUsers(ctx context.Context) error {
	aIDs, err := s.Store.DistinctAssetUserIDs(ctx)
	if err != nil {
		return err
	}
	lIDs, err := s.Store.DistinctLiabilityUserIDs(ctx)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{})
	var ids []string
	for _, id := range aIDs {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	for _, id := range lIDs {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	for _, uid := range ids {
		if err := s.CreateDailySnapshot(ctx, uid); err != nil {
			slog.Error("snapshot failed for user", "user_id", uid, "err", err)
		}
	}
	return nil
}
