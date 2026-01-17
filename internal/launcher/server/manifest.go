package server

type Manifest struct {
	Project     string           `json:"project"`
	Studio      string           `json:"studio"`
	Version     string           `json:"version"`
	GeneratedAt string           `json:"generated_at"`
	Dependencies Dependencies     `json:"dependencies"`
	Packages    ManifestPackages `json:"packages"`
}

type Dependencies struct {
	GameVersion   string `json:"game_version"`
	Loader        string `json:"loader"`
	LoaderVersion string `json:"loader_version"`
	JavaPackage   string `json:"java_package"`
	JavaVersion   int    `json:"java_version"`
}

type ManifestPackages struct {
	Mods  []FilePackage `json:"mods"`
	Javas []FilePackage `json:"javas"`
}

type FilePackage struct {
	Path   string `json:"path"`
	Name   string `json:"name,omitempty"`
	Size   int64  `json:"size"`
	Sha256 string `json:"sha256"`
	URL    string `json:"url"`
}
