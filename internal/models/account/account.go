package account

type Profile struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

type Account struct {
	Profiles        []Profile `json:"profiles"`
	SelectedProfile string    `json:"selected_profile"`
}
