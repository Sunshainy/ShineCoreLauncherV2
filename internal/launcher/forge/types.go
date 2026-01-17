package forge

import (
	"encoding/json"
	"fmt"
	"strings"
)

type InstallProfile struct {
	Minecraft  string            `json:"minecraft"`
	Path       *GAV              `json:"path,omitempty"`
	JSON       string            `json:"json"`
	Libraries  []InstallLibrary  `json:"libraries"`
	Processors []InstallProcessor `json:"processors"`
	Data       map[string]InstallDataEntry `json:"data"`
}

type InstallLibrary struct {
	Name      GAV                   `json:"name"`
	Downloads InstallLibraryDownloads `json:"downloads"`
}

type InstallLibraryDownloads struct {
	Artifact LibraryDownload `json:"artifact"`
}

type LibraryDownload struct {
	Path string `json:"path"`
	Sha1 string `json:"sha1"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

type InstallProcessor struct {
	Jar      GAV              `json:"jar"`
	Sides    []string         `json:"sides,omitempty"`
	Classpath []GAV           `json:"classpath,omitempty"`
	Args     []string         `json:"args,omitempty"`
	Outputs  map[string]string `json:"outputs,omitempty"`
}

type InstallDataEntry struct {
	Client string `json:"client"`
	Server string `json:"server"`
}

func (e InstallDataEntry) ForSide(side string) string {
	if side == "server" {
		return e.Server
	}
	return e.Client
}

type GAV struct {
	Group      string
	Artifact   string
	Version    string
	Classifier string
	Ext        string
}

func (g *GAV) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if data[0] == '"' {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		parsed, err := ParseGAV(raw)
		if err != nil {
			return err
		}
		*g = parsed
		return nil
	}
	type alias GAV
	var tmp alias
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*g = GAV(tmp)
	return nil
}

func ParseGAV(input string) (GAV, error) {
	parts := strings.Split(input, ":")
	if len(parts) < 3 {
		return GAV{}, fmt.Errorf("invalid gav: %s", input)
	}
	gav := GAV{
		Group:    parts[0],
		Artifact: parts[1],
		Version:  parts[2],
		Ext:      "jar",
	}
	if len(parts) >= 4 {
		gav.Classifier = parts[3]
	}
	if strings.Contains(gav.Version, "@") {
		split := strings.Split(gav.Version, "@")
		gav.Version = split[0]
		gav.Ext = split[1]
	}
	return gav, nil
}

func (g GAV) String() string {
	if g.Classifier != "" {
		return fmt.Sprintf("%s:%s:%s:%s", g.Group, g.Artifact, g.Version, g.Classifier)
	}
	return fmt.Sprintf("%s:%s:%s", g.Group, g.Artifact, g.Version)
}

func (g GAV) FilePath() string {
	groupPath := strings.ReplaceAll(g.Group, ".", "/")
	file := g.Artifact + "-" + g.Version
	if g.Classifier != "" {
		file += "-" + g.Classifier
	}
	file += "." + g.Ext
	return fmt.Sprintf("%s/%s/%s/%s", groupPath, g.Artifact, g.Version, file)
}
