package domain

type TeamStats struct {
	Name          string
	ID            int64
	Members       int64
	DoneLast7Days int64
}

type TeamTopCreator struct {
	TeamName     string
	UserName     string
	TeamID       int64
	UserID       int64
	TasksCreated int64
	Rank         int64
}

type OrphanAssignee struct {
	Title      string
	TaskID     int64
	TeamID     int64
	AssigneeID int64
}
