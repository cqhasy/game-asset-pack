package asset

type AssetType string

const (
	AssetTypeCharacter AssetType = "Character"
	AssetTypeUI AssetType = "UI"
	AssetTypeObject AssetType = "Object"
	AssetTypeMap AssetType = "Map"
)

type MusicType AssetType

const (
	AssetTypeBGM MusicType = "BGM"
	AssetTypeSoundEffect MusicType = "SoundEffect"
)

type MapType AssetType

const (
	AssetTypeTileMap MapType = "TileMap"
	AssetTypeScene MapType = "Scene"
)

type Asset struct {
	ID          uint
	Name        string
	Type        AssetType `json:"type"`        // Character、UI、Object、Map...
	Description string    `json:"description"` // Asset description
	ItemID      uint      `json:"itemId"`      // Item ID
	ProjectID   uint      `json:"projectId"`   // Project ID
}

type Query struct {}

type AssetService interface {
	Create(asset *Asset) error
	CreateBatch(assets []*Asset) error
	Copy(id uint) (*Asset, error)
	CopyBatch(ids []uint) ([]*Asset, error)
	List(query Query) ([]*Asset, error)
	GetDetail(id uint) (*Asset, error)
	Update(asset *Asset) error
	Delete(ids []uint) error
}

type Item struct {
	ID          uint
	Name        string
	
}