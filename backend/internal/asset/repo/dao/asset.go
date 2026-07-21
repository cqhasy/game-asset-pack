package dao

import (
	"context"
	"encoding/json"
)

type AssetType string

const (
	AssetTypeCharacter AssetType = "character"
	AssetTypeTileSet   AssetType = "tileSet"
	AssetTypeAudio     AssetType = "audio"
	AssetTypeUI        AssetType = "ui"
	AssetTypeObject    AssetType = "object"
	AssetTypeScenery   AssetType = "scenery"
)

type Asset struct {
	ID          uint
	Name        string
	ProjectID   uint
	Type        AssetType
	Description string
	Tags        []string        `json:"tags"`
	Attributes  json.RawMessage `json:"attributes"`
	Version     uint
}

type AssetDao interface {
	CreateAsset(ctx context.Context, asset *Asset) (uint, error)
}
