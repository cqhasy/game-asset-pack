package ai

type Size struct {
	Width int `json:"width"`
	Height int `json:"height"`
}

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
	ProjectPrompt string `json:"projectPrompt"`  // Project prompt
	Style string `json:"style"`  // Style of the scene
	Layers  []Layer `json:"layers"`  // Layers of the scene
}

type CreateSceneResponse struct {
	Layers []LayerResult `json:"layers"` // Results for each layer
}

type CreateTileSetRequest struct {
	ProjectPrompt string `json:"projectPrompt"`  // Project prompt
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

type ViewType string
const (
	ViewTypeTopDown ViewType = "TopDown"
	ViewTypeSideView ViewType = "SideView"
	ViewTypeIsometric ViewType = "Isometric"
)

type CreateObjectRequest struct {
	UserPrompt string `json:"prompt"`  // Prompt for the object
	ProjectPrompt string `json:"projectPrompt"`  // Project prompt
	Derictions  int `json:"derictions"`  // Number of directions for the object (e.g. 1, 4, 8)
	Reference string `json:"reference"`  // Reference image for object creation
	Size Size `json:"size"`  // Size of the object (e.g. "32X32", "64X64")
	View ViewType `json:"view"`  // View type of the object (e.g. "TopDown", "SideView", "Isometric")
}

type CreateObjectResponse struct {
	Url string `json:"url"`       // Generated object image URL
}

type ObjectService interface {
	CreateObject(request *CreateObjectRequest) (*CreateObjectResponse, error)
}