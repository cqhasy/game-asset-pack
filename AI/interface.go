package ai

type Layer struct {
	ID 	  uint     `json:"id"`        // Layer ID
	Prompt string `json:"prompt"`  // Prompt for the layer
	Reference []string `json:"reference"`  // Reference images for scene creation
}

type LayerResult struct {
	ID 	  uint     `json:"id"`        // Layer ID
	Url string `json:"url"`       // Generated image URL
}

type CreateSceneRequest struct {
	Style string `json:"style"`  // Style of the scene
	Layers  []Layer `json:"layers"`  // Layers of the scene
}

type CreateSceneResponse struct {
	Layers []LayerResult `json:"layers"` // Results for each layer
}

type CreateTileSetRequest struct {
	Prompt string `json:"prompt"`  // Prompt for the tile set
	Reference []string `json:"reference"`  // Reference images for tile set creation
}

type CreateTileSetResponse struct {
	Url string `json:"url"`       // Generated tile set image URL
}

type MapService interface {
	CreateScene(request *CreateSceneRequest) (*CreateSceneResponse, error)
	CreateTileSet(request *CreateTileSetRequest) (*CreateTileSetResponse, error)
}