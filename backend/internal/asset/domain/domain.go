package domain

import (
	"context"
	"encoding/json"
)

type AssetType string
type AssetResourceType string

const (

AssetTypeCharacter AssetType = "character"

AssetTypeTileSet AssetType = "tileSet"

AssetTypeAudio AssetType = "audio"

AssetTypeUI AssetType = "ui"

AssetTypeObject AssetType = "object"

AssetTypeScenery AssetType = "scenery"

)

const (
AssetResourceTypeProtoType AssetResourceType = "protoType"

// image绑定在protoType上
AssetResourceTypeImage     AssetResourceType = "image"

AssetResourceTypeAnimation AssetResourceType = "animation"

// frame绑定在animation上
AssetResourceTypeFrame AssetResourceType = "frame"

AssetResourceTypeItem AssetResourceType = "item"

// tile绑定在item上
AssetResourceTypeTile AssetResourceType = "tile"

AssetResourceTypeUI AssetResourceType = "ui"

AssetResourceTypeScenery AssetResourceType = "scenery"

)

type Asset struct {
	ID   uint
	Name string
	ProjectID uint
	Type AssetType
	Description string
	Tags []string `json:"tags"`
	Attributes json.RawMessage `json:"attributes"`
	Version uint
}

type AssetVersion struct {
	ID   uint
	AssetID uint
	Version uint
	CreatedAt int64
}

type AssetResource struct {
	ID   uint
	Name string
	ParentID *uint
	AssetID uint
	AssetVersion uint
	Type  AssetResourceType
	Url   *string
}

type AssetService interface {
	// 创建一个Character Asset，创建一个空protoType Resource
	CreateCharacterAsset(ctx context.Context, asset *Asset) (uint, error)
	// 创建一个Object Asset，创建一个空protoType Resource
	CreateObjectAsset(ctx context.Context, asset *Asset) (uint, error)
	CreateTileSetAsset(ctx context.Context, asset *Asset) (uint, error)
	CreateUIAsset(ctx context.Context, asset *Asset) (uint, error)

	CreateSceneryAsset(ctx context.Context, asset *Asset) (uint, error)
	// 创建绑定到某个Asset下的Animation Resource
	CreateAnimationResource(ctx context.Context, resource *AssetResource) (uint, error)
	GetAnimations(ctx context.Context, assetID uint, version uint) ([]AssetResource, error)
	// 创建绑定到某个Animation下的Frame Resource
	CreateFrameResources(ctx context.Context, resource []AssetResource) ([]AssetResource, error)
	EditFrameResources(ctx context.Context, resource []AssetResource) ([]AssetResource, error)
	// CreateItemResource(ctx context.Context, resource *AssetResource) (uint, error)
	// CreateUIResource(ctx context.Context, resource *AssetResource) (uint, error)
	// CreateSceneryResource(ctx context.Context, resource *AssetResource) (uint, error)

	CreateImageResources(ctx context.Context, resource []AssetResource) ([]AssetResource, error)
	CreateRecord(ctx context.Context, version *AssetVersion) (uint, error)
	GetVersionHistory(ctx context.Context, assetID uint) ([]AssetVersion, error)
	RollBackVersion(ctx context.Context, assetID uint, version uint) (uint, error)
	Copy(ctx context.Context, assetID uint, version uint) (uint, error)
}
