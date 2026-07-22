package domain

import "time"

type TeamRole string

const (
	TeamRoleMember TeamRole = "member"
	TeamRoleAdmin  TeamRole = "admin"
	TeamRoleOwner  TeamRole = "owner"
)

var teamRoleRank = map[TeamRole]int{
	TeamRoleMember: 1,
	TeamRoleAdmin:  2,
	TeamRoleOwner:  3,
}

func (r TeamRole) AtLeast(min TeamRole) bool {
	return teamRoleRank[r] >= teamRoleRank[min]
}

type Team struct {
	CreatedAt time.Time
	Name      string
	ID        int64
	CreatedBy int64
}

type TeamMembership struct {
	Name string
	Role TeamRole
	ID   int64
}
