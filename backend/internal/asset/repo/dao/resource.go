package dao

import (
	"context"
)

type Status uint

const (
	StatusPending Status = iota
	StatusProcessing
	StatusCompleted
	StatusFailed
)

type AssetResourceType string

const (
	AssetResourceTypeProtoType AssetResourceType = "protoType"
	AssetResourceTypeFrame     AssetResourceType = "frame"
	AssetResourceTypeTile      AssetResourceType = "tile"
	AssetResourceTypeUI        AssetResourceType = "ui"
	AssetResourceTypeScenery   AssetResourceType = "scenery"
	AssetResourceTypeAnimation AssetResourceType = "animation"
	AssetResourceTypeItem      AssetResourceType = "item"
)

type AssetResourceDao interface {
	CreateAssetResource(ctx context.Context, resource *AssetResource) (uint, error)
}

type AssetResource struct {
	ID           uint
	Name         string
	ParentID     *uint
	AssetID      uint
	AssetVersion uint
	Type         AssetResourceType
	Url          *string
	Status	   Status
}
