package mojang

import (
	"encoding/json"
)

type VersionManifest struct {
	Versions []ManifestVersion `json:"versions"`
}

type ManifestVersion struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type VersionMetadata struct {
	ID                 string            `json:"id"`
	InheritsFrom       string            `json:"inheritsFrom,omitempty"`
	MainClass          string            `json:"mainClass"`
	Type               string            `json:"type"`
	Arguments          VersionArguments  `json:"arguments"`
	MinecraftArguments string            `json:"minecraftArguments,omitempty"`
	AssetIndex         AssetIndex        `json:"assetIndex"`
	Downloads          VersionDownloads  `json:"downloads"`
	Libraries          []Library         `json:"libraries"`
	ReleaseTime        string            `json:"releaseTime,omitempty"`
	Time               string            `json:"time,omitempty"`
	Logging            map[string]any    `json:"logging,omitempty"`
	MinimumLauncherVer int               `json:"minimumLauncherVersion,omitempty"`
	JavaVersion        map[string]any    `json:"javaVersion,omitempty"`
	CompatibilityRules []map[string]any  `json:"compatibilityRules,omitempty"`
}

type VersionArguments struct {
	Game []Argument `json:"game"`
	Jvm  []Argument `json:"jvm"`
}

type Argument struct {
	Value any    `json:"value"`
	Rules []Rule `json:"rules,omitempty"`
}

func (a *Argument) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		a.Value = value
		a.Rules = nil
		return nil
	}
	var obj struct {
		Value any    `json:"value"`
		Rules []Rule `json:"rules,omitempty"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	a.Value = obj.Value
	a.Rules = obj.Rules
	return nil
}

type Rule struct {
	Action string   `json:"action"`
	OS     *RuleOS  `json:"os,omitempty"`
	Features map[string]bool `json:"features,omitempty"`
}

type RuleOS struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	Arch    string `json:"arch,omitempty"`
}

type AssetIndex struct {
	ID        string `json:"id"`
	Sha1      string `json:"sha1"`
	Size      int64  `json:"size"`
	URL       string `json:"url"`
	TotalSize int64  `json:"totalSize"`
}

type VersionDownloads struct {
	Client DownloadInfo `json:"client"`
}

type DownloadInfo struct {
	Sha1 string `json:"sha1"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
	Path string `json:"path,omitempty"`
}

type Library struct {
	Name      string              `json:"name"`
	Downloads *LibraryDownloads   `json:"downloads,omitempty"`
	URL       string              `json:"url,omitempty"`
	Natives   map[string]string   `json:"natives,omitempty"`
	Rules     []Rule              `json:"rules,omitempty"`
	Extract   *LibraryExtract     `json:"extract,omitempty"`
}

type LibraryDownloads struct {
	Artifact   *LibraryArtifact             `json:"artifact,omitempty"`
	Classifiers map[string]LibraryArtifact `json:"classifiers,omitempty"`
}

type LibraryArtifact struct {
	Path string `json:"path"`
	Sha1 string `json:"sha1"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

type LibraryExtract struct {
	Exclude []string `json:"exclude,omitempty"`
}

type AssetIndexFile struct {
	Objects map[string]AssetObject `json:"objects"`
}

type AssetObject struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}
