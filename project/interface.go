package project

import (
	"context"
)

type GameType string
type ViewType string

const (
	GameTypeRPG GameType = "RPG"
	GameTypeACT GameType = "ACT"
	GameTypeSLG GameType = "SLG"
	GameTypeOther GameType = "Other"


	ViewTypeTopDown ViewType = "TopDown"
	ViewTypeSideView ViewType = "SideView"
	ViewTypeIsometric ViewType = "Isometric"
)
type Project struct {
	ID 		uint
	Name        string
	GameType    GameType    `json:"gameType"`    // RPG、ACT、SLG...
	ViewType    ViewType    `json:"viewType"`    // TopDown、SideView、Isometric...
	Description string 	 // Project description
	Style       string 	 // Art style of the project
}

type ProjectService interface {
	Create(ctx context.Context, project *Project) error
	// Get project list by User ID
	ListByUid(ctx context.Context, uid uint) ([]*Project, error)
	// GetDetail returns the details of the project.
	GetDetail(ctx context.Context, id uint) (*Project, error)
	Update(ctx context.Context, project *Project) error
}