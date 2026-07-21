package repo

import (
	"context"

	"github.com/1024XEngineer/Holonic-Asset/internal/asset/domain"
	"github.com/1024XEngineer/Holonic-Asset/internal/asset/repo/dao"
)

type AssetRepository interface {
	// 创建一个Character Asset，创建一个空protoType Resource
	CreateCharacterAsset(ctx context.Context, asset *domain.Asset) (uint, error)
	// 创建绑定到某个protoType的image资源
	CreateImageResources(ctx context.Context, resource []domain.AssetResource) ([]domain.AssetResource, error)
	CreateAnimationResource(ctx context.Context, resource *domain.AssetResource) (uint, error)

	UpdateFrameResources(ctx context.Context, resource []domain.AssetResource) error
	UpdateProtoTypeResources(ctx context.Context, resource []domain.AssetResource) error

	GetAnimations(ctx context.Context, assetID uint, version uint) ([]domain.AssetResource, error)
	GetProtoTypeResources(ctx context.Context, assetID uint, version uint) ([]domain.AssetResource, error)
	
	// 创建一个AssetVersion并更新Asset的Version，复制Asset下的所有Resource到新版本下
	CreateRecord(ctx context.Context, version *domain.AssetVersion) (*domain.AssetVersion, error)
	// 删除 AssetVersion，回滚Asset的Version到上一个版本,删除所有该版本的Resource
	RollBackRecord(ctx context.Context, assetID uint, version uint) (uint, error)
	// 全量复制Version、Asset及其所有Resource到新Asset下
	Copy(ctx context.Context, assetID uint) (uint, error)
}

func convertAssetToDao(asset *domain.Asset) *dao.Asset {
	return &dao.Asset{
		ID:          asset.ID,
		Name:        asset.Name,
		ProjectID:   asset.ProjectID,
		Type:        dao.AssetType(asset.Type),
		Description: asset.Description,
		Tags:        asset.Tags,
		Attributes:  asset.Attributes,
		Version:     asset.Version,
	}
}

func convertAssetToDomain(asset *dao.Asset) *domain.Asset {
	return &domain.Asset{
		ID:          asset.ID,
		Name:        asset.Name,
		ProjectID:   asset.ProjectID,
		Type:        domain.AssetType(asset.Type),
		Description: asset.Description,
		Tags:        asset.Tags,
		Attributes:  asset.Attributes,
		Version:     asset.Version,
	}
}

func convertResourceToDao(resource *domain.AssetResource) *dao.AssetResource {
	return &dao.AssetResource{
		ID:           resource.ID,
		Name:         resource.Name,
		ParentID:     resource.ParentID,
		AssetID:      resource.AssetID,
		AssetVersion: resource.AssetVersion,
		Type:         dao.AssetResourceType(resource.Type),
		Url:          resource.Url,
	}
}

func convertResourceToDomain(resource *dao.AssetResource) *domain.AssetResource {
	return &domain.AssetResource{
		ID:           resource.ID,
		Name:         resource.Name,
		ParentID:     resource.ParentID,
		AssetID:      resource.AssetID,
		AssetVersion: resource.AssetVersion,
		Type:         domain.AssetResourceType(resource.Type),
		Url:          resource.Url,
	}
}
