package router

import (
	"github.com/1024XEngineer/Holonic-Asset/internal/asset/domain"
)

type CreateCharacterAssetRequest struct {
	Asset *domain.Asset
}

type CreateCharacterAssetResponse struct {
	ID uint
}

type CreateObjectAssetRequest struct {
	Asset *domain.Asset
}

type CreateObjectAssetResponse struct {
	ID uint
}

type CopyAssetRequest struct {
	AssetID uint
}

type CopyAssetResponse struct {
	NewAssetID uint
}