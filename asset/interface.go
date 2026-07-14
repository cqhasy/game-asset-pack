package asset

type AssetType string

const (
	AssetTypeCharacter AssetType = "Character"
	AssetTypeUI AssetType = "UI"
	AssetTypeObject AssetType = "Object"
	AssetTypeMusic AssetType = "Music"
	AssetTypeMap AssetType = "Map"
)

type MusicType AssetType

const (
	AssetTypeBGM MusicType = "BGM"
	AssetTypeSoundEffect MusicType = "SoundEffect"
)