package analytics

// responses
type teamStatsResponse struct {
	Name          string `json:"name"`
	ID            int64  `json:"id"`
	Members       int64  `json:"members"`
	DoneLast7Days int64  `json:"done_last_7d"`
}

type topCreatorResponse struct {
	TeamName     string `json:"team_name"`
	UserName     string `json:"user_name"`
	TeamID       int64  `json:"team_id"`
	UserID       int64  `json:"user_id"`
	TasksCreated int64  `json:"tasks_created"`
	Rank         int64  `json:"rank"`
}

type orphanAssigneeResponse struct {
	Title      string `json:"title"`
	TaskID     int64  `json:"task_id"`
	TeamID     int64  `json:"team_id"`
	AssigneeID int64  `json:"assignee_id"`
}
